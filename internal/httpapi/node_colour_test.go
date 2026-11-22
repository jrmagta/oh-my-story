package httpapi_test

import "testing"

func TestNodeColour(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	path := b.path + "/nodes/" + b.act1

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

func TestStepColour(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	created := owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Email field", "activityId": b.act1})
	if v, ok := created.body["step"].(map[string]any)["colour"]; !ok || v != nil {
		t.Errorf("a new step has no colour of its own: %v", created.body)
	}
	path := b.path + "/steps/" + str(created.body, "step", "id")

	if r := owner.must(200, "PATCH", path, map[string]any{"colour": "#3B82C4"}); r.body["colour"] != "#3B82C4" {
		t.Errorf("colour not set: %v", r.body)
	}
	if r := owner.must(200, "PATCH", path, map[string]any{"title": "Email"}); r.body["colour"] != "#3B82C4" {
		t.Errorf("colour changed by a title patch: %v", r.body)
	}
	if r := owner.must(200, "PATCH", path, map[string]any{"colour": nil}); r.body["colour"] != nil {
		t.Errorf("colour not cleared: %v", r.body)
	}
	owner.must(400, "PATCH", path, map[string]any{"colour": "blue"})
}
