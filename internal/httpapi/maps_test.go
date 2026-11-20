package httpapi_test

import (
	"slices"
	"testing"
)

const acme = "/api/workspaces/acme"

func str(v any, path ...string) string {
	for _, k := range path {
		v = v.(map[string]any)[k]
	}
	if v == nil {
		return ""
	}
	return v.(string)
}

// built is a map with one objective, two activities and a release.
type built struct {
	path                         string // the map's API path
	obj, act1, act2, rel         string
	todo, doing, blocked, doneID string
}

func buildMap(c *client) built {
	m := c.must(201, "POST", acme+"/maps", map[string]any{"name": "Onboarding"})
	b := built{path: acme + "/maps/" + str(m.body, "id")}
	b.obj = str(c.must(201, "POST", b.path+"/nodes", map[string]any{"level": "objective", "title": "Grow"}).body, "id")
	b.act1 = str(c.must(201, "POST", b.path+"/nodes", map[string]any{"level": "activity", "title": "Enter email", "parentId": b.obj}).body, "id")
	b.act2 = str(c.must(201, "POST", b.path+"/nodes", map[string]any{"level": "activity", "title": "Verify", "parentId": b.obj}).body, "id")
	b.rel = str(c.must(201, "POST", b.path+"/releases", map[string]any{"name": "MVP", "targetDate": "2026-12-31"}).body, "id")
	statuses := c.must(200, "GET", b.path, nil).body["workflow"].(map[string]any)["statuses"].([]any)
	b.todo, b.doing, b.blocked, b.doneID = str(statuses[0], "id"), str(statuses[1], "id"), str(statuses[2], "id"), str(statuses[3], "id")
	return b
}

func titles(view map[string]any, key, field string) []string {
	var out []string
	for _, v := range view[key].([]any) {
		out = append(out, str(v, field))
	}
	return out
}

func TestMapRoundTrip(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	me := str(owner.must(200, "GET", "/api/me", nil).body, "user", "id")
	b := buildMap(owner)

	created := owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Email field", "activityId": b.act1})
	step := str(created.body, "step", "id")
	if str(created.body, "step", "key") != "ACME-1" || str(created.body, "step", "statusId") != b.todo {
		t.Errorf("created step = %v", created.body)
	}
	second := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Second", "activityId": b.act1, "beforeId": step}).body, "step", "id")

	patched := owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{
		"title": "Email input", "descriptionMd": "## Notes", "statusId": b.doneID, "assigneeId": me,
	})
	if str(patched.body, "title") != "Email input" || str(patched.body, "assigneeId") != me || patched.body["version"] != 1.0 {
		t.Errorf("patched step = %v", patched.body)
	}
	if r := owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"assigneeId": nil}); r.body["assigneeId"] != nil {
		t.Errorf("assignee not cleared: %v", r.body)
	}

	moved := owner.must(200, "POST", b.path+"/steps/"+step+"/move",
		map[string]any{"activityId": b.act2, "releaseId": b.rel, "version": 0})
	if moved.body["stale"] != true || str(moved.body, "placement", "releaseId") != b.rel {
		t.Errorf("move = %v", moved.body)
	}

	view := owner.must(200, "GET", b.path, nil).body
	if got := titles(view, "nodes", "title"); len(got) != 3 {
		t.Errorf("nodes = %v", got)
	}
	if got := titles(view, "steps", "title"); !slices.Equal(got, []string{"Email input", "Second"}) {
		t.Errorf("steps = %v", got)
	}
	if view["lastEventId"].(float64) < 1 || str(view, "map", "name") != "Onboarding" {
		t.Errorf("view map/lastEventId = %v / %v", view["map"], view["lastEventId"])
	}
	for _, p := range view["placements"].([]any) {
		if str(p, "stepId") == step && (str(p, "activityId") != b.act2 || str(p, "releaseId") != b.rel) {
			t.Errorf("placement = %v", p)
		}
	}

	if r := owner.must(200, "PATCH", b.path+"/releases/"+b.rel, map[string]any{"name": "V1"}); str(r.body, "targetDate") != "2026-12-31" {
		t.Errorf("omitting targetDate changed it: %v", r.body)
	}
	if r := owner.must(200, "PATCH", b.path+"/releases/"+b.rel, map[string]any{"targetDate": nil}); r.body["targetDate"] != nil {
		t.Errorf("targetDate not cleared: %v", r.body)
	}
	rel2 := str(owner.must(201, "POST", b.path+"/releases", map[string]any{"name": "Later"}).body, "id")
	owner.must(200, "POST", b.path+"/releases/"+rel2+"/move", map[string]any{"beforeId": b.rel})
	if got := titles(owner.must(200, "GET", b.path, nil).body, "releases", "name"); !slices.Equal(got, []string{"Later", "V1"}) {
		t.Errorf("releases = %v", got)
	}

	owner.must(200, "PATCH", b.path+"/nodes/"+b.act2, map[string]any{"title": "Check inbox", "colour": "#FCB131"})
	owner.must(200, "POST", b.path+"/nodes/"+b.act2+"/move", map[string]any{"parentId": b.obj, "beforeId": b.act1})

	owner.must(204, "DELETE", b.path+"/steps/"+second, nil)
	if got := titles(owner.must(200, "GET", b.path, nil).body, "steps", "title"); !slices.Equal(got, []string{"Email input"}) {
		t.Errorf("after delete: %v", got)
	}
	owner.must(200, "POST", b.path+"/steps/"+second+"/restore", map[string]any{})

	owner.must(204, "DELETE", b.path+"/nodes/"+b.obj, nil)
	view = owner.must(200, "GET", b.path, nil).body
	if len(view["steps"].([]any)) != 0 || len(view["nodes"].([]any)) != 0 {
		t.Errorf("after deleting the objective: nodes %v steps %v", view["nodes"], view["steps"])
	}
	owner.must(200, "POST", b.path+"/nodes/"+b.obj+"/restore", map[string]any{})
	if got := titles(owner.must(200, "GET", b.path, nil).body, "steps", "title"); len(got) != 2 {
		t.Errorf("after restore: %v", got)
	}

	if r := owner.must(200, "GET", acme+"/maps", nil); len(r.list) != 1 {
		t.Errorf("maps = %v", r.raw)
	}
	owner.must(204, "DELETE", b.path, nil)
	owner.must(404, "GET", b.path, nil)
}

