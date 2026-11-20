package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrResetUsed is a password reset link that was used, replaced or has expired.
var ErrResetUsed = errors.New("this reset link was used or has expired")

// ResetThrottle is how long after one reset email another is refused, so the
// form cannot be used to flood someone's inbox.
const ResetThrottle = time.Minute

// CreatePasswordReset stores a reset token for the active local account with
// this email and returns that user. It returns ErrNotFound when there is no
// such account (unknown email, SSO-only, deactivated) or when one was sent in
// the last ResetThrottle; callers answer the same either way. A new token
// replaces the user's earlier unused ones.
func (s *Store) CreatePasswordReset(ctx context.Context, email string, tokenHash []byte, expires time.Time) (User, error) {
	var u User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`SELECT id, email, display_name FROM users
			 WHERE email = $1 AND is_active AND password_hash IS NOT NULL FOR UPDATE`, email).
			Scan(&u.ID, &u.Email, &u.DisplayName)
		if err != nil {
			return notFound(err)
		}
		var recent bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM password_resets WHERE user_id = $1 AND created_at > now() - $2::interval)`,
			u.ID, ResetThrottle.String()).Scan(&recent); err != nil {
			return err
		}
		if recent {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM password_resets WHERE user_id = $1 AND used_at IS NULL`, u.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO password_resets (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
			newID(), u.ID, tokenHash, expires)
		return err
	})
	return u, err
}

// PasswordResetEmail returns the email a usable reset token is for.
func (s *Store) PasswordResetEmail(ctx context.Context, tokenHash []byte) (string, error) {
	var email string
	err := s.pool.QueryRow(ctx,
		`SELECT u.email FROM password_resets r JOIN users u ON u.id = r.user_id
		 WHERE r.token_hash = $1 AND r.used_at IS NULL AND r.expires_at > now() AND u.is_active`, tokenHash).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrResetUsed
	}
	return email, err
}

// ResetPassword uses a token once: it sets the new password and ends every
// session the user has, so whoever knew the old password is signed out.
func (s *Store) ResetPassword(ctx context.Context, tokenHash []byte, passwordHash string) (User, error) {
	var u User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`UPDATE password_resets r SET used_at = now()
			 FROM users u
			 WHERE r.token_hash = $1 AND r.used_at IS NULL AND r.expires_at > now() AND u.id = r.user_id AND u.is_active
			 RETURNING u.id, u.email, u.display_name`, tokenHash).Scan(&u.ID, &u.Email, &u.DisplayName)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrResetUsed
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, u.ID, passwordHash); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, u.ID)
		return err
	})
	return u, err
}
