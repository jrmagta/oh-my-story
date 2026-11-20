package live_test

import (
	"context"
	"testing"
	"time"

	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/db/dbtest"
	"github.com/jrmagta/oh-my-story/internal/live"
	"github.com/jrmagta/oh-my-story/internal/store"
)

// On a first install the app can start before its schema exists (with
// CloudNativePG the migration Job runs after the app's pods are ready). The
// hub must not stop the server then, and must follow events once the
// migration lands.
func TestStartsBeforeTheSchema(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url := dbtest.NewDatabase(t)
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	hub := live.New(pool)
	hub.Start(ctx) // no schema yet: must return, not fail

	if _, err := db.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	s := store.New(pool)
	u, err := s.Bootstrap(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.CreateWorkspace(ctx, u.ID, "Acme", "acme")
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMap(ctx, u.ID, ws.ID, store.MapInput{Name: "M"})
	if err != nil {
		t.Fatal(err)
	}
	sub := hub.Subscribe(ctx, ws.ID, m.ID, live.User{ID: u.ID, Name: "Owner"})
	defer hub.Unsubscribe(sub)
	n, err := s.CreateNode(ctx, u.ID, ws.ID, m.ID, store.NodeInput{Level: "objective", Title: "A"}, store.Position{})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-sub.C():
			if msg.Event != nil && msg.Event.EntityID == n.ID {
				return
			}
		case <-deadline:
			t.Fatal("no event for the node after the schema arrived")
		}
	}
}
