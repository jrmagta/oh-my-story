package httpapi_test

import (
	"fmt"
	"slices"
	"testing"
)

type entry struct {
	id, entity, action, actor string
	before, after             map[string]any
}

func entries(r resp) ([]entry, any) {
	var out []entry
	for _, e := range r.body["entries"].([]any) {
		m := e.(map[string]any)
		b, _ := m["before"].(map[string]any)
		a, _ := m["after"].(map[string]any)
		out = append(out, entry{fmt.Sprint(m["id"]), str(m, "entity"), str(m, "action"), str(m, "actorName"), b, a})
	}
	return out, r.body["next"]
}

func kinds(es []entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.entity+":"+e.action)
	}
	return out
}

func TestStepHistory(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, _ := h.join(owner, "ed@example.com", "editor")
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Draft", "activityId": b.act1}).body, "step", "id")
	other := owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Other", "activityId": b.act1})
	editor.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Final", "statusId": b.doing})
	owner.must(200, "POST", b.path+"/steps/"+step+"/move", map[string]any{"activityId": b.act2, "releaseId": b.rel, "version": 1})
	owner.must(200, "PATCH", b.path+"/steps/"+str(other.body, "step", "id"), map[string]any{"title": "Not this one"})
	owner.must(204, "DELETE", b.path+"/steps/"+step, nil)

	hist := b.path + "/steps/" + step + "/history"
	es, next := entries(owner.must(200, "GET", hist, nil))
	if want := []string{"step:delete", "placement:move", "step:update", "placement:create", "step:create"}; !slices.Equal(kinds(es), want) || next != nil {
		t.Fatalf("history = %v next=%v, want %v", kinds(es), next, want)
	}
	if es[2].actor != "ed@example.com" || es[2].before["title"] != "Draft" || es[2].after["statusId"] != b.doing {
		t.Errorf("update entry = %+v", es[2])
	}
	if es[0].actor != "Olive Owner" || es[0].before["title"] != "Final" || es[0].before["key"] != "ACM-1" {
		t.Errorf("a delete names what it deleted: %+v", es[0])
	}

	// Pages of two, following next, cover the same entries in the same order.
	var paged []string
	cursor := ""
	for {
		es, next := entries(owner.must(200, "GET", hist+"?limit=2"+cursor, nil))
		paged = append(paged, kinds(es)...)
		if next == nil {
			break
		}
		cursor = fmt.Sprintf("&before=%.0f", next.(float64))
	}
	if all, _ := entries(owner.must(200, "GET", hist, nil)); !slices.Equal(paged, kinds(all)) {
		t.Errorf("paged %v", paged)
	}

	owner.must(400, "GET", hist+"?limit=0", nil)
	owner.must(400, "GET", hist+"?before=x", nil)

	// The map's feed has everything on the map, newest first.
	feed, _ := entries(owner.must(200, "GET", b.path+"/history?limit=3", nil))
	if want := []string{"step:delete", "step:update", "placement:move"}; !slices.Equal(kinds(feed), want) {
		t.Errorf("map feed = %v", kinds(feed))
	}
}

func TestHistoryTenancy(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "s", "activityId": b.act1}).body, "step", "id")
	viewer, _ := h.join(owner, "v@example.com", "viewer")
	viewer.must(200, "GET", b.path+"/steps/"+step+"/history", nil)
	viewer.must(200, "GET", b.path+"/history", nil)

	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	o := owner.must(201, "POST", "/api/workspaces/other/maps", map[string]any{"name": "O"})
	op := "/api/workspaces/other/maps/" + str(o.body, "id")
	owner.must(404, "GET", op+"/steps/"+step+"/history", nil)
	owner.must(404, "GET", acme+"/maps/"+str(o.body, "id")+"/history", nil)
	h.client().must(401, "GET", b.path+"/history", nil)
}
