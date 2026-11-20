package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jrmagta/oh-my-story/internal/store"
)

func TestPasswordResetRules(t *testing.T) {
	f := newFixture(t)
	hour := time.Now().Add(time.Hour)
	reset := func(email string, token string) error {
		_, err := f.s.CreatePasswordReset(ctx, email, []byte(token), hour)
		return err
	}

	// SSO-only and deactivated accounts get no reset.
	if _, err := f.pool.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES (gen_random_uuid(), 'sso@example.com', 'SSO')`); err != nil {
		t.Fatal(err)
	}
	if err := reset("sso@example.com", "t-sso"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SSO-only account: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO users (id, email, display_name, password_hash, is_active) VALUES (gen_random_uuid(), 'gone@example.com', 'Gone', 'h', false)`); err != nil {
		t.Fatal(err)
	}
	if err := reset("gone@example.com", "t-gone"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deactivated account: %v", err)
	}

	// One per minute; a later one replaces the earlier, unused one.
	if err := reset("owner@example.com", "t-1"); err != nil {
		t.Fatal(err)
	}
	if err := reset("owner@example.com", "t-2"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second reset within a minute: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE password_resets SET created_at = now() - interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	if err := reset("owner@example.com", "t-3"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PasswordResetEmail(ctx, []byte("t-1")); !errors.Is(err, store.ErrResetUsed) {
		t.Errorf("replaced token still works: %v", err)
	}

	// An expired token is refused, for reading and for use.
	if _, err := f.pool.Exec(ctx, `UPDATE password_resets SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PasswordResetEmail(ctx, []byte("t-3")); !errors.Is(err, store.ErrResetUsed) {
		t.Errorf("expired token: %v", err)
	}
	if _, err := f.s.ResetPassword(ctx, []byte("t-3"), "new-hash"); !errors.Is(err, store.ErrResetUsed) {
		t.Errorf("expired token used: %v", err)
	}
}
