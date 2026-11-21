// Package store is the only code that issues SQL. Invariants that span rows
// (one bootstrap, at least one owner, single-use invites) are enforced here, in transactions.
package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jrmagta/oh-my-story/internal/role"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	// ErrForbidden is a change the caller's role allows in general but not on
	// this row, such as editing someone else's comment.
	ErrForbidden     = errors.New("forbidden")
	ErrLastOwner     = errors.New("a workspace must keep at least one owner")
	ErrBootstrapped  = errors.New("instance already has users")
	ErrInviteUsed    = errors.New("invite already used or expired")
	ErrAlreadyMember = errors.New("already a member")
	ErrWrongUser     = errors.New("invite is for a different email")
	ErrDisabled      = errors.New("account is deactivated")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
}

type Workspace struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	KeyPrefix    string    `json:"keyPrefix"`
	EstimateUnit string    `json:"estimateUnit"`
}

type Membership struct {
	Workspace
	Role role.Role `json:"role"`
}

type Member struct {
	UserID      uuid.UUID `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        role.Role `json:"role"`
}

type Invite struct {
	ID            uuid.UUID `json:"id"`
	WorkspaceID   uuid.UUID `json:"-"`
	WorkspaceName string    `json:"workspaceName"`
	Email         string    `json:"email"`
	Role          role.Role `json:"role"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Accepted      bool      `json:"-"`
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ---- users ----

func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&ok)
	return ok, err
}

// Bootstrap creates the first user. It fails with ErrBootstrapped once any user exists.
func (s *Store) Bootstrap(ctx context.Context, email, name, passwordHash string) (User, error) {
	var u User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Serialise concurrent bootstrap attempts.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('oms.bootstrap'))`); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrBootstrapped
		}
		var err error
		u, err = insertUser(ctx, tx, email, name, &passwordHash)
		return err
	})
	return u, err
}

func insertUser(ctx context.Context, tx pgx.Tx, email, name string, passwordHash *string) (User, error) {
	u := User{ID: newID()}
	err := tx.QueryRow(ctx,
		`INSERT INTO users (id, email, display_name, password_hash) VALUES ($1, $2, $3, $4)
		 RETURNING email, display_name`, u.ID, email, name, passwordHash).Scan(&u.Email, &u.DisplayName)
	if isUnique(err) {
		return User{}, ErrConflict
	}
	return u, err
}

// UserForLogin returns the user and password hash (empty for SSO-only users).
func (s *Store) UserForLogin(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, display_name, password_hash FROM users WHERE email = $1 AND is_active`, email).
		Scan(&u.ID, &u.Email, &u.DisplayName, &hash)
	if err != nil {
		return User{}, "", notFound(err)
	}
	if hash == nil {
		return u, "", nil
	}
	return u, *hash, nil
}

// ---- sessions ----

// CreateSession stores a new session. It ends the session the browser held
// before (replaces, nil if none) and deletes the user's expired sessions.
func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, expires time.Time, userAgent string, replaces []byte) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`DELETE FROM sessions WHERE token_hash = $1 OR (user_id = $2 AND expires_at <= now())`,
			replaces, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO sessions (id, user_id, token_hash, expires_at, user_agent) VALUES ($1, $2, $3, $4, $5)`,
			newID(), userID, tokenHash, expires, userAgent)
		return err
	})
}

func (s *Store) UserBySession(ctx context.Context, tokenHash []byte) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT u.id, u.email, u.display_name FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1 AND s.expires_at > now() AND u.is_active`, tokenHash).
		Scan(&u.ID, &u.Email, &u.DisplayName)
	return u, notFound(err)
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// ---- SSO identities ----

