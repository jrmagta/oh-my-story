package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/httpapi"
)

func TestHealthz(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	r := h.client().must(200, "GET", "/healthz", nil)
	if r.body["database"] != "ok" {
		t.Errorf("reachable database: %s", r.raw)
	}

	// Nothing listens on port 1, so the ping fails.
	dead, err := db.Open(context.Background(), "postgres://oms:oms@127.0.0.1:1/oms?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer dead.Close()
	srv := httptest.NewServer(httpapi.New(config.Config{BaseURL: "http://x"}, dead, &recorder{}, nil))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]string
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != 503 || body["database"] != "unreachable" {
		t.Errorf("unreachable database: %d %v", res.StatusCode, body)
	}
}

func TestBootstrapLoginLogout(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	c := h.client()

	if r := c.must(200, "GET", "/api/setup", nil); r.body["needsBootstrap"] != true {
		t.Fatalf("fresh instance: %s", r.raw)
	}
	if r := c.do("POST", "/api/setup", map[string]string{"email": "a@example.com", "displayName": "A", "password": "short"}); r.errCode() != "bad_request" {
		t.Errorf("short password: %d %s", r.status, r.raw)
	}
	c.must(201, "POST", "/api/setup", map[string]string{"email": "Owner@Example.com", "displayName": "Olive", "password": ownerPassword})
	c.must(200, "GET", "/api/me", nil) // bootstrap signs you in
	if r := c.must(200, "GET", "/api/setup", nil); r.body["needsBootstrap"] != false {
		t.Errorf("after bootstrap: %s", r.raw)
	}
	if r := h.client().do("POST", "/api/setup", map[string]string{"email": "b@example.com", "displayName": "B", "password": ownerPassword}); r.status != 410 {
		t.Errorf("second bootstrap: %d %s", r.status, r.raw)
	}

	c.must(204, "POST", "/api/auth/logout", map[string]string{})
	if r := c.do("GET", "/api/me", nil); r.status != 401 {
		t.Errorf("me after logout: %d", r.status)
	}

	wrongPw := c.do("POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": "wrong-password-xx"})
	noUser := c.do("POST", "/api/auth/login", map[string]string{"email": "nobody@example.com", "password": "wrong-password-xx"})
	if wrongPw.status != 401 || noUser.status != 401 || wrongPw.raw != noUser.raw {
		t.Errorf("login failures must look identical: %d %s / %d %s", wrongPw.status, wrongPw.raw, noUser.status, noUser.raw)
	}
	// Email lookup is case-insensitive and ignores surrounding spaces, as setup does.
	c.must(200, "POST", "/api/auth/login", map[string]string{"email": " OWNER@example.com ", "password": ownerPassword})
	if r := c.must(200, "GET", "/api/me", nil); r.body["user"].(map[string]any)["displayName"] != "Olive" {
		t.Errorf("me: %s", r.raw)
	}

	// Signing in again ends the session the browser held before.
	old := c.sessionToken()
	c.must(200, "POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": ownerPassword})
	if c.sessionToken() == old {
		t.Fatal("login did not issue a new session token")
	}
	c.must(200, "GET", "/api/me", nil)
	if r := h.clientWithSession(old).do("GET", "/api/me", nil); r.status != 401 {
		t.Errorf("replaced session still works: %d", r.status)
	}
}

// When the invite email can't be sent, the invite is not kept, so a retry works.
func TestInviteEmailFailure(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()

	h.mail.setFail(errors.New("relay down"))
	if r := owner.do("POST", "/api/workspaces/acme/invites", map[string]string{"email": "c@example.com", "role": "viewer"}); r.status != 502 || r.errCode() != "email_failed" {
		t.Fatalf("invite with failing mail: %d %s", r.status, r.raw)
	}
	if r := owner.must(200, "GET", "/api/workspaces/acme/invites", nil); len(r.list) != 0 {
		t.Errorf("unsent invite kept: %s", r.raw)
	}

	h.mail.setFail(nil)
	owner.must(201, "POST", "/api/workspaces/acme/invites", map[string]string{"email": "c@example.com", "role": "viewer"})
}

func TestCSRFGuards(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	body := strings.NewReader(`{"email":"a@example.com","password":"x"}`)

	res, err := http.Post(h.base+"/api/auth/login", "text/plain", body)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("text/plain POST: %d, want 400", res.StatusCode)
	}

	req, _ := http.NewRequest("POST", h.base+"/api/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Errorf("cross-origin POST: %d, want 403", res.StatusCode)
	}
}