func TestWorkflowRoundTrip(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "s", "activityId": b.act1}).body, "step", "id")

	wf := owner.must(201, "POST", acme+"/workflows", map[string]any{"name": "Design"})
	wfID := str(wf.body, "id")
	if n := len(wf.body["statuses"].([]any)); n != 4 {
		t.Errorf("new workflow has %d statuses", n)
	}
	owner.must(200, "PATCH", acme+"/workflows/"+wfID, map[string]any{"name": "Design flow"})

	review := owner.must(201, "POST", acme+"/workflows/"+wfID+"/statuses",
		map[string]any{"name": "Review", "category": "doing", "colour": "#FCB131"})
	reviewID := str(review.body, "id")
	owner.must(200, "PATCH", acme+"/statuses/"+reviewID, map[string]any{"name": "In review"})
	owner.must(409, "POST", acme+"/workflows/"+wfID+"/statuses", map[string]any{"name": "In review", "category": "doing"})
	first := str(wf.body["statuses"].([]any)[0], "id")
	owner.must(200, "POST", acme+"/statuses/"+reviewID+"/move", map[string]any{"beforeId": first})
	wfs := owner.must(200, "GET", acme+"/workflows", nil)
	if len(wfs.list) != 2 || str(wfs.list[1].(map[string]any)["statuses"].([]any)[0], "name") != "In review" {
		t.Errorf("workflows = %s", wfs.raw)
	}
	owner.must(204, "DELETE", acme+"/statuses/"+reviewID, nil)

	// The step's status is in use: deleting needs a replacement from the same workflow.
	if r := owner.must(400, "DELETE", acme+"/statuses/"+b.todo, nil); r.errCode() != "bad_request" {
		t.Errorf("delete in use = %s", r.raw)
	}
	owner.must(400, "DELETE", acme+"/statuses/"+b.todo+"?replacementId="+first, nil)
	owner.must(204, "DELETE", acme+"/statuses/"+b.todo+"?replacementId="+b.doing, nil)
	for _, s := range owner.must(200, "GET", b.path, nil).body["steps"].([]any) {
		if str(s, "id") == step && str(s, "statusId") != b.doing {
			t.Errorf("step status after reassignment = %v", s)
		}
	}

	// A map can use a non-default workflow, and steps then take its statuses.
	m2 := owner.must(201, "POST", acme+"/maps", map[string]any{"name": "Design", "workflowId": wfID})
	if str(m2.body, "workflowId") != wfID {
		t.Errorf("map workflow = %v", m2.body)
	}
	p2 := acme + "/maps/" + str(m2.body, "id")
	obj := str(owner.must(201, "POST", p2+"/nodes", map[string]any{"level": "objective", "title": "O"}).body, "id")
	act := str(owner.must(201, "POST", p2+"/nodes", map[string]any{"level": "activity", "title": "A", "parentId": obj}).body, "id")
	s2 := str(owner.must(201, "POST", p2+"/steps", map[string]any{"title": "s", "activityId": act}).body, "step", "id")
	owner.must(400, "PATCH", p2+"/steps/"+s2, map[string]any{"statusId": b.doneID})
}

