package httpapi_test

import "testing"

func TestStoryEstimate(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	story := str(owner.must(201, "POST", b.path+"/stories", map[string]any{"title": "s", "activityId": b.act1}).body, "story", "id")
	path := b.path + "/stories/" + story

	if r := owner.must(200, "GET", b.path, nil); r.body["stories"].([]any)[0].(map[string]any)["estimate"] != nil {
		t.Errorf("a new story has no estimate: %v", r.body["stories"])
	}
	for _, v := range []float64{3, 0.5, 9999.99, 0} {
		if r := owner.must(200, "PATCH", path, map[string]any{"estimate": v}); r.body["estimate"] != v {
			t.Errorf("estimate %v: got %v", v, r.body["estimate"])
		}
	}
	owner.must(200, "PATCH", path, map[string]any{"estimate": 2.25})
	// A patch without estimate leaves it alone; null clears it.
	if r := owner.must(200, "PATCH", path, map[string]any{"title": "t"}); r.body["estimate"] != 2.25 {
		t.Errorf("estimate changed by a title patch: %v", r.body["estimate"])
	}
	if r := owner.must(200, "PATCH", path, map[string]any{"estimate": nil}); r.body["estimate"] != nil {
		t.Errorf("estimate not cleared: %v", r.body["estimate"])
	}
	for _, v := range []any{-1, 10000, 1.234, "3"} {
		owner.must(400, "PATCH", path, map[string]any{"estimate": v})
	}

	es, _ := entries(owner.must(200, "GET", path+"/history?limit=2", nil))
	if len(es) != 2 || es[0].before["estimate"] != 2.25 || es[0].after["estimate"] != nil || es[1].after["title"] != "t" {
		t.Errorf("estimate changes are logged: %+v", es)
	}
	if _, ok := es[1].after["estimate"]; ok {
		t.Errorf("an unchanged estimate is not logged: %+v", es[1])
	}

	viewer, _ := h.join(owner, "v@example.com", "viewer")
	viewer.must(403, "PATCH", path, map[string]any{"estimate": 1})
}
