package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/db/dbtest"
	"github.com/jrmagta/oh-my-story/internal/httpapi"
	"github.com/jrmagta/oh-my-story/internal/live"
	"github.com/jrmagta/oh-my-story/internal/mail"
)

// recorder is the test mail.Sender: it keeps every message, or fails when told to.
type recorder struct {
	mu   sync.Mutex
	msgs []mail.Message
	fail error
}

func (r *recorder) Send(_ context.Context, m mail.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.msgs = append(r.msgs, m)
	return nil
}

func (r *recorder) setFail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = err
}

var inviteLinkRE = regexp.MustCompile(`/invites/([A-Za-z0-9_-]{43})`)

// inviteToken returns the token from the latest email sent to addr.
func (r *recorder) inviteToken(t *testing.T, addr string) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(r.msgs[i].To, addr) {
			if m := inviteLinkRE.FindStringSubmatch(r.msgs[i].Body); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("no invite email to %s", addr)
	return ""
}

type harness struct {
	t        *testing.T
	base     string
	mail     *recorder
	database string
}

type harnessOpts struct {
	listen   string // fixed address, needed when an identity provider must redirect back
	base     string
	oidc     *config.OIDC
	database string // share another harness's database, like a second app replica
}

func newHarness(t *testing.T, opts harnessOpts) *harness {
	t.Helper()
	ctx := context.Background()
	url := opts.database
	if url == "" {
		url = dbtest.NewDatabase(t)
		if _, err := db.Migrate(ctx, url); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	srv := httptest.NewUnstartedServer(nil)
	if opts.listen != "" {
		srv.Listener.Close()
		if srv.Listener, err = net.Listen("tcp", opts.listen); err != nil {
			t.Fatal(err)
		}
	}
	base := opts.base
	if base == "" {
		base = "http://" + srv.Listener.Addr().String()
	}
	cfg := config.Config{BaseURL: base}

	var sso *httpapi.OIDC
	if opts.oidc != nil {
		if sso, err = httpapi.NewOIDC(ctx, *opts.oidc, base); err != nil {
			t.Fatal(err)
		}
	}
	hubCtx, stopHub := context.WithCancel(ctx)
	hub := live.New(pool)
	hub.Start(hubCtx)
	rec := &recorder{}
	srv.Config.Handler = httpapi.New(cfg, pool, rec, sso, hub, nil)
	srv.Start()
	// Cleanups run last-in first-out: close the server (and its sockets), then
	// stop the hub, then the pool closes.
	t.Cleanup(stopHub)
	t.Cleanup(srv.Close)
	return &harness{t: t, base: base, mail: rec, database: url}
}

// client is one browser: its own cookie jar.
type client struct {
	h    *harness
	http *http.Client
}

func (h *harness) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{h: h, http: &http.Client{Jar: jar}}
}

// sessionToken returns the browser's session cookie value.
func (c *client) sessionToken() string {
	u, _ := url.Parse(c.h.base)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "oms_session" {
			return ck.Value
		}
	}
	return ""
}

// clientWithSession is a new browser that holds the given session token.
func (h *harness) clientWithSession(token string) *client {
	c := h.client()
	u, _ := url.Parse(h.base)
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "oms_session", Value: token, Path: "/"}})
	return c
}

type resp struct {
	status int
	body   map[string]any
	list   []any
	raw    string
}

func (r resp) errCode() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (c *client) do(method, path string, body any) resp {
	c.h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.h.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.h.base+path, rd)
	if err != nil {
		c.h.t.Fatal(err)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	_ = json.Unmarshal(raw, &out.list)
	return out
}

// must fails the test unless the response has the wanted status.
func (c *client) must(want int, method, path string, body any) resp {
	c.h.t.Helper()
	r := c.do(method, path, body)
	if r.status != want {
		c.h.t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.status, want, r.raw)
	}
	return r
}

const ownerPassword = "owner-password-123"

// bootstrap creates the first user and a workspace "acme" they own.
func (h *harness) bootstrap() *client {
	owner := h.client()
	owner.must(201, "POST", "/api/setup", map[string]string{"email": "owner@example.com", "displayName": "Olive Owner", "password": ownerPassword})
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Acme", "slug": "acme"})
	return owner
}

// join invites email with role into acme and has a new browser accept it as a new account.
func (h *harness) join(inviter *client, email, roleName string) (*client, string) {
	h.t.Helper()
	inviter.must(201, "POST", "/api/workspaces/acme/invites", map[string]string{"email": email, "role": roleName})
	c := h.client()
	r := c.must(201, "POST", "/api/invites/"+h.mail.inviteToken(h.t, email)+"/accept",
		map[string]string{"displayName": email, "password": "member-password-123"})
	return c, r.body["user"].(map[string]any)["id"].(string)
}
