package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/store"
)

const oidcCookie = "oms_oidc"

type OIDC struct {
	issuer    string
	name      string
	oauth     oauth2.Config
	verifier  *oidc.IDTokenVerifier
	directory *keycloakDirectory // nil unless OMS_OIDC_DIRECTORY=keycloak
}

// NewOIDC fetches the provider's discovery document. DiscoveryURL may differ from
// the issuer (in compose the app reaches Keycloak by its service name), so the
// issuer in tokens is checked against cfg.Issuer, not the URL fetched.
func NewOIDC(ctx context.Context, cfg config.OIDC, baseURL string) (*OIDC, error) {
	ctx = oidc.InsecureIssuerURLContext(ctx, cfg.Issuer)
	p, err := oidc.NewProvider(ctx, cfg.DiscoveryURL)
	if err != nil {
		return nil, err
	}
	var dir *keycloakDirectory
	if cfg.Directory == "keycloak" {
		if dir, err = newKeycloakDirectory(cfg, p.Endpoint().TokenURL); err != nil {
			return nil, err
		}
	}
	return &OIDC{
		directory: dir,
		issuer:    cfg.Issuer,
		name:      cfg.Name,
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     p.Endpoint(),
			RedirectURL:  baseURL + "/auth/oidc/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// oidcFlow is what the browser carries between login and callback.
type oidcFlow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"` // where to send the browser afterwards
}

// localPath returns p if it is a path on this site, else "/". It keeps the
// redirect after sign-in from becoming an open redirect.
func localPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return "/"
	}
	if u, err := url.Parse(p); err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return p
}

func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	state, _, err1 := auth.NewToken()
	nonce, _, err2 := auth.NewToken()
	if err := errors.Join(err1, err2); err != nil {
		writeError(w, err)
		return
	}
	f := oidcFlow{State: state, Nonce: nonce, Verifier: oauth2.GenerateVerifier(), Next: localPath(r.URL.Query().Get("next"))}
	b, _ := json.Marshal(f)
	http.SetCookie(w, &http.Cookie{
		Name: oidcCookie, Value: base64.RawURLEncoding.EncodeToString(b), Path: "/auth/oidc", MaxAge: 600,
		HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.oidc.oauth.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.NotFound(w, r)
		return
	}
	var f oidcFlow
	fail := func(code string, err error) {
		if err != nil {
			slog.Warn("oidc callback", "code", code, "err", err)
		}
		q := url.Values{"error": {code}}
		if f.Next != "" && f.Next != "/" {
			q.Set("next", f.Next)
		}
		http.Redirect(w, r, "/login?"+q.Encode(), http.StatusFound)
	}

	c, err := r.Cookie(oidcCookie)
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Path: "/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookies()})
	if err != nil {
		fail("sso_expired", nil)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || json.Unmarshal(raw, &f) != nil || f.State == "" {
		fail("sso_expired", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(f.State), []byte(r.URL.Query().Get("state"))) != 1 {
		fail("sso_state", nil)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		fail("sso_denied", errors.New(e))
		return
	}

	tok, err := s.oidc.oauth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		fail("sso_failed", err)
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		fail("sso_failed", errors.New("no id_token in token response"))
		return
	}
	idt, err := s.oidc.verifier.Verify(r.Context(), rawID)
	if err != nil {
		fail("sso_failed", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(f.Nonce)) != 1 {
		fail("sso_failed", errors.New("nonce mismatch"))
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		GivenName     string `json:"given_name"`
		FamilyName    string `json:"family_name"`
	}
	if err := idt.Claims(&claims); err != nil {
		fail("sso_failed", err)
		return
	}
	email, err := parseEmail(claims.Email)
	if err != nil || !claims.EmailVerified {
		fail("sso_email_unverified", nil)
		return
	}
	u, err := s.store.ResolveOIDCUser(r.Context(), s.oidc.issuer, idt.Subject, profile(email, claims.GivenName, claims.FamilyName, claims.Name))
	if errors.Is(err, store.ErrNotFound) {
		fail("not_invited", nil)
		return
	}
	if errors.Is(err, store.ErrDisabled) {
		fail("account_disabled", nil)
		return
	}
	if err != nil {
		fail("sso_failed", err)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		fail("sso_failed", err)
		return
	}
	http.Redirect(w, r, localPath(f.Next), http.StatusFound)
}
