package httpapi_test

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var resetLinkRE = regexp.MustCompile(`/reset/([A-Za-z0-9_-]{43})`)

// resetTokens waits briefly for reset emails to addr, then returns the tokens
// sent so far. The email goes out after the reply, so it can lag a little.
func (r *recorder) resetTokens(addr string, want int) []string {
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.mu.Lock()
		var out []string
		for _, m := range r.msgs {
			if strings.EqualFold(m.To, addr) {
				if t := resetLinkRE.FindStringSubmatch(m.Body); t != nil {
					out = append(out, t[1])
				}
			}
		}
		r.mu.Unlock()
		if len(out) >= want || time.Now().After(deadline) {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPasswordReset(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.bootstrap()
	elsewhere := h.client()
	elsewhere.must(200, "POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": ownerPassword})

	// Unknown addresses get the same answer and no email.
	anon := h.client()
	anon.must(204, "POST", "/api/auth/reset", map[string]string{"email": "nobody@example.com"})
	anon.must(400, "POST", "/api/auth/reset", map[string]string{"email": "not an email"})

	anon.must(204, "POST", "/api/auth/reset", map[string]string{"email": "Owner@Example.com"})
	tokens := h.mail.resetTokens("owner@example.com", 1)
	if len(tokens) != 1 {
		t.Fatalf("reset emails: %v", tokens)
	}
	if got := h.mail.resetTokens("nobody@example.com", 1); len(got) != 0 {
		t.Errorf("an unknown address got mail: %v", got)
	}
	// Asking again at once sends nothing more.
	anon.must(204, "POST", "/api/auth/reset", map[string]string{"email": "owner@example.com"})
	if got := h.mail.resetTokens("owner@example.com", 2); len(got) != 1 {
		t.Errorf("a second request within the throttle sent mail: %v", got)
	}

	link := "/api/auth/reset/" + tokens[0]
	if r := anon.must(200, "GET", link, nil); r.body["email"] != "owner@example.com" {
		t.Errorf("reset preview = %s", r.raw)
	}
	anon.must(400, "POST", link, map[string]string{"password": "short"})
	anon.must(200, "POST", link, map[string]string{"password": "a-brand-new-password"})

	// This browser is signed in; the other session has ended.
	anon.must(200, "GET", "/api/me", nil)
	elsewhere.must(401, "GET", "/api/me", nil)
	h.client().must(401, "POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": ownerPassword})
	h.client().must(200, "POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": "a-brand-new-password"})

	// The link works once.
	anon.must(410, "GET", link, nil)
	anon.must(410, "POST", link, map[string]string{"password": "another-new-password"})
	anon.must(410, "GET", "/api/auth/reset/not-a-real-token", nil)
}
