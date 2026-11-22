// Package httpapi is the HTTP surface of the server. The contract is docs/API.md.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jrmagta/oh-my-story/api"
	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/live"
	"github.com/jrmagta/oh-my-story/internal/mail"
	"github.com/jrmagta/oh-my-story/internal/role"
	"github.com/jrmagta/oh-my-story/internal/store"
)

const (
	sessionCookie   = "oms_session"
	sessionLifetime = 30 * 24 * time.Hour
	inviteLifetime  = 7 * 24 * time.Hour
	maxBody         = 1 << 20
)

type Server struct {
	cfg   config.Config
	pool  *pgxpool.Pool
	store *store.Store
	mail  mail.Sender
	oidc  *OIDC // nil when SSO is off
	hub   *live.Hub
	// routes lists the patterns registered, in order (see handle).
	routes []string
	// transfers holds a slot for each file upload or download in progress.
	transfers chan struct{}
}

// New returns the server's handler. hub must be started. ui serves the web
// app for every other GET; nil leaves those paths 404 (the API tests run without it).
func New(cfg config.Config, pool *pgxpool.Pool, sender mail.Sender, oidc *OIDC, hub *live.Hub, ui http.Handler) http.Handler {
	s := &Server{cfg: cfg, pool: pool, store: store.New(pool), mail: sender, oidc: oidc, hub: hub, transfers: make(chan struct{}, maxTransfers)}
	mux := http.NewServeMux()
	s.register(mux, ui)
	return s.csrf(mux)
}

// register adds every route, then the catch-all for other paths.
func (s *Server) register(mux *http.ServeMux, ui http.Handler) {

	s.handle(mux, "GET /healthz", s.healthz)

	s.handle(mux, "GET /api/setup", s.getSetup)
	s.handle(mux, "POST /api/setup", s.postSetup)
	s.handle(mux, "POST /api/auth/login", s.login)
	s.handle(mux, "POST /api/auth/logout", s.logout)
	s.handle(mux, "GET /api/auth/providers", s.providers)
	s.handle(mux, "GET /api/me", s.authed(s.me))
	s.handle(mux, "GET /auth/oidc/login", s.oidcLogin)
	s.handle(mux, "GET /auth/oidc/callback", s.oidcCallback)

	s.handle(mux, "GET /api/workspaces", s.authed(s.listWorkspaces))
	s.handle(mux, "POST /api/workspaces", s.authed(s.createWorkspace))
	s.handle(mux, "GET /api/project-keys", s.authed(s.projectKeys))
	s.handle(mux, "GET /api/shared/{token}", s.sharedMap)
	s.handle(mux, "GET /api/workspaces/{slug}", s.inWorkspace(role.View, s.getWorkspace))
	s.handle(mux, "PATCH /api/workspaces/{slug}", s.inWorkspace(role.ManageSettings, s.patchWorkspace))
	s.handle(mux, "DELETE /api/workspaces/{slug}", s.inWorkspace(role.DeleteWorkspace, s.deleteWorkspace))
	s.handle(mux, "GET /api/workspaces/{slug}/members", s.inWorkspace(role.View, s.listMembers))
	s.handle(mux, "PATCH /api/workspaces/{slug}/members/{userId}", s.inWorkspace(role.ManageMembers, s.patchMember))
	// Removing yourself only needs membership; the handler checks ManageMembers for anyone else.
	s.handle(mux, "DELETE /api/workspaces/{slug}/members/{userId}", s.inWorkspace(role.View, s.deleteMember))
	s.handle(mux, "POST /api/workspaces/{slug}/members", s.inWorkspace(role.ManageMembers, s.addDirectoryMember))
	s.handle(mux, "GET /api/workspaces/{slug}/directory", s.inWorkspace(role.ManageMembers, s.directorySearch))
	s.handle(mux, "GET /api/workspaces/{slug}/invites", s.inWorkspace(role.ManageMembers, s.listInvites))
	s.handle(mux, "POST /api/workspaces/{slug}/invites", s.inWorkspace(role.ManageMembers, s.createInvite))
	s.handle(mux, "DELETE /api/workspaces/{slug}/invites/{id}", s.inWorkspace(role.ManageMembers, s.revokeInvite))
	s.handle(mux, "POST /api/auth/reset", s.requestReset)
	s.handle(mux, "GET /api/auth/reset/{token}", s.getReset)
	s.handle(mux, "POST /api/auth/reset/{token}", s.resetPassword)
	s.handle(mux, "GET /api/invites/{token}", s.getInvite)
	s.handle(mux, "POST /api/invites/{token}/accept", s.acceptInvite)
	s.mapRoutes(mux)
	s.handle(mux, "GET /api/openapi.yaml", serveOpenAPI)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			writeError(w, &apiError{http.StatusNotFound, "not_found", "no such endpoint"})
		case ui != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) && !strings.HasPrefix(r.URL.Path, "/auth/"):
			ui.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// handle registers a route and remembers its pattern, so a test can hold the
// routes against the OpenAPI description.
func (s *Server) handle(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	s.routes = append(s.routes, pattern)
	mux.HandleFunc(pattern, h)
}

// serveOpenAPI sends the API's description. No sign-in needed.
func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(api.OpenAPI)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": "unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "ok"})
}

// ---- middleware ----

