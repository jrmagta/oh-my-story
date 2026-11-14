package db_test

import (
	"context"
	"testing"

	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/db/dbtest"
)

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	url := dbtest.NewDatabase(t)

	n, err := db.Migrate(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("first run applied %d migrations, want 1", n)
	}
	n, err = db.Migrate(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second run applied %d migrations, want 0", n)
	}
}

func TestSchemaConstraints(t *testing.T) {
	ctx := context.Background()
	url := dbtest.NewDatabase(t)
	if _, err := db.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const (
		user = "00000000-0000-7000-8000-000000000001"
		ws   = "00000000-0000-7000-8000-000000000002"
	)
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustExec(`INSERT INTO users (id, email, display_name) VALUES ($1, 'Ann@Example.com', 'Ann')`, user)
	mustExec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'Acme', 'acme')`, ws)
	mustExec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, ws, user)

	rejected := map[string]struct {
		sql  string
		args []any
	}{
		"email is case-insensitive unique": {`INSERT INTO users (id, email, display_name) VALUES ('00000000-0000-7000-8000-0000000000aa', 'ann@example.com', 'Dup')`, nil},
		"unknown role":                     {`UPDATE workspace_members SET role = 'superuser' WHERE user_id = $1`, []any{user}},
		"lowercase key prefix":             {`UPDATE workspaces SET key_prefix = 'story' WHERE id = $1`, []any{ws}},
		"invite cannot grant owner": {`INSERT INTO invites (id, workspace_id, email, role, token_hash, invited_by, expires_at)
			VALUES ('00000000-0000-7000-8000-0000000000bb', $1, 'bo@example.com', 'owner', 'x', $2, now())`, []any{ws, user}},
	}
	for name, c := range rejected {
		if _, err := pool.Exec(ctx, c.sql, c.args...); err == nil {
			t.Errorf("%s: statement succeeded, want constraint error", name)
		}
	}

	invite := `INSERT INTO invites (id, workspace_id, email, role, token_hash, invited_by, expires_at, accepted_at)
		VALUES ($1, $2, 'bo@example.com', 'editor', $3, $4, now() + interval '7 days', $5)`
	mustExec(invite, "00000000-0000-7000-8000-0000000000c1", ws, []byte("t1"), user, nil)
	if _, err := pool.Exec(ctx, invite, "00000000-0000-7000-8000-0000000000c2", ws, []byte("t2"), user, nil); err == nil {
		t.Error("second open invite for the same email succeeded, want unique violation")
	}
	mustExec(`UPDATE invites SET accepted_at = now() WHERE id = '00000000-0000-7000-8000-0000000000c1'`)
	mustExec(invite, "00000000-0000-7000-8000-0000000000c3", ws, []byte("t3"), user, nil)
}
