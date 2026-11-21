package httpapi_test

import (
	"strings"
	"testing"
)

func TestLinkedMaps(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	a := buildMap(owner)
	b := buildMap(owner)
	viewer, _ := h.join(owner, "vi@example.com", "viewer")
	made := owner.must(201, "POST", a.path+"/steps", map[string]any{"title": "Sign up", "activityId": a.act1}).body
	step := str(made, "step", "id")
	key := str(made, "step", "key")
	place := map[string]any{"stepId": step, "activityId": b.act2, "releaseId": b.rel}

	// Put the step on map B too, in a cell of B.
	viewer.must(403, "POST", b.path+"/placements", place)
	owner.must(400, "POST", b.path+"/placements", map[string]any{"stepId": step, "activityId": a.act1})
	pb := owner.must(201, "POST", b.path+"/placements", place).body
	if str(pb, "step", "key") != key || str(pb, "placement", "activityId") != b.act2 {
		t.Errorf("placed = %v", pb)
	}
	owner.must(409, "POST", b.path+"/placements", place)
	bv := owner.must(200, "GET", b.path, nil).body
	if titles(bv, "steps", "key")[0] != key || len(bv["placements"].([]any)) != 1 {
		t.Errorf("map B = %v", bv["steps"])
	}
	if e := bv["elsewhere"].([]any); len(e) != 1 || str(e[0], "mapId") != strings.TrimPrefix(a.path, acme+"/maps/") {
		t.Errorf("elsewhere on B = %v", e)
	}

	// One step, one key: an edit through B shows on A, and both maps' feeds have it.
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Sign up fast", "statusId": b.doing})
	av := owner.must(200, "GET", a.path, nil).body
	if titles(av, "steps", "title")[0] != "Sign up fast" {
		t.Errorf("A did not see the edit: %v", av["steps"])
	}
	for _, m := range []built{a, b} {
		e := owner.must(200, "GET", m.path+"/history", nil).body["entries"].([]any)[0]
		if str(e, "entity") != "step" || str(e, "action") != "update" {
			t.Errorf("%s newest event = %v", m.path, e)
		}
	}
	// The step's own history lists the edit once, whichever map asks.
	for _, m := range []built{a, b} {
		n := 0
		for _, e := range owner.must(200, "GET", m.path+"/steps/"+step+"/history", nil).body["entries"].([]any) {
			if str(e, "action") == "update" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("step history from %s has %d updates", m.path, n)
		}
	}
	// Comments count on both maps.
	owner.must(201, "POST", a.path+"/steps/"+step+"/comments", map[string]any{"bodyMd": "Looks good"})
	if st := owner.must(200, "GET", b.path, nil).body["steps"].([]any)[0]; st.(map[string]any)["commentCount"] != 1.0 {
		t.Errorf("comment count on B = %v", st)
	}

	// Deleting the activity on A keeps the step (it is on B) and hides it on A until the activity is back.
	owner.must(204, "DELETE", a.path+"/nodes/"+a.act1, nil)
	if av := owner.must(200, "GET", a.path, nil).body; len(av["steps"].([]any)) != 0 || len(av["placements"].([]any)) != 0 {
		t.Errorf("hidden step shown on A: %v", av["steps"])
	}
	if bv := owner.must(200, "GET", b.path, nil).body; len(bv["steps"].([]any)) != 1 || len(bv["elsewhere"].([]any)) != 0 {
		t.Errorf("B after A's activity went = %v %v", bv["steps"], bv["elsewhere"])
	}
	owner.must(200, "POST", a.path+"/nodes/"+a.act1+"/restore", map[string]any{})
	if av := owner.must(200, "GET", a.path, nil).body; len(av["steps"].([]any)) != 1 {
		t.Errorf("step not back on A: %v", av["steps"])
	}

	// Taking it off A leaves it on B; taking it off its last map is refused.
	viewer.must(403, "DELETE", a.path+"/steps/"+step+"/placement", nil)
	gone := owner.must(200, "DELETE", a.path+"/steps/"+step+"/placement", nil).body
	if str(gone, "activityId") != a.act1 {
		t.Errorf("removed placement = %v", gone)
	}
	if av := owner.must(200, "GET", a.path, nil).body; len(av["steps"].([]any)) != 0 {
		t.Errorf("still on A: %v", av["steps"])
	}
	r := owner.must(400, "DELETE", b.path+"/steps/"+step+"/placement", nil)
	if !strings.Contains(r.raw, "delete it instead") {
		t.Errorf("last placement = %s", r.raw)
	}

	// Put back, then delete everywhere through one map; restore brings it back on both.
	owner.must(201, "POST", a.path+"/placements", map[string]any{"stepId": step, "activityId": a.act1})
	owner.must(204, "DELETE", b.path+"/steps/"+step, nil)
	for _, m := range []built{a, b} {
		if v := owner.must(200, "GET", m.path, nil).body; len(v["steps"].([]any)) != 0 {
			t.Errorf("deleted step on %s: %v", m.path, v["steps"])
		}
	}
	owner.must(200, "POST", b.path+"/steps/"+step+"/restore", map[string]any{})
	if v := owner.must(200, "GET", a.path, nil).body; len(v["steps"].([]any)) != 1 {
		t.Errorf("restore missed A: %v", v["steps"])
	}

	// A map with another workflow cannot show it; another workspace's step cannot be placed.
	wf := owner.must(201, "POST", acme+"/workflows", map[string]any{"name": "Bugs"}).body
	c := owner.must(201, "POST", acme+"/maps", map[string]any{"name": "Bugs", "workflowId": str(wf, "id")}).body
	cPath := acme + "/maps/" + str(c, "id")
	cObj := str(owner.must(201, "POST", cPath+"/nodes", map[string]any{"level": "objective", "title": "Fix"}).body, "id")
	cAct := str(owner.must(201, "POST", cPath+"/nodes", map[string]any{"level": "activity", "title": "Triage", "parentId": cObj}).body, "id")
	if r := owner.must(400, "POST", cPath+"/placements", map[string]any{"stepId": step, "activityId": cAct}); !strings.Contains(r.raw, "workflow") {
		t.Errorf("workflow mismatch = %s", r.raw)
	}
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	far := buildMapIn(owner, "/api/workspaces/other")
	owner.must(400, "POST", far.path+"/placements", map[string]any{"stepId": step, "activityId": far.act1})

	// A share link of B does not name A.
	url := str(owner.must(201, "POST", b.path+"/share-links", map[string]any{}).body, "url")
	shared := h.client().must(200, "GET", "/api/shared/"+url[strings.LastIndex(url, "/s/")+3:], nil)
	if e := shared.body["view"].(map[string]any)["elsewhere"].([]any); len(e) != 0 {
		t.Errorf("shared elsewhere = %v", e)
	}
}
