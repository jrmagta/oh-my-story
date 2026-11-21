package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/role"
	"github.com/jrmagta/oh-my-story/internal/store"
)

// keycloakDirectory searches a Keycloak realm's users through its admin REST
// API, as the OIDC client's service account. The account needs the
// realm-management roles view-users and query-users and, from Keycloak 26.8,
// the kc-admin-api client scope.
type keycloakDirectory struct {
	admin  string // e.g. http://keycloak:8080/admin/realms/oms
	client *http.Client
}

// DirectoryUser is a person in the identity provider, as Members shows them.
type DirectoryUser struct {
	ID        string `json:"id"` // the provider's user id, which is their OIDC subject
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Name      string `json:"name"`
	Member    bool   `json:"member"` // already a member of this workspace
}

type keycloakUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Enabled       bool   `json:"enabled"`
}

var errDirectoryOff = errBadRequest("the user directory is not set up (OMS_OIDC_DIRECTORY)")

// newKeycloakDirectory reads the realm from the discovery URL
// (…/realms/<realm>), which is also how the server reaches Keycloak.
func newKeycloakDirectory(cfg config.OIDC, tokenURL string) (*keycloakDirectory, error) {
	base, realm, ok := strings.Cut(strings.TrimSuffix(cfg.DiscoveryURL, "/"), "/realms/")
	if !ok || realm == "" || strings.Contains(realm, "/") {
		return nil, fmt.Errorf("OMS_OIDC_DIRECTORY=keycloak needs a Keycloak issuer or discovery URL like https://sso.example.com/realms/<realm>; got %s", cfg.DiscoveryURL)
	}
	cc := clientcredentials.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, TokenURL: tokenURL}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 10 * time.Second})
	client := cc.Client(ctx)
	client.Timeout = 10 * time.Second
	return &keycloakDirectory{admin: base + "/admin/realms/" + url.PathEscape(realm), client: client}, nil
}

func (d *keycloakDirectory) get(ctx context.Context, path string, q url.Values, out any) error {
	u := d.admin + path
	if q != nil {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("keycloak directory: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return store.ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("keycloak directory: %s %s: %s", res.Status, path, b)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

// search finds up to 20 enabled users with an email by name, username or email.
func (d *keycloakDirectory) search(ctx context.Context, q string) ([]keycloakUser, error) {
	var users []keycloakUser
	err := d.get(ctx, "/users", url.Values{"search": {q}, "max": {"20"}, "briefRepresentation": {"true"}}, &users)
	out := users[:0]
	for _, u := range users {
		if u.Enabled && u.Email != "" {
			out = append(out, u)
		}
	}
	return out, err
}

func (d *keycloakDirectory) user(ctx context.Context, id string) (keycloakUser, error) {
	var u keycloakUser
	err := d.get(ctx, "/users/"+url.PathEscape(id), nil, &u)
	return u, err
}

// profile builds the names an account takes from its provider: "First Last",
// else the provider's full name, else the email.
func profile(email, first, last, name string) store.Profile {
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	display := strings.TrimSpace(first + " " + last)
	if display == "" {
		display = name
	}
	if d, err := parseDisplayName(display); err == nil {
		display = d
	} else {
		display = email
	}
	return store.Profile{Email: email, FirstName: first, LastName: last, DisplayName: display}
}

// directorySearch lists people in the identity provider matching ?q=, saying
// which are members of the workspace already.
func (s *Server) directorySearch(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	if s.oidc == nil || s.oidc.directory == nil {
		writeError(w, errDirectoryOff)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 100 {
		writeError(w, errBadRequest("q must be 1 to 100 characters"))
		return
	}
	found, err := s.oidc.directory.search(r.Context(), q)
	if err != nil {
		writeError(w, err)
		return
	}
	subjects, emails := make([]string, len(found)), make([]string, len(found))
	for i, u := range found {
		subjects[i], emails[i] = u.ID, strings.ToLower(u.Email)
	}
	bySubject, byEmail, err := s.store.DirectoryMembers(r.Context(), m.ID, s.oidc.issuer, subjects, emails)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]DirectoryUser, len(found))
	for i, u := range found {
		p := profile(u.Email, u.FirstName, u.LastName, u.Username)
		out[i] = DirectoryUser{ID: u.ID, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName, Name: p.DisplayName,
			Member: bySubject[u.ID] || byEmail[strings.ToLower(u.Email)]}
	}
	writeJSON(w, http.StatusOK, out)
}

// addDirectoryMember makes a person from the identity provider a member at
// once, with no invite: their account is made from the directory (or an
// existing one with their email is linked), and they sign in with SSO.
func (s *Server) addDirectoryMember(w http.ResponseWriter, r *http.Request, _ store.User, m store.Membership) {
	if s.oidc == nil || s.oidc.directory == nil {
		writeError(w, errDirectoryOff)
		return
	}
	var in struct{ DirectoryID, Role string }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	rl, err := role.Parse(in.Role)
	// As with invites, anyone but an owner can be added this way.
	if err != nil || rl == role.Owner {
		writeError(w, errBadRequest("role must be viewer, commenter, editor or admin"))
		return
	}
	if in.DirectoryID == "" {
		writeError(w, errBadRequest("directoryId is required"))
		return
	}
	// Ask the directory, not the request, for who this is.
	ku, err := s.oidc.directory.user(r.Context(), in.DirectoryID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, errBadRequest("no such person in the directory"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	email, err := parseEmail(ku.Email)
	if !ku.Enabled || err != nil {
		writeError(w, errBadRequest("that person is disabled in the directory or has no email address"))
		return
	}
	member, err := s.store.AddDirectoryMember(r.Context(), m.ID, s.oidc.issuer, ku.ID, profile(email, ku.FirstName, ku.LastName, ku.Username), rl)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, &apiError{http.StatusConflict, "conflict", "already a member"})
	case errors.Is(err, store.ErrDisabled):
		writeError(w, errBadRequest("that person's account here is deactivated"))
	case err != nil:
		writeError(w, err)
	default:
		writeJSON(w, http.StatusCreated, member)
	}
}
