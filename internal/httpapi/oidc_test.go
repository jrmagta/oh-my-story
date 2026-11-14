package httpapi_test

import (
	"html"
	"io"
	"net/url"
	"os"
	"regexp"
	"testing"

	"github.com/jrmagta/oh-my-story/internal/config"
)

var kcFormRE = regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)

// ssoLogin drives a real browser-style login through Keycloak's form and
// returns where the app finally redirected the browser.
func (c *client) ssoLogin(t *testing.T, username, password, next string) string {
	t.Helper()
	res, err := c.http.Get(c.h.base + "/auth/oidc/login?next=" + url.QueryEscape(next))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	m := kcFormRE.FindSubmatch(page)
	if m == nil {
		t.Fatalf("no Keycloak login form at %s (status %d)", res.Request.URL, res.StatusCode)
	}
	res, err = c.http.PostForm(html.UnescapeString(string(m[1])), url.Values{"username": {username}, "password": {password}, "credentialId": {""}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	final := res.Request.URL
	if final.Host != "localhost:18080" {
		t.Fatalf("login ended at %s, not back at the app", final)
	}
	return final.RequestURI()
}

// TestOIDCRoundTrip needs the compose Keycloak (make up). make test sets TEST_OIDC_ISSUER.
func TestOIDCRoundTrip(t *testing.T) {
	issuer := os.Getenv("TEST_OIDC_ISSUER")
	if issuer == "" {
		t.Skip("TEST_OIDC_ISSUER not set; run make up && make test")
	}
	h := newHarness(t, harnessOpts{
		listen: "127.0.0.1:18080",
		base:   "http://localhost:18080",
		oidc: &config.OIDC{Issuer: issuer, DiscoveryURL: issuer, Name: "Keycloak",
			ClientID: "oh-my-story", ClientSecret: "dev-only-client-secret"},
	})

	if r := h.client().must(200, "GET", "/api/auth/providers", nil); r.body["oidc"].(map[string]any)["name"] != "Keycloak" {
		t.Errorf("providers: %s", r.raw)
	}

	// Sign-up is invite-only, SSO included.
	if got := h.client().ssoLogin(t, "alice", "alice", "/invites/x"); got != "/login?error=not_invited&next=%2Finvites%2Fx" {
		t.Fatalf("uninvited SSO login ended at %q", got)
	}

	owner := h.bootstrap()
	owner.must(201, "POST", "/api/workspaces/acme/invites", map[string]string{"email": "alice@example.com", "role": "editor"})

	// The browser comes back to where it started, so an invite link survives SSO.
	alice := h.client()
	token := h.mail.inviteToken(t, "alice@example.com")
	if got := alice.ssoLogin(t, "alice", "alice", "/invites/"+token); got != "/invites/"+token {
		t.Fatalf("invited SSO login ended at %q", got)
	}
	me := alice.must(200, "GET", "/api/me", nil)
	if u := me.body["user"].(map[string]any); u["email"] != "alice@example.com" || u["displayName"] != "Alice Example" {
		t.Errorf("SSO user: %s", me.raw)
	}
	alice.must(200, "POST", "/api/invites/"+token+"/accept", map[string]string{})

	// A second login, from a new browser, finds the linked identity.
	again := h.client()
	if got := again.ssoLogin(t, "alice", "alice", "https://evil.example/"); got != "/" {
		t.Fatalf("second SSO login ended at %q", got)
	}
	if r := again.must(200, "GET", "/api/workspaces/acme", nil); r.body["role"] != "editor" {
		t.Errorf("alice in acme: %s", r.raw)
	}

	// SSO users have no password, so password login is refused.
	if r := again.do("POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "alice"}); r.status != 401 {
		t.Errorf("password login for SSO-only user: %d", r.status)
	}
}
