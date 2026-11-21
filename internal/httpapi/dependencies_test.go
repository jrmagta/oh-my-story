package httpapi_test

import (
	"strings"
	"testing"
)

func TestDependencies(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	other := buildMap(owner) // a second map in the same workspace
	viewer, _ := h.join(owner, "vi@example.com", "viewer")
	step := func(m built, title string) string {
		return str(owner.must(201, "POST", m.path+"/steps", map[string]any{"title": title, "activityId": m.act1}).body, "step", "id")
	}
	a, bb, c := step(b, "Design"), step(b, "Build"), step(b, "Ship")
	x := step(other, "Legal review")
	deps := b.path + "/dependencies"
	link := func(path, blocker, blocked string) map[string]any {
		return owner.must(201, "POST", path, map[string]any{"blockerId": blocker, "blockedId": blocked}).body
	}

	ab := link(deps, a, bb)
	view := owner.must(200, "GET", b.path, nil).body
	if d := view["dependencies"].([]any); len(d) != 1 || str(d[0], "blockerId") != a || str(d[0], "blockedId") != bb {
		t.Errorf("dependencies = %v", view["dependencies"])
	}
	if refs := view["otherSteps"].([]any); len(refs) != 0 {
		t.Errorf("otherSteps = %v", refs)
	}

	// Loops, self-links and repeats are refused; viewers cannot link.
	owner.must(400, "POST", deps, map[string]any{"blockerId": bb, "blockedId": a})
	link(deps, bb, c)
	r := owner.must(400, "POST", deps, map[string]any{"blockerId": c, "blockedId": a})
	if !strings.Contains(r.raw, "loop") {
		t.Errorf("cycle error = %s", r.raw)
	}
	owner.must(400, "POST", deps, map[string]any{"blockerId": a, "blockedId": a})
	owner.must(409, "POST", deps, map[string]any{"blockerId": a, "blockedId": bb})
	owner.must(400, "POST", deps, map[string]any{"blockerId": a})
	viewer.must(403, "POST", deps, map[string]any{"blockerId": c, "blockedId": x})

	// Across maps: each map sees the link and names the far end.
	xa := link(deps, x, a)
	refs := owner.must(200, "GET", b.path, nil).body["otherSteps"].([]any)
	if len(refs) != 1 || str(refs[0], "key") != "ACM-4" || refs[0].(map[string]any)["done"] != false || str(refs[0], "mapId") == "" {
		t.Errorf("otherSteps = %v", refs)
	}
	ov := owner.must(200, "GET", other.path, nil).body
	if len(ov["dependencies"].([]any)) != 1 || str(ov["otherSteps"].([]any)[0], "id") != a {
		t.Errorf("other map = %v %v", ov["dependencies"], ov["otherSteps"])
	}
	// Finishing the blocker on its own map shows on the map that only links to it, live too.
	owner.must(200, "PATCH", other.path+"/steps/"+x, map[string]any{"statusId": other.doneID})
	if refs := owner.must(200, "GET", b.path, nil).body["otherSteps"].([]any); refs[0].(map[string]any)["done"] != true {
		t.Errorf("done not seen: %v", refs)
	}
	hist := owner.must(200, "GET", b.path+"/history", nil).body["entries"].([]any)
	if str(hist[0], "entity") != "stepref" || hist[0].(map[string]any)["after"].(map[string]any)["done"] != true {
		t.Errorf("newest map event = %v", hist[0])
	}

	// A link needs one end on the map it is made through.
	o3 := buildMap(owner)
	owner.must(400, "POST", o3.path+"/dependencies", map[string]any{"blockerId": c, "blockedId": x})

	// Another workspace's steps cannot be linked.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	far := buildMapIn(owner, "/api/workspaces/other")
	y := str(owner.must(201, "POST", far.path+"/steps", map[string]any{"title": "Elsewhere", "activityId": far.act1}).body, "step", "id")
	owner.must(400, "POST", deps, map[string]any{"blockerId": y, "blockedId": a})

	// The step's history lists its links.
	sh := owner.must(200, "GET", b.path+"/steps/"+a+"/history", nil).body["entries"].([]any)
	n := 0
	for _, e := range sh {
		if str(e, "entity") == "dependency" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("step history has %d links: %v", n, sh)
	}

	// A share link shows links within the map only.
	url := str(owner.must(201, "POST", b.path+"/share-links", map[string]any{}).body, "url")
	shared := h.client().must(200, "GET", "/api/shared/"+url[strings.LastIndex(url, "/s/")+3:], nil)
	if sv := shared.body["view"].(map[string]any); len(sv["dependencies"].([]any)) != 2 || len(sv["otherSteps"].([]any)) != 0 ||
		strings.Contains(shared.raw, "Legal review") {
		t.Errorf("shared = %v %v", sv["dependencies"], sv["otherSteps"])
	}

	// Removing: through a map that shows one end; a second time is not found.
	owner.must(404, "DELETE", o3.path+"/dependencies/"+str(xa, "id"), nil)
	owner.must(204, "DELETE", other.path+"/dependencies/"+str(xa, "id"), nil)
	owner.must(204, "DELETE", deps+"/"+str(ab, "id"), nil)
	owner.must(404, "DELETE", deps+"/"+str(ab, "id"), nil)
	viewer.must(403, "DELETE", deps+"/"+str(ab, "id"), nil)
	if d := owner.must(200, "GET", b.path, nil).body["dependencies"].([]any); len(d) != 1 {
		t.Errorf("after delete = %v", d)
	}

	// A deleted step's links drop out of the map until it is restored.
	owner.must(204, "DELETE", b.path+"/steps/"+c, nil)
	if d := owner.must(200, "GET", b.path, nil).body["dependencies"].([]any); len(d) != 0 {
		t.Errorf("deleted step's link shown: %v", d)
	}
}
