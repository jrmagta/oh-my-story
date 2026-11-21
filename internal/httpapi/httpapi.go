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
}

// New returns the server's handler. hub must be started. ui serves the web
// app for every other GET; nil leaves those paths 404 (the API tests run without it).
func New(cfg config.Config, pool *pgxpool.Pool, sender mail.Sender, oidc *OIDC, hub *live.Hub, ui http.Handler) http.Handler {
	s := &Server{cfg: cfg, pool: pool, store: store.New(pool), mail: sender, oidc: oidc, hub: hub}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)

	mux.HandleFunc("GET /api/setup", s.getSetup)
	mux.HandleFunc("POST /api/setup", s.postSetup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/providers", s.providers)
	mux.HandleFunc("GET /api/me", s.authed(s.me))
	mux.HandleFunc("GET /auth/oidc/login", s.oidcLogin)
	mux.HandleFunc("GET /auth/oidc/callback", s.oidcCallback)

	mux.HandleFunc("GET /api/workspaces", s.authed(s.listWorkspaces))
	mux.HandleFunc("POST /api/workspaces", s.authed(s.createWorkspace))
	mux.HandleFunc("GET /api/project-keys", s.authed(s.projectKeys))
	mux.HandleFunc("GET /api/workspaces/{slug}", s.inWorkspace(role.View, s.getWorkspace))
	mux.HandleFunc("PATCH /api/workspaces/{slug}", s.inWorkspace(role.ManageSettings, s.patchWorkspace))
	mux.HandleFunc("DELETE /api/workspaces/{slug}", s.inWorkspace(role.DeleteWorkspace, s.deleteWorkspace))
	mux.HandleFunc("GET /api/workspaces/{slug}/members", s.inWorkspace(role.View, s.listMembers))
	mux.HandleFunc("PATCH /api/workspaces/{slug}/members/{userId}", s.inWorkspace(role.ManageMembers, s.patchMember))
	// Removing yourself only needs membership; the handler checks ManageMembers for anyone else.
	mux.HandleFunc("DELETE /api/workspaces/{slug}/members/{userId}", s.inWorkspace(role.View, s.deleteMember))
	mux.HandleFunc("GET /api/workspaces/{slug}/invites", s.inWorkspace(role.ManageMembers, s.listInvites))
	mux.HandleFunc("POST /api/workspaces/{slug}/invites", s.inWorkspace(role.ManageMembers, s.createInvite))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/invites/{id}", s.inWorkspace(role.ManageMembers, s.revokeInvite))
	mux.HandleFunc("POST /api/auth/reset", s.requestReset)
	mux.HandleFunc("GET /api/auth/reset/{token}", s.getReset)
	mux.HandleFunc("POST /api/auth/reset/{token}", s.resetPassword)
	mux.HandleFunc("GET /api/invites/{token}", s.getInvite)
	mux.HandleFunc("POST /api/invites/{token}/accept", s.acceptInvite)
	s.mapRoutes(mux)

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

	return s.csrf(mux)
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

// csrf rejects state-changing /api requests that a cross-site form could send.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				writeError(w, errBadRequest("Content-Type must be application/json"))
				return
			}
			if o := r.Header.Get("Origin"); o != "" && o != s.cfg.BaseURL {
				writeError(w, &apiError{http.StatusForbidden, "forbidden", "cross-origin request refused"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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

func (s *Server) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.currentUser(r)
		if !ok {
			writeError(w, errUnauthenticated)
			return
		}
		h(w, r, u)
	}
}

type workspaceHandler func(w http.ResponseWriter, r *http.Request, u store.User, m store.Membership)

// inWorkspace loads the caller's membership of {slug} and requires the action.
// Non-members get 404 so that slugs do not leak.
func (s *Server) inWorkspace(a role.Action, h workspaceHandler) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, u store.User) {
		m, err := s.store.Membership(r.Context(), u.ID, r.PathValue("slug"))
		if err != nil {
			writeError(w, err)
			return
		}
		if !m.Role.Can(a) {
			writeError(w, errForbidden)
			return
		}
		h(w, r, u, m)
	})
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
