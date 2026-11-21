package httpapi

import (
	"net/http"
	"time"

	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/store"
)

func (s *Server) listShareLinks(q mapRequest) (int, any, error) {
	ls, err := s.store.ShareLinks(q.r.Context(), q.ws, q.mapID)
	return http.StatusOK, ls, err
}

// createShareLink makes a read-only link. expiresInDays is 7, 30, 90 or null
// (never). The URL, with its token, is in this reply only.
func (s *Server) createShareLink(q mapRequest) (int, any, error) {
	var in struct{ ExpiresInDays *int }
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var expires *time.Time
	if in.ExpiresInDays != nil {
		switch *in.ExpiresInDays {
		case 7, 30, 90:
			t := time.Now().Add(time.Duration(*in.ExpiresInDays) * 24 * time.Hour)
			expires = &t
		default:
			return 0, nil, errBadRequest("expiresInDays must be 7, 30, 90 or null")
		}
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		return 0, nil, err
	}
	l, err := s.store.CreateShareLink(q.r.Context(), q.user, q.ws, q.mapID, hash, expires)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, struct {
		store.ShareLink
		URL string `json:"url"`
	}{l, s.cfg.BaseURL + "/s/" + token}, nil
}

func (s *Server) revokeShareLink(q mapRequest) (int, any, error) {
	return 0, nil, s.store.RevokeShareLink(q.r.Context(), q.ws, q.mapID, q.id)
}

// sharedMap is the read-only map behind a share link. No session needed.
func (s *Server) sharedMap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	m, err := s.store.SharedMapByToken(r.Context(), auth.HashToken(r.PathValue("token")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}
