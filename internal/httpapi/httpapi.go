// Package httpapi is the HTTP surface of the server.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger is the slice of *pgxpool.Pool the health check needs.
type Pinger interface {
	Ping(ctx context.Context) error
}

func NewHandler(db Pinger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": "unreachable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "ok"})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
