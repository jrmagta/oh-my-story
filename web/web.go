// Package web serves the single-page app, built by `npm run build` into dist/
// and embedded in the binary. dist/.gitkeep is committed so the Go build works
// before the app has been built; the handler then explains what is missing.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Dist is the embedded build output.
func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // the embed pattern guarantees dist exists
	}
	return sub
}

// The app needs nothing from other origins: fonts are self-hosted and the API is same-origin.
const csp = "default-src 'self'; img-src 'self' data:; font-src 'self' data:; style-src 'self'; script-src 'self'; " +
	"connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Handler serves files from fsys. Any other path without a file extension is
// an app route, so it gets index.html and the client router takes over.
func Handler(fsys fs.FS) http.Handler {
	index, indexErr := fs.ReadFile(fsys, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		// Invite links carry their token in the path; never send it to another site.
		h.Set("Referrer-Policy", "no-referrer")

		if indexErr != nil {
			http.Error(w, "web UI not built: run `make web`, then rebuild the server", http.StatusServiceUnavailable)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && strings.HasPrefix(path.Base(name), ".") {
			http.NotFound(w, r) // .gitkeep and the like are build plumbing
			return
		}
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable") // content-hashed names
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				http.ServeFileFS(w, r, fsys, name)
				return
			}
			if path.Ext(name) != "" {
				http.NotFound(w, r) // a missing file, not an app route
				return
			}
		}
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
