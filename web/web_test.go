package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jrmagta/oh-my-story/web"
)

func get(t *testing.T, h http.Handler, path string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestHandler(t *testing.T) {
	h := web.Handler(fstest.MapFS{
		"index.html":        {Data: []byte("<html>app</html>")},
		"theme.js":          {Data: []byte("// theme")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
		".gitkeep":          {Data: nil},
	})

	for _, path := range []string{"/", "/login", "/w/acme/members", "/invites/tok_en-123", "/index.html"} {
		res, body := get(t, h, path)
		if res.StatusCode != 200 || body != "<html>app</html>" || res.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d %q cache=%q, want the app shell", path, res.StatusCode, body, res.Header.Get("Cache-Control"))
		}
		if !strings.Contains(res.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s: no CSP", path)
		}
		if res.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: referrer policy %q", path, res.Header.Get("Referrer-Policy"))
		}
	}

	res, body := get(t, h, "/assets/app-abc.js")
	if res.StatusCode != 200 || body != "console.log(1)" || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("hashed asset: %d %q cache=%q", res.StatusCode, body, res.Header.Get("Cache-Control"))
	}
	if res, _ := get(t, h, "/theme.js"); res.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("unhashed file cached as %q", res.Header.Get("Cache-Control"))
	}
	for _, path := range []string{"/assets/missing.js", "/.gitkeep", "/favicon.ico"} {
		if res, _ := get(t, h, path); res.StatusCode != 404 {
			t.Errorf("%s: %d, want 404", path, res.StatusCode)
		}
	}
}

func TestHandlerUnbuilt(t *testing.T) {
	res, body := get(t, web.Handler(fstest.MapFS{".gitkeep": {}}), "/")
	if res.StatusCode != 503 || !strings.Contains(body, "make web") {
		t.Errorf("unbuilt: %d %q", res.StatusCode, body)
	}
}