func TestMapRoles(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	for _, c := range []struct {
		role           string
		edit, workflow bool
	}{
		{"viewer", false, false},
		{"commenter", false, false},
		{"editor", true, false},
		{"admin", true, true},
	} {
		t.Run(c.role, func(t *testing.T) {
			u, _ := h.join(owner, c.role+"@example.com", c.role)
			u.must(200, "GET", b.path, nil)
			u.must(200, "GET", acme+"/maps", nil)
			u.must(200, "GET", acme+"/workflows", nil)

			want := map[bool]int{true: 201, false: 403}
			u.must(want[c.edit], "POST", b.path+"/nodes", map[string]any{"level": "activity", "title": "x", "parentId": b.obj})
			u.must(want[c.edit], "POST", b.path+"/steps", map[string]any{"title": "x", "activityId": b.act1})
			u.must(want[c.edit], "POST", acme+"/maps", map[string]any{"name": "x"})
			u.must(want[c.workflow], "POST", acme+"/workflows", map[string]any{"name": "wf-" + c.role})
			u.must(map[bool]int{true: 200, false: 403}[c.workflow], "PATCH", acme+"/statuses/"+b.todo, map[string]any{"colour": "#000000"})
		})
	}
}

func TestMapTenancy(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	other := owner.must(201, "POST", "/api/workspaces/other/maps", map[string]any{"name": "Secret"})
	otherID := str(other.body, "id")

	// The caller belongs to both workspaces, but ids only resolve under their own.
	owner.must(404, "GET", acme+"/maps/"+otherID, nil)
	owner.must(404, "POST", acme+"/maps/"+otherID+"/nodes", map[string]any{"level": "activity", "title": "x"})
	owner.must(404, "PATCH", "/api/workspaces/other/maps/"+otherID+"/nodes/"+b.act1, map[string]any{"title": "x"})
	otherStatus := str(owner.must(200, "GET", "/api/workspaces/other/workflows", nil).list[0].(map[string]any)["statuses"].([]any)[0], "id")
	owner.must(404, "PATCH", acme+"/statuses/"+otherStatus, map[string]any{"name": "x"})
	owner.must(400, "POST", acme+"/maps", map[string]any{"name": "x", "workflowId": str(owner.must(200, "GET", "/api/workspaces/other/workflows", nil).list[0], "id")})

	// A former member sees nothing.
	outsider, _ := h.join(owner, "outsider@example.com", "viewer")
	owner.must(204, "DELETE", acme+"/members/"+str(outsider.must(200, "GET", "/api/me", nil).body, "user", "id"), nil)
	outsider.must(404, "GET", b.path, nil)
	outsider.must(404, "GET", acme+"/maps", nil)
}

func TestMapValidation(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	cases := []struct {
		method, path string
		body         any
	}{
		{"POST", acme + "/maps", map[string]any{"name": " "}},
		{"POST", b.path + "/nodes", map[string]any{"level": "activity", "title": "no objective"}},
		{"POST", b.path + "/nodes", map[string]any{"level": "chapter", "title": "x"}},
		{"POST", b.path + "/nodes", map[string]any{"level": "activity", "title": "x", "parentId": b.obj, "colour": "red"}},
		{"POST", b.path + "/nodes", map[string]any{"level": "activity", "title": "x", "parentId": b.obj, "afterId": b.act1, "beforeId": b.act2}},
		{"POST", b.path + "/nodes", map[string]any{"level": "step", "title": "x", "parentId": b.act1}},
		{"POST", b.path + "/nodes/" + b.act1 + "/move", map[string]any{"afterId": b.act2}},
		{"POST", b.path + "/releases", map[string]any{"name": "x", "targetDate": "31/12/2026"}},
		{"POST", b.path + "/steps", map[string]any{"title": "x"}},
		{"POST", b.path + "/steps", map[string]any{"title": "x", "activityId": b.obj}},
		{"POST", acme + "/workflows/" + b.act1 + "/statuses", map[string]any{"name": "x", "category": "later"}},
	}
	for _, c := range cases {
		if r := owner.do(c.method, c.path, c.body); r.status != 400 {
			t.Errorf("%s %s %v: status %d, want 400: %s", c.method, c.path, c.body, r.status, r.raw)
		}
	}

	// Malformed ids are not found, like ids that do not exist.
	owner.must(404, "POST", b.path+"/nodes/not-a-uuid/move", map[string]any{"parentId": nil})

	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "x", "activityId": b.act1}).body, "step", "id")
	for _, body := range []map[string]any{
		{"activityId": b.act1, "releaseId": nil},                                 // no version
		{"activityId": b.act1, "version": 0},                                     // no releaseId
		{"releaseId": nil, "version": 0},                                         // no activityId
		{"activityId": b.act1, "releaseId": nil, "version": 0, "afterId": b.obj}, // anchor is not a step in the cell
	} {
		if r := owner.do("POST", b.path+"/steps/"+step+"/move", body); r.status != 400 {
			t.Errorf("move %v: status %d: %s", body, r.status, r.raw)
		}
	}
	if r := owner.do("POST", b.path+"/steps/00000000-0000-7000-8000-000000000000/move",
		map[string]any{"activityId": b.act1, "releaseId": nil, "version": 0}); r.status != 404 {
		t.Errorf("move of unknown step: %d", r.status)
	}
}
