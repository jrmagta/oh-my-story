package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/mail"
	"github.com/jrmagta/oh-my-story/internal/store"
)

const resetLifetime = time.Hour

// requestReset always answers 204, so the form never says whether an account
// exists. The lookup and the email happen after the reply, so neither does
// the time it takes. Only active local accounts get an email; see
// store.CreatePasswordReset.
func (s *Server) requestReset(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email string }
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	email, err := parseEmail(in.Email)
	if err != nil {
		writeError(w, err)
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		writeError(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		defer cancel()
		expires := time.Now().Add(resetLifetime)
		u, err := s.store.CreatePasswordReset(ctx, email, hash, expires)
		if errors.Is(err, store.ErrNotFound) {
			return
		}
		if err != nil {
			slog.Error("create password reset", "err", err)
			return
		}
		msg := mail.Message{
			To:      u.Email,
			Subject: "Reset your oh-my-story password",
			Body: fmt.Sprintf("Someone asked to reset the password for %s on oh-my-story.\n\nChoose a new password:\n%s\n\n"+
				"The link works once, for one hour. Setting a new password signs you out everywhere else.\n"+
				"If you did not ask for this, ignore this email; your password stays as it is.\n",
				u.Email, s.cfg.BaseURL+"/reset/"+token),
		}
		if err := s.mail.Send(ctx, msg); err != nil {
			slog.Error("send password reset email", "err", err)
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getReset(w http.ResponseWriter, r *http.Request) {
	email, err := s.store.PasswordResetEmail(r.Context(), auth.HashToken(r.PathValue("token")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": email})
}

// resetPassword sets the new password, ends all the user's sessions and signs
// this browser in.
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string }
	if err := readJSON(w, r, &in); err != nil {
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
	u, err := s.store.ResetPassword(r.Context(), auth.HashToken(r.PathValue("token")), hash)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}
