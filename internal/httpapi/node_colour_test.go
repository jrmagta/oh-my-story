package httpapi_test

import "testing"

func TestNodeColour(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	path := b.path + "/nodes/" + b.act

	if r := owner.must(200, "PATCH", path, map[string]any{"colour": "#3B82C4"}); r.body["colour"] != "#3B82C4" {
		t.Errorf("colour not set: %v", r.body)
	}
	// A patch without colour keeps it; null clears it.
	if r := owner.must(200, "PATCH", path, map[string]any{"title": "Join"}); r.body["colour"] != "#3B82C4" {
		t.Errorf("colour changed by a title patch: %v", r.body)
	}
	if r := owner.must(200, "PATCH", path, map[string]any{"colour": nil}); r.body["colour"] != nil {
		t.Errorf("colour not cleared: %v", r.body)
	}
	owner.must(400, "PATCH", path, map[string]any{"colour": "blue"})
	owner.must(400, "PATCH", path, map[string]any{"colour": ""})

	es, _ := entries(owner.must(200, "GET", b.path+"/history?limit=1", nil))
	if len(es) != 1 || es[0].entity != "node" || es[0].before["colour"] != "#3B82C4" || es[0].after["colour"] != nil {
		t.Errorf("clearing is logged: %+v", es)
	}
}
