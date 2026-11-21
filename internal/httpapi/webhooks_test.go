package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/store"
	"github.com/jrmagta/oh-my-story/internal/webhook"
)

// receiver is a webhook endpoint that records what it is sent and answers with status.
type receiver struct {
	*httptest.Server
	mu     sync.Mutex
	got    []received
	status int
}

type received struct {
	header http.Header
	body   []byte
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{status: 200}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, received{req.Header.Clone(), b})
		status := r.status
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) take() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.got
	r.got = nil
	return out
}

func (r *receiver) answer(status int) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
}

// drain sends until nothing is due.
func drain(t *testing.T, s *webhook.Sender) {
	t.Helper()
	for range 50 {
		n, err := s.RunOnce(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("deliveries kept coming due")
}

func TestWebhooks(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	other := buildMap(owner)
	editor, _ := h.join(owner, "ed@example.com", "editor")
	pool, err := db.Open(context.Background(), h.database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)
	sender := webhook.New(st, false)
	rcv := newReceiver(t)
	hooks := acme + "/webhooks"

	// Admins add webhooks; bad addresses and event names are refused; the secret comes back once.
	editor.must(403, "POST", hooks, map[string]any{"url": rcv.URL, "events": []string{"*"}})
	for _, bad := range []string{"ftp://example.com/x", "http://169.254.169.254/latest", "http://[fd00:ec2::254]/", "not a url", "http:///nohost"} {
		owner.must(400, "POST", hooks, map[string]any{"url": bad, "events": []string{"*"}})
	}
	owner.must(400, "POST", hooks, map[string]any{"url": rcv.URL, "events": []string{"step.explode"}})
	owner.must(400, "POST", hooks, map[string]any{"url": rcv.URL, "events": []string{}})
	made := owner.must(201, "POST", hooks, map[string]any{"url": rcv.URL, "events": []string{"step.create", "step.update"}}).body
	secret := str(made, "secret")
	id := str(made, "id")
	if !strings.HasPrefix(secret, "whsec_") {
		t.Errorf("secret = %q", secret)
	}
	if r := owner.must(200, "GET", hooks, nil); strings.Contains(r.raw, secret) || len(r.list) != 1 {
		t.Errorf("list = %s", r.raw)
	}

	// A change it asked for is sent once, signed, with the workspace, map, actor and event.
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Sign up", "activityId": b.act1}).body, "step", "id")
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Sign up fast"})
	owner.must(201, "POST", b.path+"/releases", map[string]any{"name": "Not asked for"})
	drain(t, sender)
	got := rcv.take()
	if len(got) != 2 {
		t.Fatalf("got %d deliveries, want step.create and step.update", len(got))
	}
	for _, g := range got {
		if g.header.Get("X-OMS-Signature") != webhook.Sign(secret, g.body) || g.header.Get("Content-Type") != "application/json" {
			t.Errorf("bad signature or type: %v", g.header)
		}
	}
	var body struct {
		ID        int64
		Type      string
		Workspace struct{ Slug string }
		Map       struct{ Name string }
		Actor     struct{ Name, Kind string }
		Event     struct {
			Entity, Action string
			After          map[string]any
		}
	}
	if err := json.Unmarshal(got[1].body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != "step.update" || got[1].header.Get("X-OMS-Event") != "step.update" || body.Workspace.Slug != "acme" ||
		body.Map.Name != "Onboarding" || body.Actor.Kind != "user" || body.Actor.Name == "" || body.Event.After["title"] != "Sign up fast" {
		t.Errorf("body = %s", got[1].body)
	}
	ds := owner.must(200, "GET", hooks+"/"+id+"/deliveries", nil).list
	if len(ds) != 2 || str(ds[0], "status") != "sent" || ds[0].(map[string]any)["lastStatusCode"] != 200.0 || str(ds[0], "type") != "step.update" {
		t.Errorf("deliveries = %v", ds)
	}

	// A step on two maps is one change, sent once.
	owner.must(201, "POST", other.path+"/placements", map[string]any{"stepId": step, "activityId": other.act1})
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Sign up faster"})
	drain(t, sender)
	if got := rcv.take(); len(got) != 1 {
		t.Errorf("linked step change sent %d times", len(got))
	}

	// A failing receiver is retried on the schedule: first after a minute.
	rcv.answer(500)
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Retry me"})
	drain(t, sender)
	d := owner.must(200, "GET", hooks+"/"+id+"/deliveries", nil).list[0].(map[string]any)
	next, _ := time.Parse(time.RFC3339Nano, d["nextAttemptAt"].(string))
	if d["status"] != "pending" || d["attempts"] != 1.0 || d["lastStatusCode"] != 500.0 || time.Until(next) < 50*time.Second || time.Until(next) > 70*time.Second {
		t.Errorf("after a failure = %v", d)
	}
	// With the waits shortened, it is tried six times in all, then given up.
	fast := webhook.New(st, false).WithBackoff([]time.Duration{0, 0, 0, 0, 0})
	if _, err := pool.Exec(context.Background(), `UPDATE webhook_deliveries SET next_attempt_at = now() WHERE status = 'pending'`); err != nil {
		t.Fatal(err)
	}
	drain(t, fast)
	d = owner.must(200, "GET", hooks+"/"+id+"/deliveries", nil).list[0].(map[string]any)
	if d["status"] != "failed" || d["attempts"] != 6.0 || d["nextAttemptAt"] != nil || !strings.Contains(str(d, "lastError"), "500") {
		t.Errorf("after giving up = %v", d)
	}
	if n := len(rcv.take()); n != 6 {
		t.Errorf("receiver saw %d tries, want 6", n)
	}
	rcv.answer(200)

	// Two senders on one database send each delivery once.
	for i := range 30 {
		owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Busy " + string(rune('A'+i))})
	}
	var wg sync.WaitGroup
	for range 2 {
		s := webhook.New(st, false)
		wg.Go(func() {
			for {
				if n, err := s.RunOnce(context.Background()); err != nil || n == 0 {
					return
				}
			}
		})
	}
	wg.Wait()
	seen := map[string]int{}
	for _, g := range rcv.take() {
		seen[g.header.Get("X-OMS-Delivery")]++
	}
	if len(seen) != 30 {
		t.Errorf("sent %d distinct deliveries, want 30", len(seen))
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("delivery %s sent %d times", k, n)
		}
	}

	// A ping tests the webhook; an inactive webhook gets nothing.
	ping := owner.must(201, "POST", hooks+"/"+id+"/test", map[string]any{}).body
	if str(ping, "type") != "ping" || str(ping, "status") != "pending" {
		t.Errorf("ping = %v", ping)
	}
	drain(t, sender)
	if got := rcv.take(); len(got) != 1 || !strings.Contains(string(got[0].body), `"type":"ping"`) || !strings.Contains(string(got[0].body), `"event":null`) {
		t.Errorf("ping sent = %v", got)
	}
	owner.must(200, "PATCH", hooks+"/"+id, map[string]any{"active": false})
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Quiet"})
	drain(t, sender)
	if got := rcv.take(); len(got) != 0 {
		t.Errorf("inactive webhook got %d", len(got))
	}
	owner.must(400, "PATCH", hooks+"/"+id, map[string]any{"events": []string{"nope"}})

	// With private addresses blocked, a local receiver is refused when connecting.
	owner.must(200, "PATCH", hooks+"/"+id, map[string]any{"active": true, "events": []string{"*"}})
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Blocked"})
	drain(t, webhook.New(st, true))
	d = owner.must(200, "GET", hooks+"/"+id+"/deliveries", nil).list[0].(map[string]any)
	if d["status"] != "pending" || !strings.Contains(str(d, "lastError"), "cannot post") {
		t.Errorf("blocked delivery = %v", d)
	}
	if got := rcv.take(); len(got) != 0 {
		t.Errorf("blocked sender reached the receiver %d times", len(got))
	}

	// Other workspaces cannot see or change it; deleting it stops it.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	owner.must(404, "GET", "/api/workspaces/other/webhooks/"+id+"/deliveries", nil)
	owner.must(404, "DELETE", "/api/workspaces/other/webhooks/"+id, nil)
	owner.must(204, "DELETE", hooks+"/"+id, nil)
	owner.must(404, "GET", hooks+"/"+id+"/deliveries", nil)
}
