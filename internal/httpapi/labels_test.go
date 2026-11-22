package httpapi_test

import (
	"slices"
	"testing"
)

func labelIDs(step map[string]any) []string {
	var out []string
	for _, v := range step["labelIds"].([]any) {
		out = append(out, v.(string))
	}
	return out
}

func TestLabels(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, _ := h.join(owner, "ed@example.com", "editor")
	commenter, _ := h.join(owner, "co@example.com", "commenter")
	labels := acme + "/labels"

	// Editors create labels; names are unique ignoring case. Labels have no colour.
	auth := str(editor.must(201, "POST", labels, map[string]any{"name": "auth"}).body, "id")
	email := editor.must(201, "POST", labels, map[string]any{"name": " Email "})
	if _, coloured := email.body["colour"]; str(email.body, "name") != "Email" || coloured {
		t.Errorf("created label = %v", email.body)
	}
	editor.must(409, "POST", labels, map[string]any{"name": "AUTH"})
	commenter.must(403, "POST", labels, map[string]any{"name": "x"})
	for _, body := range []map[string]any{{"name": ""}, {"name": "x", "colour": "#3B82C4"}, {"name": string(make([]byte, 51))}} {
		editor.must(400, "POST", labels, body)
	}
	if r := commenter.must(200, "GET", labels, nil); len(r.list) != 2 || str(r.list[0], "name") != "auth" {
		t.Errorf("labels = %s", r.raw)
	}

	// A step's labels are replaced as a set and come back sorted.
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "s", "activityId": b.act1}).body, "step", "id")
	path := b.path + "/steps/" + step
	if r := owner.must(200, "GET", b.path, nil); len(labelIDs(r.body["steps"].([]any)[0].(map[string]any))) != 0 {
		t.Errorf("a new step has no labels: %v", r.body["steps"])
	}
	both := []string{auth, str(email.body, "id")}
	slices.Sort(both)
	if r := editor.must(200, "PATCH", path, map[string]any{"labelIds": []string{both[1], both[0]}}); !slices.Equal(labelIDs(r.body), both) {
		t.Errorf("labelIds = %v, want %v", r.body["labelIds"], both)
	}
	if r := editor.must(200, "PATCH", path, map[string]any{"title": "t"}); len(labelIDs(r.body)) != 2 {
		t.Errorf("a patch without labelIds changed them: %v", r.body["labelIds"])
	}
	editor.must(400, "PATCH", path, map[string]any{"labelIds": []string{auth, auth}})
	editor.must(400, "PATCH", path, map[string]any{"labelIds": []string{b.act1}})
	commenter.must(403, "PATCH", path, map[string]any{"labelIds": []string{}})

	es, _ := entries(owner.must(200, "GET", path+"/history?limit=2", nil))
	if es[1].action != "update" || len(es[1].before["labelIds"].([]any)) != 0 || len(es[1].after["labelIds"].([]any)) != 2 {
		t.Errorf("label change is a step update: %+v", es[1])
	}

	// Admins rename; editors cannot.
	editor.must(403, "PATCH", labels+"/"+auth, map[string]any{"name": "login"})
	if r := owner.must(200, "PATCH", labels+"/"+auth, map[string]any{"name": "login"}); str(r.body, "name") != "login" {
		t.Errorf("patched label = %v", r.body)
	}
	owner.must(409, "PATCH", labels+"/"+auth, map[string]any{"name": "EMAIL"})

	// Deleting a label takes it off its steps.
	editor.must(403, "DELETE", labels+"/"+auth, nil)
	owner.must(204, "DELETE", labels+"/"+auth, nil)
	view := owner.must(200, "GET", b.path, nil).body
	if got := labelIDs(view["steps"].([]any)[0].(map[string]any)); !slices.Equal(got, []string{str(email.body, "id")}) {
		t.Errorf("after delete, labelIds = %v", got)
	}
	if ls := view["labels"].([]any); len(ls) != 1 || str(ls[0], "name") != "Email" {
		t.Errorf("map labels = %v", ls)
	}
	owner.must(404, "DELETE", labels+"/"+auth, nil)

	// Another workspace's labels are not found and cannot be put on steps.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	other := str(owner.must(201, "POST", "/api/workspaces/other/labels", map[string]any{"name": "x"}).body, "id")
	owner.must(404, "PATCH", labels+"/"+other, map[string]any{"name": "y"})
	owner.must(400, "PATCH", path, map[string]any{"labelIds": []string{other}})
}
