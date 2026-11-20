package httpapi_test

import (
	"slices"
	"testing"
)

func ids(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestPersonas(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, _ := h.join(owner, "ed@example.com", "editor")
	commenter, _ := h.join(owner, "co@example.com", "commenter")
	personas := acme + "/personas"

	// Editors manage personas; names are unique ignoring case.
	admin := editor.must(201, "POST", personas, map[string]any{"name": "Admin", "description": "Sets up the workspace", "colour": "#3B82C4"})
	if str(admin.body, "description") != "Sets up the workspace" || str(admin.body, "colour") != "#3B82C4" {
		t.Errorf("created persona = %v", admin.body)
	}
	guest := str(editor.must(201, "POST", personas, map[string]any{"name": "Guest"}).body, "id")
	editor.must(409, "POST", personas, map[string]any{"name": "admin"})
	commenter.must(403, "POST", personas, map[string]any{"name": "x"})
	editor.must(400, "POST", personas, map[string]any{"name": " "})
	if r := editor.must(200, "PATCH", personas+"/"+guest, map[string]any{"description": "Browses"}); str(r.body, "name") != "Guest" || str(r.body, "description") != "Browses" {
		t.Errorf("patched persona = %v", r.body)
	}
	if r := commenter.must(200, "GET", personas, nil); len(r.list) != 2 {
		t.Errorf("personas = %s", r.raw)
	}

	// Only objectives take personas; a new node has none.
	obj := b.path + "/nodes/" + b.obj
	both := []string{str(admin.body, "id"), guest}
	slices.Sort(both)
	if r := owner.must(200, "GET", b.path, nil); len(ids(r.body["nodes"].([]any)[0].(map[string]any)["personaIds"])) != 0 {
		t.Errorf("a new node has no personas: %v", r.body["nodes"])
	}
	if r := editor.must(200, "PATCH", obj, map[string]any{"personaIds": []string{both[1], both[0]}}); !slices.Equal(ids(r.body["personaIds"]), both) {
		t.Errorf("personaIds = %v, want %v", r.body["personaIds"], both)
	}
	editor.must(400, "PATCH", b.path+"/nodes/"+b.act1, map[string]any{"personaIds": []string{guest}})
	editor.must(400, "PATCH", obj, map[string]any{"personaIds": []string{guest, guest}})
	editor.must(400, "PATCH", obj, map[string]any{"personaIds": []string{b.act1}})
	commenter.must(403, "PATCH", obj, map[string]any{"personaIds": []string{}})

	es, _ := entries(owner.must(200, "GET", b.path+"/history?limit=1", nil))
	if es[0].entity != "node" || len(es[0].after["personaIds"].([]any)) != 2 {
		t.Errorf("persona change is a node update: %+v", es[0])
	}

	// Deleting a persona unlinks it from objectives.
	editor.must(204, "DELETE", personas+"/"+guest, nil)
	view := owner.must(200, "GET", b.path, nil).body
	for _, n := range view["nodes"].([]any) {
		if str(n, "id") == b.obj && !slices.Equal(ids(n.(map[string]any)["personaIds"]), []string{str(admin.body, "id")}) {
			t.Errorf("after delete, personaIds = %v", n)
		}
	}
	if ps := view["personas"].([]any); len(ps) != 1 || str(ps[0], "name") != "Admin" {
		t.Errorf("map personas = %v", ps)
	}

	// Another workspace's personas are not found and cannot be linked.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	other := str(owner.must(201, "POST", "/api/workspaces/other/personas", map[string]any{"name": "x"}).body, "id")
	owner.must(404, "DELETE", personas+"/"+other, nil)
	owner.must(400, "PATCH", obj, map[string]any{"personaIds": []string{other}})
}
