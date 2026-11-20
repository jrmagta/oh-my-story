package db

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"

	"github.com/jrmagta/oh-my-story/internal/db/dbtest"
)

// Workspaces that exist before migration 0002 get the default workflow, with
// UUIDv7 ids like the ones the app generates.
func TestDefaultWorkflowBackfill(t *testing.T) {
	ctx := context.Background()
	url := dbtest.NewDatabase(t)
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	dir, _ := fs.Sub(migrations, "migrations")
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO workspaces (id, name, slug) VALUES ('00000000-0000-7000-8000-000000000002', 'Acme', 'acme')`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}

	rows, err := sqlDB.Query(`SELECT w.id, w.name, w.is_default, s.id, s.name, s.category
		FROM workflows w JOIN statuses s ON s.workflow_id = w.id ORDER BY s.rank`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var wf, st uuid.UUID
		var wfName, stName, category string
		var isDefault bool
		if err := rows.Scan(&wf, &wfName, &isDefault, &st, &stName, &category); err != nil {
			t.Fatal(err)
		}
		if wfName != "Default" || !isDefault || wf.Version() != 7 || st.Version() != 7 {
			t.Errorf("workflow %s %q default=%v, status id %s", wf, wfName, isDefault, st)
		}
		got = append(got, stName+"/"+category)
	}
	want := []string{"To do/todo", "In progress/doing", "Blocked/blocked", "Done/done"}
	if len(got) != len(want) {
		t.Fatalf("statuses = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statuses = %v, want %v", got, want)
			break
		}
	}
}
