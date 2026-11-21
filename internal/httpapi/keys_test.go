package httpapi_test

import (
	"strings"
	"testing"
)

func TestProjectKeys(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()

	if r := owner.must(200, "GET", "/api/project-keys?name=Cheshire+Cat&key=CHC", nil); str(r.body, "suggestion") != "CHC" || r.body["available"] != true {
		t.Errorf("before = %v", r.body)
	}
	ws := owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Cheshire Cat", "slug": "cat", "keyPrefix": "CHC"})
	if str(ws.body, "keyPrefix") != "CHC" {
		t.Errorf("created = %v", ws.body)
	}
	if r := owner.must(200, "GET", "/api/project-keys?name=Cheshire+Cat&key=CHC", nil); str(r.body, "suggestion") != "CHE" || r.body["available"] != false {
		t.Errorf("after = %v", r.body)
	}
	if r := owner.must(200, "GET", "/api/project-keys?name=Cheshire+Cat", nil); r.body["available"] != nil {
		t.Errorf("without key, available is left out: %v", r.body)
	}
	owner.must(400, "GET", "/api/project-keys?key=chc", nil)
	h.client().must(401, "GET", "/api/project-keys?name=x", nil)

	// Conflicts say which field is taken.
	r := owner.must(409, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other", "keyPrefix": "CHC"})
	if !strings.Contains(r.raw, "project key is taken") {
		t.Errorf("key conflict = %s", r.raw)
	}
	r = owner.must(409, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "cat", "keyPrefix": "OTH"})
	if !strings.Contains(r.raw, "URL slug is taken") {
		t.Errorf("slug conflict = %s", r.raw)
	}
	owner.must(400, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other", "keyPrefix": "1AB"})
	// Without a key, the workspace takes the suggestion.
	if r := owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Cheshire Cat", "slug": "cat-2"}); str(r.body, "keyPrefix") != "CHE" {
		t.Errorf("default key = %v", r.body)
	}
	owner.must(409, "PATCH", "/api/workspaces/cat-2", map[string]string{"keyPrefix": "CHC"})
}
