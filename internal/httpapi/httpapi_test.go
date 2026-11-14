package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jrmagta/oh-my-story/internal/db"
	"github.com/jrmagta/oh-my-story/internal/db/dbtest"
	"github.com/jrmagta/oh-my-story/internal/httpapi"
)

func getHealth(t *testing.T, h http.Handler) (int, map[string]string) {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	res, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}

func TestHealthz(t *testing.T) {
	ctx := context.Background()

	pool, err := db.Open(ctx, dbtest.NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if code, body := getHealth(t, httpapi.NewHandler(pool)); code != 200 || body["database"] != "ok" {
		t.Errorf("reachable database: got %d %v, want 200 database=ok", code, body)
	}

	// Nothing listens on port 1, so the ping fails.
	dead, err := db.Open(ctx, "postgres://oms:oms@127.0.0.1:1/oms?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer dead.Close()
	if code, body := getHealth(t, httpapi.NewHandler(dead)); code != 503 || body["database"] != "unreachable" {
		t.Errorf("unreachable database: got %d %v, want 503 database=unreachable", code, body)
	}
}
