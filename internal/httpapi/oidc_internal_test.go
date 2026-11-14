package httpapi

import "testing"

func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"/invites/abc":         "/invites/abc",
		"/w/acme/members?x=1":  "/w/acme/members?x=1",
		"":                     "/",
		"https://evil.example": "/",
		"//evil.example/x":     "/",
		`/\evil.example`:       "/",
		"evil":                 "/",
		"/%2F%2Fevil":          "/%2F%2Fevil", // stays a local path once the browser resolves it
	} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}