// ResolveOIDCUser finds or creates the user for a verified OIDC identity:
// a linked identity wins; otherwise a user with the same email is linked;
// otherwise a user is created only if an open invite exists for the email.
// A deactivated user gets ErrDisabled.
func (s *Store) ResolveOIDCUser(ctx context.Context, issuer, subject, email, name string) (User, error) {
	var u User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Two first-time callbacks for one person would both try to create the
		// user or the identity. Take the locks in a fixed order so one waits.
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('oms.oidc.subject:' || $1 || ' ' || $2)),
			        pg_advisory_xact_lock(hashtext('oms.oidc.email:' || lower($3)))`,
			issuer, subject, email); err != nil {
			return err
		}

		var active bool
		err := tx.QueryRow(ctx,
			`SELECT u.id, u.email, u.display_name, u.is_active FROM identities i JOIN users u ON u.id = i.user_id
			 WHERE i.issuer = $1 AND i.subject = $2`, issuer, subject).
			Scan(&u.ID, &u.Email, &u.DisplayName, &active)
		if err == nil && !active {
			return ErrDisabled
		}
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		err = tx.QueryRow(ctx, `SELECT id, email, display_name, is_active FROM users WHERE email = $1`, email).
			Scan(&u.ID, &u.Email, &u.DisplayName, &active)
		switch {
		case err == nil && !active:
			return ErrDisabled
		case errors.Is(err, pgx.ErrNoRows):
			var invited bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM invites WHERE email = $1 AND accepted_at IS NULL AND expires_at > now())`,
				email).Scan(&invited); err != nil {
				return err
			}
			if !invited {
				return ErrNotFound
			}
			if u, err = insertUser(ctx, tx, email, name, nil); err != nil {
				return err
			}
		case err != nil:
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO identities (id, user_id, issuer, subject) VALUES ($1, $2, $3, $4)`,
			newID(), u.ID, issuer, subject)
		return err
	})
	return u, err
}

// ---- workspaces ----

// CreateWorkspace makes the owner its first member. key is its project key;
// empty takes SuggestKey(name). A taken slug or key is ErrSlugTaken or
// ErrKeyTaken.
func (s *Store) CreateWorkspace(ctx context.Context, owner uuid.UUID, name, slug, key string) (Workspace, error) {
	w := Workspace{ID: newID()}
	if key == "" {
		var err error
		if key, err = s.SuggestKey(ctx, name); err != nil {
			return w, err
		}
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO workspaces (id, name, slug, key_prefix) VALUES ($1, $2, $3, $4)
			 RETURNING name, slug, key_prefix, estimate_unit`, w.ID, name, slug, key).
			Scan(&w.Name, &w.Slug, &w.KeyPrefix, &w.EstimateUnit)
		if err != nil {
			return workspaceConflict(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, w.ID, owner); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `SELECT oms_seed_workflow($1, 'Default', true)`, w.ID)
		return err
	})
	return w, err
}

const membershipCols = `w.id, w.name, w.slug, w.key_prefix, w.estimate_unit, m.role`

func scanMembership(row pgx.Row) (Membership, error) {
	var m Membership
	var r string
	if err := row.Scan(&m.ID, &m.Name, &m.Slug, &m.KeyPrefix, &m.EstimateUnit, &r); err != nil {
		return Membership{}, notFound(err)
	}
	var err error
	m.Role, err = role.Parse(r)
	return m, err
}

func (s *Store) Memberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+membershipCols+` FROM workspace_members m JOIN workspaces w ON w.id = m.workspace_id
		 WHERE m.user_id = $1 ORDER BY w.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Membership returns ErrNotFound when the workspace does not exist or the user is not a member.
func (s *Store) Membership(ctx context.Context, userID uuid.UUID, slug string) (Membership, error) {
	return scanMembership(s.pool.QueryRow(ctx,
		`SELECT `+membershipCols+` FROM workspace_members m JOIN workspaces w ON w.id = m.workspace_id
		 WHERE m.user_id = $1 AND w.slug = $2`, userID, slug))
}

type WorkspacePatch struct {
	Name, KeyPrefix, EstimateUnit *string
}

func (s *Store) UpdateWorkspace(ctx context.Context, id uuid.UUID, p WorkspacePatch) (Workspace, error) {
	w := Workspace{ID: id}
	err := s.pool.QueryRow(ctx,
		`UPDATE workspaces SET
		   name = COALESCE($2, name), key_prefix = COALESCE($3, key_prefix),
		   estimate_unit = COALESCE($4, estimate_unit), updated_at = now()
		 WHERE id = $1 RETURNING name, slug, key_prefix, estimate_unit`,
		id, p.Name, p.KeyPrefix, p.EstimateUnit).Scan(&w.Name, &w.Slug, &w.KeyPrefix, &w.EstimateUnit)
	return w, workspaceConflict(notFound(err))
}

func (s *Store) DeleteWorkspace(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM workspaces WHERE id = $1`, id)
	return err
}

