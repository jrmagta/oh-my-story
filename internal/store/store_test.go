package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/db/dbtest"
	"github.com/jrmagta/oh-my-story/internal/role"
	"github.com/jrmagta/oh-my-story/internal/store"
)

func newStore(t *testing.T) *store.Store {
	s, _ := newStoreWithPool(t)
	return s
}

// newStoreWithPool also returns the pool, for state the store has no method for yet.
func newStoreWithPool(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	url := dbtest.NewDatabase(t)
	if _, err := db.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool), pool
}

// Concurrent bootstraps must create exactly one user.
func TestBootstrapRace(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Bootstrap(context.Background(), "u"+string(rune('a'+i))+"@example.com", "U", "hash")
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, store.ErrBootstrapped):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d bootstraps succeeded, want 1", ok)
	}
}

// Two owners demoting each other at once must not leave the workspace ownerless.
func TestLastOwnerRace(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a, err := s.Bootstrap(ctx, "a@example.com", "A", "hash")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.CreateWorkspace(ctx, a.ID, "W", "w", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateInvite(ctx, ws.ID, "b@example.com", role.Admin, []byte("tok"), a.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.AcceptInvite(ctx, []byte("tok"), nil, &store.NewUser{DisplayName: "B", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	allow := func(role.Role) error { return nil }
	if _, err := s.SetRole(ctx, ws.ID, b.ID, role.Owner, allow); err != nil {
		t.Fatal(err)
	}

	for range 20 {
		var wg sync.WaitGroup
		var errA, errB error
		wg.Add(2)
		go func() { defer wg.Done(); _, errA = s.SetRole(ctx, ws.ID, a.ID, role.Admin, allow) }()
		go func() { defer wg.Done(); _, errB = s.SetRole(ctx, ws.ID, b.ID, role.Admin, allow) }()
		wg.Wait()

		members, err := s.Members(ctx, ws.ID)
		if err != nil {
			t.Fatal(err)
		}
		owners := 0
		for _, m := range members {
			if m.Role == role.Owner {
				owners++
			}
		}
		if owners != 1 || (errA == nil) == (errB == nil) {
			t.Fatalf("owners=%d errA=%v errB=%v; want exactly one demotion to succeed", owners, errA, errB)
		}
		// Restore two owners for the next round.
		for _, m := range members {
			if m.Role != role.Owner {
				if _, err := s.SetRole(ctx, ws.ID, m.UserID, role.Owner, allow); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// Accepting the same invite concurrently must succeed once.
func TestInviteSingleUse(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a, _ := s.Bootstrap(ctx, "a@example.com", "A", "hash")
	ws, _ := s.CreateWorkspace(ctx, a.ID, "W", "w", "")
	if _, err := s.CreateInvite(ctx, ws.ID, "c@example.com", role.Viewer, []byte("tok"), a.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = s.AcceptInvite(ctx, []byte("tok"), nil, &store.NewUser{DisplayName: "C", PasswordHash: "h"})
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, store.ErrInviteUsed) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d accepts succeeded, want 1", ok)
	}
}

// Concurrent first-time SSO callbacks for one invited person must all resolve
// to the same new user, not fail on a unique constraint.
func TestOIDCFirstLoginRace(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a, _ := s.Bootstrap(ctx, "a@example.com", "A", "hash")
	ws, _ := s.CreateWorkspace(ctx, a.ID, "W", "w", "")
	for round := range 20 {
		email, sub := fmt.Sprintf("c%d@example.com", round), fmt.Sprintf("sub-c%d", round)
		if _, err := s.CreateInvite(ctx, ws.ID, email, role.Viewer, []byte(email), a.ID, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		users := make([]store.User, 8)
		errs := make([]error, len(users))
		for i := range users {
			wg.Add(1)
			go func() {
				defer wg.Done()
				users[i], errs[i] = s.ResolveOIDCUser(ctx, "https://idp", sub, store.Profile{Email: email, DisplayName: "C"})
			}()
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d, callback %d: %v", round, i, err)
			}
			if users[i].ID != users[0].ID {
				t.Fatalf("round %d: callbacks resolved to different users", round)
			}
		}
	}
}

// A deactivated user is refused by SSO whether or not their identity is linked yet,
// even with an open invite for their email.
func TestOIDCDeactivatedUser(t *testing.T) {
	ctx := context.Background()
	s, pool := newStoreWithPool(t)
	a, _ := s.Bootstrap(ctx, "a@example.com", "A", "hash")
	ws, _ := s.CreateWorkspace(ctx, a.ID, "W", "w", "")
	if _, err := s.CreateInvite(ctx, ws.ID, "d@example.com", role.Viewer, []byte("tok"), a.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	d, err := s.ResolveOIDCUser(ctx, "https://idp", "sub-d", store.Profile{Email: "d@example.com", DisplayName: "D"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_active = false WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"sub-d", "sub-new"} {
		if _, err := s.ResolveOIDCUser(ctx, "https://idp", sub, store.Profile{Email: "d@example.com", DisplayName: "D"}); !errors.Is(err, store.ErrDisabled) {
			t.Errorf("%s: err = %v, want ErrDisabled", sub, err)
		}
	}
}

func TestKeyCandidates(t *testing.T) {
	for name, want := range map[string]string{
		"Cheshire Cat":         "CHC",
		"Acme":                 "ACM",
		"Mad Hatter Tea Party": "MHT",
		"Oh my story":          "OMS",
		"2026 plans":           "PLA",
		"X":                    "X",
		"Q 1":                  "Q1",
		"!!!":                  "WS",
	} {
		c := store.KeyCandidates(name)
		if c[0] != want {
			t.Errorf("KeyCandidates(%q)[0] = %q, want %q", name, c[0], want)
		}
		seen := map[string]bool{}
		for _, k := range c {
			if seen[k] || k[0] < 'A' || k[0] > 'Z' || len(k) > 10 {
				t.Errorf("KeyCandidates(%q) has bad or repeated %q", name, k)
			}
			seen[k] = true
		}
	}
	// Alternatives keep the first letter and draw on the rest of the name.
	if c := store.KeyCandidates("Cheshire Cat"); c[1] != "CHE" || c[2] != "CHS" {
		t.Errorf("alternatives = %v", c[:5])
	}
}

func TestProjectKeysAreUnique(t *testing.T) {
	s := newStore(t)
	u := must[store.User](t)(s.Bootstrap(ctx, "a@example.com", "A", "hash"))
	ws1 := must[store.Workspace](t)(s.CreateWorkspace(ctx, u.ID, "Cheshire Cat", "cat", ""))
	if ws1.KeyPrefix != "CHC" {
		t.Errorf("first key = %q", ws1.KeyPrefix)
	}
	// The same name suggests the next free key.
	if k := must[string](t)(s.SuggestKey(ctx, "Cheshire Cat")); k != "CHE" {
		t.Errorf("suggestion after CHC is taken = %q", k)
	}
	ws2 := must[store.Workspace](t)(s.CreateWorkspace(ctx, u.ID, "Cheshire Cat", "cat-2", ""))
	if ws2.KeyPrefix != "CHE" {
		t.Errorf("second key = %q", ws2.KeyPrefix)
	}
	if _, err := s.CreateWorkspace(ctx, u.ID, "Other", "other", "CHC"); !errors.Is(err, store.ErrKeyTaken) {
		t.Errorf("taken key: %v", err)
	}
	if _, err := s.CreateWorkspace(ctx, u.ID, "Other", "cat", "OTH"); !errors.Is(err, store.ErrSlugTaken) {
		t.Errorf("taken slug: %v", err)
	}
	k := "CHC"
	if _, err := s.UpdateWorkspace(ctx, ws2.ID, store.WorkspacePatch{KeyPrefix: &k}); !errors.Is(err, store.ErrKeyTaken) {
		t.Errorf("renaming onto a taken key: %v", err)
	}
	if taken := must[bool](t)(s.KeyTaken(ctx, "CHC")); !taken {
		t.Error("CHC should be taken")
	}
}
