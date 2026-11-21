package httpapi_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func hitKeys(r resp) []string {
	var out []string
	for _, h := range r.list {
		out = append(out, str(h, "key"))
	}
	return out
}

func TestSearch(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	viewer, _ := h.join(owner, "v@example.com", "viewer")
	add := func(title, desc string) string {
		return str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": title, "descriptionMd": desc, "activityId": b.act1}).body, "step", "id")
	}
	add("Email login", "Send a magic link")                // ACM-1
	add("Password login", "Hash with argon2id")            // ACM-2
	gone := add("Logout", "Clear the session everywhere")  // ACM-3
	add("Reset password", "Email a link mentioning ACM-1") // ACM-4
	owner.must(204, "DELETE", b.path+"/steps/"+gone, nil)

	search := func(q string) []string {
		return hitKeys(viewer.must(200, "GET", acme+"/search?q="+q, nil))
	}
	// Words match the start of words in titles and descriptions; every word must match.
	if got := search("passw"); !slices.Equal(got, []string{"ACM-2", "ACM-4"}) && !slices.Equal(got, []string{"ACM-4", "ACM-2"}) {
		t.Errorf("passw = %v", got)
	}
	if got := search("argon"); !slices.Equal(got, []string{"ACM-2"}) {
		t.Errorf("argon (description) = %v", got)
	}
	if got := search("email+link"); len(got) != 2 {
		t.Errorf("email link = %v", got)
	}
	if got := search("email+argon"); len(got) != 0 {
		t.Errorf("email argon = %v", got)
	}
	// A key comes first, ignoring case, ahead of a step that only mentions it.
	if got := search("acm-1"); len(got) == 0 || got[0] != "ACM-1" {
		t.Errorf("acm-1 = %v", got)
	}
	// Deleted steps, and steps on deleted maps, are not found.
	if got := search("session"); len(got) != 0 {
		t.Errorf("deleted step found: %v", got)
	}
	r := viewer.must(200, "GET", acme+"/search?q=argon", nil)
	if str(r.list[0], "mapName") != "Onboarding" || str(r.list[0], "activity") != "Enter email" || str(r.list[0], "mapId") == "" {
		t.Errorf("hit = %v", r.list[0])
	}
	if got := search("+"); len(got) != 0 {
		t.Errorf("no words = %v", got)
	}
	viewer.must(400, "GET", acme+"/search?q="+strings.Repeat("a", 201), nil)

	// Other workspaces' steps never appear.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	o := buildMapIn(owner, "/api/workspaces/other")
	owner.must(201, "POST", o.path+"/steps", map[string]any{"title": "Password vault", "activityId": o.act1})
	if got := search("vault"); len(got) != 0 {
		t.Errorf("other workspace leaked: %v", got)
	}

	// At most 20 hits.
	for i := range 25 {
		add(fmt.Sprintf("Bulk %d", i), "")
	}
	if got := search("bulk"); len(got) != 20 {
		t.Errorf("bulk hits = %d", len(got))
	}
	owner.must(204, "DELETE", b.path, nil)
	if got := search("argon"); len(got) != 0 {
		t.Errorf("deleted map's steps found: %v", got)
	}
}