// ---- members ----

func (s *Store) Members(ctx context.Context, workspaceID uuid.UUID) ([]Member, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.email, u.display_name, m.role FROM workspace_members m JOIN users u ON u.id = m.user_id
		 WHERE m.workspace_id = $1 ORDER BY u.display_name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		var r string
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &r); err != nil {
			return nil, err
		}
		if m.Role, err = role.Parse(r); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// changeMember locks the workspace, loads the target's current role, and runs
// apply. It refuses any change that would leave the workspace without an owner.
func (s *Store) changeMember(ctx context.Context, workspaceID, userID uuid.UUID, newRole *role.Role,
	allow func(current role.Role) error) (Member, error) {
	var m Member
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM workspaces WHERE id = $1 FOR UPDATE`, workspaceID); err != nil {
			return err
		}
		var r string
		err := tx.QueryRow(ctx,
			`SELECT u.id, u.email, u.display_name, m.role FROM workspace_members m JOIN users u ON u.id = m.user_id
			 WHERE m.workspace_id = $1 AND m.user_id = $2`, workspaceID, userID).
			Scan(&m.UserID, &m.Email, &m.DisplayName, &r)
		if err != nil {
			return notFound(err)
		}
		current, err := role.Parse(r)
		if err != nil {
			return err
		}
		if err := allow(current); err != nil {
			return err
		}
		losingOwner := current == role.Owner && (newRole == nil || *newRole != role.Owner)
		if losingOwner {
			var owners int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM workspace_members WHERE workspace_id = $1 AND role = 'owner'`, workspaceID).
				Scan(&owners); err != nil {
				return err
			}
			if owners <= 1 {
				return ErrLastOwner
			}
		}
		if newRole == nil {
			_, err = tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
			return err
		}
		m.Role = *newRole
		_, err = tx.Exec(ctx, `UPDATE workspace_members SET role = $3 WHERE workspace_id = $1 AND user_id = $2`,
			workspaceID, userID, newRole.String())
		return err
	})
	return m, err
}

// SetRole changes a member's role. allow sees the member's current role and can veto.
func (s *Store) SetRole(ctx context.Context, workspaceID, userID uuid.UUID, r role.Role, allow func(current role.Role) error) (Member, error) {
	return s.changeMember(ctx, workspaceID, userID, &r, allow)
}

func (s *Store) RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID, allow func(current role.Role) error) error {
	_, err := s.changeMember(ctx, workspaceID, userID, nil, allow)
	return err
}

// ---- invites ----

