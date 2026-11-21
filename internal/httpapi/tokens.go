package httpapi

import (
	"net/http"
	"time"

	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/store"
)

// tokenPrefix starts every API token, so leaked tokens are easy to spot in logs and scanners.
const tokenPrefix = "oms_"

func (s *Server) listAPITokens(q mapRequest) (int, any, error) {
	out, err := s.store.APITokens(q.r.Context(), q.ws)
	return http.StatusOK, out, err
}

// createAPIToken makes a token for this workspace. scope is read or write;
// expiresInDays is 30, 90, 365 or null (never). The token is in this reply only.
func (s *Server) createAPIToken(q mapRequest) (int, any, error) {
	var in struct {
		Name, Scope   string
		ExpiresInDays *int
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	name, err := text("name", in.Name, maxName, false)
	if err != nil {
		return 0, nil, err
	}
	if _, ok := store.TokenScopes[in.Scope]; !ok {
		return 0, nil, errBadRequest("scope must be read or write")
	}
	var expires *time.Time
	if in.ExpiresInDays != nil {
		switch *in.ExpiresInDays {
		case 30, 90, 365:
			t := time.Now().Add(time.Duration(*in.ExpiresInDays) * 24 * time.Hour)
			expires = &t
		default:
			return 0, nil, errBadRequest("expiresInDays must be 30, 90, 365 or null")
		}
	}
	secret, _, err := auth.NewToken()
	if err != nil {
		return 0, nil, err
	}
	token := tokenPrefix + secret
	t, err := s.store.CreateAPIToken(q.r.Context(), q.user, q.ws, name, in.Scope, token[:len(tokenPrefix)+6], auth.HashToken(token), expires)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, struct {
		store.APIToken
		Token string `json:"token"`
	}{t, token}, nil
}

func (s *Server) revokeAPIToken(q mapRequest) (int, any, error) {
	return 0, nil, s.store.RevokeAPIToken(q.r.Context(), q.ws, q.id)
}