func TestInviteFlow(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()

	if r := owner.do("POST", "/api/workspaces/acme/invites", map[string]string{"email": "x@example.com", "role": "owner"}); r.status != 400 {
		t.Errorf("invite as owner: %d %s", r.status, r.raw)
	}
	owner.must(201, "POST", "/api/workspaces/acme/invites", map[string]string{"email": "bob@example.com", "role": "editor"})
	if r := owner.do("POST", "/api/workspaces/acme/invites", map[string]string{"email": "BOB@example.com", "role": "viewer"}); r.status != 409 {
		t.Errorf("second open invite: %d %s", r.status, r.raw)
	}
	h.mail.mu.Lock()
	subject := h.mail.msgs[0].Subject
	h.mail.mu.Unlock()
	if subject != "Olive Owner invited you to Acme on oh-my-story" {
		t.Errorf("subject %q", subject)
	}
	token := h.mail.inviteToken(t, "bob@example.com")

	bob := h.client()
	if r := bob.must(200, "GET", "/api/invites/"+token, nil); r.body["workspaceName"] != "Acme" || r.body["role"] != "editor" {
		t.Errorf("invite preview: %s", r.raw)
	}
	if r := bob.do("POST", "/api/invites/"+token+"/accept", map[string]string{}); r.status != 401 {
		t.Errorf("accept with no account and no password: %d %s", r.status, r.raw)
	}
	bob.must(201, "POST", "/api/invites/"+token+"/accept", map[string]string{"displayName": "Bob", "password": "bob-password-1234"})
	r := bob.must(200, "GET", "/api/me", nil)
	ws := r.body["workspaces"].([]any)
	if len(ws) != 1 || ws[0].(map[string]any)["slug"] != "acme" || ws[0].(map[string]any)["role"] != "editor" {
		t.Errorf("bob's workspaces: %s", r.raw)
	}

	if r := h.client().do("GET", "/api/invites/"+token, nil); r.status != 410 {
		t.Errorf("used invite: %d", r.status)
	}
	if r := h.client().do("POST", "/api/invites/"+token+"/accept", map[string]string{"displayName": "Eve", "password": "eve-password-1234"}); r.status != 410 {
		t.Errorf("reusing invite: %d %s", r.status, r.raw)
	}
	if r := owner.do("POST", "/api/workspaces/acme/invites", map[string]string{"email": "bob@example.com", "role": "viewer"}); r.status != 409 {
		t.Errorf("inviting an existing member: %d %s", r.status, r.raw)
	}

	// An invite for carol cannot be accepted by bob's session.
	owner.must(201, "POST", "/api/workspaces/acme/invites", map[string]string{"email": "carol@example.com", "role": "viewer"})
	carolToken := h.mail.inviteToken(t, "carol@example.com")
	if r := bob.do("POST", "/api/invites/"+carolToken+"/accept", map[string]string{}); r.status != 403 {
		t.Errorf("accepting someone else's invite: %d %s", r.status, r.raw)
	}

	// An existing user who is signed in accepts with an empty body.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Beta", "slug": "beta"})
	owner.must(201, "POST", "/api/workspaces/beta/invites", map[string]string{"email": "bob@example.com", "role": "viewer"})
	bob.must(200, "POST", "/api/invites/"+h.mail.inviteToken(t, "bob@example.com")+"/accept", map[string]string{})
	bob.must(200, "GET", "/api/workspaces/beta", nil)

	// Revoked invites stop working.
	owner.must(201, "POST", "/api/workspaces/acme/invites", map[string]string{"email": "dan@example.com", "role": "viewer"})
	open := owner.must(200, "GET", "/api/workspaces/acme/invites", nil).list
	var danID string
	for _, i := range open {
		if i.(map[string]any)["email"] == "dan@example.com" {
			danID = i.(map[string]any)["id"].(string)
		}
	}
	owner.must(204, "DELETE", "/api/workspaces/acme/invites/"+danID, map[string]string{})
	if r := h.client().do("GET", "/api/invites/"+h.mail.inviteToken(t, "dan@example.com"), nil); r.status != 404 {
		t.Errorf("revoked invite: %d", r.status)
	}
}

func TestRoleMatrix(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	clients := map[string]*client{"owner": owner}
	for _, r := range []string{"admin", "editor", "commenter", "viewer"} {
		clients[r], _ = h.join(owner, r+"@example.com", r)
	}
	// The outsider is a real user, but a member of a different workspace only.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	owner.must(201, "POST", "/api/workspaces/other/invites", map[string]string{"email": "outsider@example.com", "role": "admin"})
	outsider := h.client()
	outsider.must(201, "POST", "/api/invites/"+h.mail.inviteToken(t, "outsider@example.com")+"/accept",
		map[string]string{"displayName": "Out", "password": "outsider-password-1"})

	type check struct {
		name         string
		method, path string
		body         func(role string) any
		minRole      string // lowest role that succeeds
		okStatus     int
	}
	rank := map[string]int{"viewer": 1, "commenter": 2, "editor": 3, "admin": 4, "owner": 5}
	checks := []check{
		{"view workspace", "GET", "/api/workspaces/acme", nil, "viewer", 200},
		{"list members", "GET", "/api/workspaces/acme/members", nil, "viewer", 200},
		{"change settings", "PATCH", "/api/workspaces/acme", func(string) any { return map[string]string{"estimateUnit": "days"} }, "admin", 200},
		{"list invites", "GET", "/api/workspaces/acme/invites", nil, "admin", 200},
		{"invite", "POST", "/api/workspaces/acme/invites", func(r string) any {
			return map[string]string{"email": "new-by-" + r + "@example.com", "role": "viewer"}
		}, "admin", 201},
	}
	for _, c := range checks {
		for r, cl := range clients {
			var body any
			if c.body != nil {
				body = c.body(r)
			}
			got := cl.do(c.method, c.path, body).status
			want := 403
			if rank[r] >= rank[c.minRole] {
				want = c.okStatus
			}
			if got != want {
				t.Errorf("%s as %s: %d, want %d", c.name, r, got, want)
			}
		}
	}

	// Non-members see 404, not 403, so slugs do not leak.
	if r := outsider.do("GET", "/api/workspaces/acme", nil); r.status != 404 {
		t.Errorf("outsider: %d", r.status)
	}
	if r := h.client().do("GET", "/api/workspaces/acme", nil); r.status != 401 {
		t.Errorf("anonymous: %d", r.status)
	}

	// Only the owner can delete; check the others first.
	for _, r := range []string{"admin", "editor", "commenter", "viewer"} {
		if got := clients[r].do("DELETE", "/api/workspaces/acme", map[string]string{}).status; got != 403 {
			t.Errorf("delete as %s: %d, want 403", r, got)
		}
	}
	owner.must(204, "DELETE", "/api/workspaces/acme", map[string]string{})
	if r := clients["viewer"].do("GET", "/api/workspaces/acme", nil); r.status != 404 {
		t.Errorf("deleted workspace: %d", r.status)
	}
}