// csrf rejects state-changing /api requests that a cross-site form could
// send. Forms send only urlencoded, multipart and text/plain bodies, so JSON
// and octet-stream (file uploads) cannot come from one.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "application/octet-stream") {
				writeError(w, errBadRequest("Content-Type must be application/json (or application/octet-stream for a file)"))
				return
			}
			// A bearer token is never sent by a browser on its own, so it needs no Origin check.
			if o := r.Header.Get("Origin"); o != "" && o != s.cfg.BaseURL && bearer(r) == "" {
				writeError(w, &apiError{http.StatusForbidden, "forbidden", "cross-origin request refused"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// bearer is the token in an "Authorization: Bearer ..." header, or "".
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}

// caller is who is making a request: a signed-in user, or a token's bot user
// with the grant that token carries.
type caller struct {
	user  store.User
	token *store.TokenGrant
}

// currentCaller reads an API token if the request has one (and then nothing
// else), or else the session cookie.
func (s *Server) currentCaller(r *http.Request) (caller, bool) {
	if t := bearer(r); t != "" {
		u, g, err := s.store.UserByAPIToken(r.Context(), auth.HashToken(t))
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				slog.Error("load api token", "err", err)
			}
			return caller{}, false
		}
		return caller{user: u, token: &g}, true
	}
	u, ok := s.currentUser(r)
	return caller{user: u}, ok
}

func (s *Server) currentUser(r *http.Request) (store.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return store.User{}, false
	}
	u, err := s.store.UserBySession(r.Context(), auth.HashToken(c.Value))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load session", "err", err)
		}
		return store.User{}, false
	}
	return u, true
}

type authedHandler func(w http.ResponseWriter, r *http.Request, u store.User)

// authed serves signed-in people only; API tokens are for workspace endpoints.
func (s *Server) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.currentCaller(r)
		if !ok {
			writeError(w, errUnauthenticated)
			return
		}
		if c.token != nil {
			writeError(w, &apiError{http.StatusForbidden, "forbidden", "API tokens can only use /api/workspaces/{slug}/… endpoints"})
			return
		}
		h(w, r, c.user)
	}
}

type workspaceHandler func(w http.ResponseWriter, r *http.Request, u store.User, m store.Membership)

// inWorkspace loads the caller's membership of {slug} and requires the action.
// Non-members get 404 so that slugs do not leak.
// An API token acts in its own workspace only, as a viewer (read) or an editor (write).
func (s *Server) inWorkspace(a role.Action, h workspaceHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.currentCaller(r)
		if !ok {
			writeError(w, errUnauthenticated)
			return
		}
		u := c.user
		var m store.Membership
		var err error
		if c.token != nil {
			m, err = s.store.TokenMembership(r.Context(), *c.token, r.PathValue("slug"))
		} else {
			m, err = s.store.Membership(r.Context(), u.ID, r.PathValue("slug"))
		}
		if err != nil {
			writeError(w, err)
			return
		}
		if !m.Role.Can(a) {
			writeError(w, errForbidden)
			return
		}
		h(w, r, u, m)
	}
}

// ---- sessions ----

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) error {
	token, hash, err := auth.NewToken()
	if err != nil {
		return err
	}
	var replaces []byte
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		replaces = auth.HashToken(c.Value)
	}
	expires := time.Now().Add(sessionLifetime)
	if err := s.store.CreateSession(r.Context(), u.ID, hash, expires, r.UserAgent(), replaces); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

// ---- JSON and errors ----

type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

var (
	errUnauthenticated = &apiError{http.StatusUnauthorized, "unauthenticated", "sign in required"}
	errForbidden       = &apiError{http.StatusForbidden, "forbidden", "your role does not allow this"}
)

func errBadRequest(msg string) *apiError { return &apiError{http.StatusBadRequest, "bad_request", msg} }

func writeError(w http.ResponseWriter, err error) {
	var e *apiError
	switch {
	case errors.As(err, &e):
	case errors.Is(err, store.ErrNotFound):
		e = &apiError{http.StatusNotFound, "not_found", "not found"}
	case errors.Is(err, store.ErrLastOwner):
		e = &apiError{http.StatusConflict, "conflict", err.Error()}
	case errors.Is(err, store.ErrAlreadyMember):
		e = &apiError{http.StatusConflict, "conflict", "already a member of this workspace"}
	case errors.Is(err, store.ErrInvalid):
		e = errBadRequest(err.Error())
	case errors.Is(err, store.ErrForbidden):
		e = errForbidden
	case errors.Is(err, store.ErrSlugTaken):
		e = &apiError{http.StatusConflict, "conflict", "that URL slug is taken"}
	case errors.Is(err, store.ErrKeyTaken):
		e = &apiError{http.StatusConflict, "conflict", "that project key is taken"}
	case errors.Is(err, store.ErrConflict):
		e = &apiError{http.StatusConflict, "conflict", "already exists"}
	case errors.Is(err, store.ErrInviteUsed), errors.Is(err, store.ErrBootstrapped), errors.Is(err, store.ErrResetUsed):
		e = &apiError{http.StatusGone, "gone", err.Error()}
	case errors.Is(err, store.ErrWrongUser):
		e = &apiError{http.StatusForbidden, "forbidden", "this invite is for a different email address"}
	default:
		slog.Error("request failed", "err", err)
		e = &apiError{http.StatusInternalServerError, "internal", "internal error"}
	}
	writeJSON(w, e.status, map[string]any{"error": map[string]string{"code": e.code, "message": e.message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// readJSON decodes one JSON object into dst, rejecting unknown fields and oversized bodies.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errBadRequest("invalid JSON body: " + err.Error())
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errBadRequest("body must be a single JSON object")
	}
	return nil
}
