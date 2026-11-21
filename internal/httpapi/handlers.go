package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/mail"
	"github.com/jrmagta/oh-my-story/internal/role"
	"github.com/jrmagta/oh-my-story/internal/store"
)

// ---- setup, login, me ----

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	has, err := s.store.HasUsers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needsBootstrap": !has})
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, DisplayName, Password string }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	email, err := parseEmail(in.Email)
	if err != nil {
		writeError(w, err)
		return
	}
	name, err := parseDisplayName(in.DisplayName)
	if err != nil {
		writeError(w, err)
		return
	}
	password, err := parseNewPassword(in.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		writeError(w, err)
		return
	}
	u, err := s.store.Bootstrap(r.Context(), email, name, hash)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

var errBadCredentials = &apiError{http.StatusUnauthorized, "unauthenticated", "email or password is incorrect"}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	u, hash, err := s.store.UserForLogin(r.Context(), strings.TrimSpace(in.Email))
	if errors.Is(err, store.ErrNotFound) || (err == nil && hash == "") {
		auth.BurnPasswordCheck(in.Password) // same timing as a wrong password
		writeError(w, errBadCredentials)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	ok, err := auth.VerifyPassword(hash, in.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	if !ok {
		writeError(w, errBadCredentials)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			writeError(w, err)
			return
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u store.User) {
	ms, err := s.store.Memberships(r.Context(), u.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "workspaces": ms})
}

func (s *Server) providers(w http.ResponseWriter, r *http.Request) {
	o := map[string]any{"enabled": false}
	if s.oidc != nil {
		o = map[string]any{"enabled": true, "name": s.oidc.name, "directory": s.oidc.directory != nil}
	}
	writeJSON(w, http.StatusOK, map[string]any{"oidc": o})
}

// ---- workspaces ----

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request, u store.User) {
	ms, err := s.store.Memberships(r.Context(), u.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request, u store.User) {
	var in struct{ Name, Slug, KeyPrefix string }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	name, err := parseWorkspaceName(in.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	slug, err := parseSlug(in.Slug)
	if err != nil {
		writeError(w, err)
		return
	}
	key := ""
	if in.KeyPrefix != "" {
		if key, err = parseKeyPrefix(in.KeyPrefix); err != nil {
			writeError(w, err)
			return
		}
	}
	ws, err := s.store.CreateWorkspace(r.Context(), u.ID, name, slug, key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, store.Membership{Workspace: ws, Role: role.Owner})
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) patchWorkspace(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	var in struct{ Name, KeyPrefix, EstimateUnit *string }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	var p store.WorkspacePatch
	if in.Name != nil {
		v, err := parseWorkspaceName(*in.Name)
		if err != nil {
			writeError(w, err)
			return
		}
		p.Name = &v
	}
	if in.KeyPrefix != nil {
		v, err := parseKeyPrefix(*in.KeyPrefix)
		if err != nil {
			writeError(w, err)
			return
		}
		p.KeyPrefix = &v
	}
	if in.EstimateUnit != nil {
		v, err := parseEstimateUnit(*in.EstimateUnit)
		if err != nil {
			writeError(w, err)
			return
		}
		p.EstimateUnit = &v
	}
	ws, err := s.store.UpdateWorkspace(r.Context(), m.ID, p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, store.Membership{Workspace: ws, Role: m.Role})
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	if err := s.store.DeleteWorkspace(r.Context(), m.ID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- members ----

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	ms, err := s.store.Members(r.Context(), m.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, store.ErrNotFound
	}
	return id, nil
}

// onlyOwnersTouchOwners vetoes a change to an owner, or a grant of owner, by a non-owner.
func onlyOwnersTouchOwners(caller role.Role, newRole *role.Role) func(role.Role) error {
	return func(current role.Role) error {
		if caller != role.Owner && (current == role.Owner || (newRole != nil && *newRole == role.Owner)) {
			return &apiError{http.StatusForbidden, "forbidden", "only an owner can change or grant the owner role"}
		}
		return nil
	}
}

func (s *Server) patchMember(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	target, err := pathUUID(r, "userId")
	if err != nil {
		writeError(w, err)
		return
	}
	var in struct{ Role role.Role }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if in.Role == 0 {
		writeError(w, errBadRequest("role is required"))
		return
	}
	member, err := s.store.SetRole(r.Context(), m.ID, target, in.Role, onlyOwnersTouchOwners(m.Role, &in.Role))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request, u store.User, m store.Membership) {
	target, err := pathUUID(r, "userId")
	if err != nil {
		writeError(w, err)
		return
	}
	allow := func(role.Role) error { return nil } // leaving is always allowed, bar the last-owner rule
	if target != u.ID {
		if !m.Role.Can(role.ManageMembers) {
			writeError(w, errForbidden)
			return
		}
		allow = onlyOwnersTouchOwners(m.Role, nil)
	}
	if err := s.store.RemoveMember(r.Context(), m.ID, target, allow); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- invites ----

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	invs, err := s.store.OpenInvites(r.Context(), m.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invs)
}

var errEmailFailed = &apiError{http.StatusBadGateway, "email_failed", "the invite email could not be sent; nothing was saved, so try again"}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request, u store.User, m store.Membership) {
	var in struct {
		Email string
		Role  role.Role
	}
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	email, err := parseEmail(in.Email)
	if err != nil {
		writeError(w, err)
		return
	}
	if in.Role == 0 || in.Role == role.Owner {
		writeError(w, errBadRequest("role must be admin, editor, commenter or viewer"))
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		writeError(w, err)
		return
	}
	inv, err := s.store.CreateInvite(r.Context(), m.ID, email, in.Role, hash, u.ID, time.Now().Add(inviteLifetime))
	if err != nil {
		writeError(w, err)
		return
	}
	link := s.cfg.BaseURL + "/invites/" + token
	msg := mail.Message{
		To:      inv.Email,
		Subject: fmt.Sprintf("%s invited you to %s on oh-my-story", u.DisplayName, m.Name),
		Body: fmt.Sprintf("%s invited you to join the %s workspace as %s.\n\nAccept the invite:\n%s\n\nThe link works once and expires on %s.\n",
			u.DisplayName, m.Name, inv.Role, link, inv.ExpiresAt.UTC().Format("2 January 2006 15:04 MST")),
	}
	if err := s.mail.Send(r.Context(), msg); err != nil {
		// Nobody holds the token, so the invite is useless, and while it stays
		// open it blocks a retry for the same email. Remove it.
		slog.Error("send invite email", "err", err)
		if err := s.store.RevokeInvite(context.WithoutCancel(r.Context()), m.ID, inv.ID); err != nil {
			slog.Error("remove unsent invite", "err", err)
		}
		writeError(w, errEmailFailed)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.RevokeInvite(r.Context(), m.ID, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.InviteByToken(r.Context(), auth.HashToken(r.PathValue("token")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaceName": inv.WorkspaceName, "email": inv.Email, "role": inv.Role})
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct{ DisplayName, Password *string }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	hash := auth.HashToken(r.PathValue("token"))

	if u, ok := s.currentUser(r); ok {
		_, ws, err := s.store.AcceptInvite(r.Context(), hash, &u, nil)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"workspace": ws})
		return
	}

	if in.DisplayName == nil || in.Password == nil {
		writeError(w, &apiError{http.StatusUnauthorized, "unauthenticated", "sign in, or send displayName and password to create an account"})
		return
	}
	name, err := parseDisplayName(*in.DisplayName)
	if err != nil {
		writeError(w, err)
		return
	}
	password, err := parseNewPassword(*in.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	pwHash, err := auth.HashPassword(password)
	if err != nil {
		writeError(w, err)
		return
	}
	u, ws, err := s.store.AcceptInvite(r.Context(), hash, nil, &store.NewUser{DisplayName: name, PasswordHash: pwHash})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, &apiError{http.StatusConflict, "conflict", "an account with this email already exists; sign in, then accept"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workspace": ws, "user": u})
}

// projectKeys suggests a free project key for ?name= and says whether ?key= is free.
func (s *Server) projectKeys(w http.ResponseWriter, r *http.Request, _ store.User) {
	out := struct {
		Suggestion string `json:"suggestion"`
		Available  *bool  `json:"available,omitempty"`
	}{}
	var err error
	if out.Suggestion, err = s.store.SuggestKey(r.Context(), r.URL.Query().Get("name")); err != nil {
		writeError(w, err)
		return
	}
	if k := r.URL.Query().Get("key"); k != "" {
		if _, err := parseKeyPrefix(k); err != nil {
			writeError(w, err)
			return
		}
		taken, err := s.store.KeyTaken(r.Context(), k)
		if err != nil {
			writeError(w, err)
			return
		}
		free := !taken
		out.Available = &free
	}
	writeJSON(w, http.StatusOK, out)
}
