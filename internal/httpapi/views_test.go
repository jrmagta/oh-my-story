package httpapi_test

import (
	"slices"
	"testing"
)

func viewNames(r resp) []string {
	var out []string
	for _, v := range r.list {
		out = append(out, str(v, "name"))
	}
	return out
}

func TestSavedViews(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, _ := h.join(owner, "ed@example.com", "editor")
	viewer, _ := h.join(owner, "vi@example.com", "viewer")
	views := b.path + "/views"
	filter := map[string]any{"labels": []string{"a"}, "statuses": []string{}, "assignees": []string{}, "personas": []string{},
		"releases": []string{"none"}, "text": "login", "mode": "hide"}

	// Anyone who can read the map saves private views; only editors share.
	mine := viewer.must(201, "POST", views, map[string]any{"name": "Mine", "filter": filter})
	if mine.body["shared"] != false || str(mine.body, "filter", "text") != "login" || str(mine.body, "creatorName") != "vi@example.com" {
		t.Errorf("created view = %v", mine.body)
	}
	viewer.must(403, "POST", views, map[string]any{"name": "x", "filter": filter, "shared": true})
	team := str(editor.must(201, "POST", views, map[string]any{"name": "Team", "filter": filter, "shared": true}).body, "id")
	editor.must(201, "POST", views, map[string]any{"name": "Draft", "filter": filter})

	// Each person sees shared views and their own.
	if got := viewNames(viewer.must(200, "GET", views, nil)); !slices.Equal(got, []string{"Mine", "Team"}) {
		t.Errorf("viewer sees %v", got)
	}
	if got := viewNames(owner.must(200, "GET", views, nil)); !slices.Equal(got, []string{"Team"}) {
		t.Errorf("owner sees %v", got)
	}

	// Only the creator changes a view; others get not found, even for shared ones.
	path := views + "/" + str(mine.body, "id")
	if r := viewer.must(200, "PATCH", path, map[string]any{"name": "Logins", "filter": map[string]any{"text": "", "mode": "highlight"}}); str(r.body, "name") != "Logins" || str(r.body, "filter", "mode") != "highlight" || len(r.body["filter"].(map[string]any)["labels"].([]any)) != 0 {
		t.Errorf("patched view = %v", r.body)
	}
	viewer.must(403, "PATCH", path, map[string]any{"shared": true})
	owner.must(404, "PATCH", path, map[string]any{"name": "x"})
	owner.must(404, "PATCH", views+"/"+team, map[string]any{"name": "x"})

	// Filters are checked for shape.
	for _, f := range []any{nil, "x", map[string]any{"mode": "blink"}, map[string]any{"mode": "hide", "colour": "red"}, map[string]any{"mode": "hide", "labels": "a"}} {
		viewer.must(400, "POST", views, map[string]any{"name": "bad", "filter": f})
	}

	// The creator deletes their own; admins delete shared views but not private ones.
	viewer.must(404, "DELETE", views+"/"+team, nil)
	owner.must(404, "DELETE", path, nil)
	owner.must(204, "DELETE", views+"/"+team, nil)
	viewer.must(204, "DELETE", path, nil)
	if got := viewNames(viewer.must(200, "GET", views, nil)); len(got) != 0 {
		t.Errorf("after deletes viewer sees %v", got)
	}

	// Another workspace's map is not found.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	other := str(owner.must(201, "POST", "/api/workspaces/other/maps", map[string]any{"name": "O"}).body, "id")
	owner.must(404, "GET", acme+"/maps/"+other+"/views", nil)
	owner.must(404, "POST", acme+"/maps/"+other+"/views", map[string]any{"name": "x", "filter": filter})
}