func (s *Store) CreateInvite(ctx context.Context, workspaceID uuid.UUID, email string, r role.Role,
	tokenHash []byte, invitedBy uuid.UUID, expires time.Time) (Invite, error) {
	inv := Invite{ID: newID(), WorkspaceID: workspaceID, Role: r, ExpiresAt: expires}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var member bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM workspace_members m JOIN users u ON u.id = m.user_id
			 WHERE m.workspace_id = $1 AND u.email = $2)`, workspaceID, email).Scan(&member); err != nil {
			return err
		}
		if member {
			return ErrAlreadyMember
		}
		// Replace an expired open invite rather than tripping the one-open-invite index.
		if _, err := tx.Exec(ctx,
			`DELETE FROM invites WHERE workspace_id = $1 AND email = $2 AND accepted_at IS NULL AND expires_at <= now()`,
			workspaceID, email); err != nil {
			return err
		}
		err := tx.QueryRow(ctx,
			`INSERT INTO invites (id, workspace_id, email, role, token_hash, invited_by, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 RETURNING email, (SELECT name FROM workspaces WHERE id = $2)`,
			inv.ID, workspaceID, email, r.String(), tokenHash, invitedBy, expires).Scan(&inv.Email, &inv.WorkspaceName)
		if isUnique(err) {
			return ErrConflict
		}
		return err
	})
	return inv, err
}

const inviteCols = `i.id, i.workspace_id, w.name, i.email, i.role, i.expires_at, i.accepted_at IS NOT NULL`

func scanInvite(row pgx.Row) (Invite, error) {
	var inv Invite
	var r string
	if err := row.Scan(&inv.ID, &inv.WorkspaceID, &inv.WorkspaceName, &inv.Email, &r, &inv.ExpiresAt, &inv.Accepted); err != nil {
		return Invite{}, notFound(err)
	}
	var err error
	inv.Role, err = role.Parse(r)
	return inv, err
}

func (s *Store) OpenInvites(ctx context.Context, workspaceID uuid.UUID) ([]Invite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+inviteCols+` FROM invites i JOIN workspaces w ON w.id = i.workspace_id
		 WHERE i.workspace_id = $1 AND i.accepted_at IS NULL AND i.expires_at > now() ORDER BY i.created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Store) RevokeInvite(ctx context.Context, workspaceID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM invites WHERE workspace_id = $1 AND id = $2 AND accepted_at IS NULL`, workspaceID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// InviteByToken returns the invite, or ErrInviteUsed if it was accepted or has expired.
func (s *Store) InviteByToken(ctx context.Context, tokenHash []byte) (Invite, error) {
	inv, err := scanInvite(s.pool.QueryRow(ctx,
		`SELECT `+inviteCols+` FROM invites i JOIN workspaces w ON w.id = i.workspace_id WHERE i.token_hash = $1`, tokenHash))
	if err != nil {
		return Invite{}, err
	}
	if inv.Accepted || time.Now().After(inv.ExpiresAt) {
		return Invite{}, ErrInviteUsed
	}
	return inv, nil
}

// AcceptInvite adds the member and marks the invite used. With newUser set, it
// first creates that local account for the invite's email.
func (s *Store) AcceptInvite(ctx context.Context, tokenHash []byte, existing *User, newUser *NewUser) (User, Workspace, error) {
	var u User
	var w Workspace
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		inv, err := scanInvite(tx.QueryRow(ctx,
			`SELECT `+inviteCols+` FROM invites i JOIN workspaces w ON w.id = i.workspace_id
			 WHERE i.token_hash = $1 FOR UPDATE OF i`, tokenHash))
		if err != nil {
			return err
		}
		if inv.Accepted || time.Now().After(inv.ExpiresAt) {
			return ErrInviteUsed
		}
		switch {
		case existing != nil:
			if !strings.EqualFold(existing.Email, inv.Email) {
				return ErrWrongUser
			}
			u = *existing
		case newUser != nil:
			if u, err = insertUser(ctx, tx, inv.Email, newUser.DisplayName, &newUser.PasswordHash); err != nil {
				return err // ErrConflict: an account already exists, so sign in first
			}
		default:
			return errors.New("AcceptInvite needs a user")
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			inv.WorkspaceID, u.ID, inv.Role.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrAlreadyMember
		}
		if _, err := tx.Exec(ctx, `UPDATE invites SET accepted_at = now() WHERE id = $1`, inv.ID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT id, name, slug, key_prefix, estimate_unit FROM workspaces WHERE id = $1`, inv.WorkspaceID).
			Scan(&w.ID, &w.Name, &w.Slug, &w.KeyPrefix, &w.EstimateUnit)
	})
	return u, w, err
}

type NewUser struct {
	DisplayName  string
	PasswordHash string
}