func TestOwnerRules(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	ownerID := owner.must(200, "GET", "/api/me", nil).body["user"].(map[string]any)["id"].(string)
	admin, adminID := h.join(owner, "admin@example.com", "admin")
	editor, editorID := h.join(owner, "editor@example.com", "editor")
	members := "/api/workspaces/acme/members/"

	// Admins manage non-owners only.
	admin.must(200, "PATCH", members+editorID, map[string]string{"role": "viewer"})
	if r := admin.do("PATCH", members+editorID, map[string]string{"role": "owner"}); r.status != 403 {
		t.Errorf("admin grants owner: %d %s", r.status, r.raw)
	}
	if r := admin.do("PATCH", members+ownerID, map[string]string{"role": "admin"}); r.status != 403 {
		t.Errorf("admin demotes owner: %d %s", r.status, r.raw)
	}
	if r := admin.do("DELETE", members+ownerID, map[string]string{}); r.status != 403 {
		t.Errorf("admin removes owner: %d %s", r.status, r.raw)
	}
	if r := editor.do("DELETE", members+adminID, map[string]string{}); r.status != 403 {
		t.Errorf("viewer removes admin: %d %s", r.status, r.raw)
	}

	// The last owner cannot be demoted or leave.
	if r := owner.do("PATCH", members+ownerID, map[string]string{"role": "admin"}); r.status != 409 {
		t.Errorf("sole owner demotes self: %d %s", r.status, r.raw)
	}
	if r := owner.do("DELETE", members+ownerID, map[string]string{}); r.status != 409 {
		t.Errorf("sole owner leaves: %d %s", r.status, r.raw)
	}

	// With a second owner, the first can leave.
	owner.must(200, "PATCH", members+adminID, map[string]string{"role": "owner"})
	owner.must(204, "DELETE", members+ownerID, map[string]string{})
	if r := owner.do("GET", "/api/workspaces/acme", nil); r.status != 404 {
		t.Errorf("after leaving: %d", r.status)
	}
	// Anyone can leave.
	editor.must(204, "DELETE", members+editorID, map[string]string{})

	if r := admin.do("PATCH", members+"not-a-uuid", map[string]string{"role": "viewer"}); r.status != 404 {
		t.Errorf("bad member id: %d", r.status)
	}
	if r := admin.do("PATCH", members+editorID, map[string]string{"role": "superuser"}); r.status != 400 {
		t.Errorf("unknown role: %d %s", r.status, r.raw)
	}
}

func TestWorkspaceValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	for _, body := range []map[string]string{
		{"name": "X", "slug": "Has Caps"},
		{"name": "X", "slug": "-leading"},
		{"name": "", "slug": "ok"},
	} {
		if r := owner.do("POST", "/api/workspaces", body); r.status != 400 {
			t.Errorf("create %v: %d", body, r.status)
		}
	}
	if r := owner.do("POST", "/api/workspaces", map[string]string{"name": "Dup", "slug": "acme"}); r.status != 409 {
		t.Errorf("duplicate slug: %d", r.status)
	}
	if r := owner.do("PATCH", "/api/workspaces/acme", map[string]string{"keyPrefix": "lower"}); r.status != 400 {
		t.Errorf("bad key prefix: %d", r.status)
	}
	r := owner.must(200, "PATCH", "/api/workspaces/acme", map[string]string{"keyPrefix": "ACME", "name": "Acme Corp"})
	if r.body["keyPrefix"] != "ACME" || r.body["name"] != "Acme Corp" || r.body["estimateUnit"] != "points" {
		t.Errorf("patch: %s", r.raw)
	}
	if r := owner.do("POST", "/api/workspaces", map[string]any{"name": "X", "slug": "x", "extra": 1}); r.status != 400 {
		t.Errorf("unknown field: %d", r.status)
	}
}
