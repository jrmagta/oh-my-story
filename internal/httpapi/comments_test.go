package httpapi_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mentionMails waits briefly for mention emails (sent after the reply) and
// returns those sent to addr.
func (r *recorder) mentionMails(addr string) []string {
	var out []string
	for range 50 {
		r.mu.Lock()
		out = out[:0]
		for _, m := range r.msgs {
			if strings.EqualFold(m.To, addr) && strings.Contains(m.Subject, "mentioned you") {
				out = append(out, m.Subject+"\n"+m.Body)
			}
		}
		r.mu.Unlock()
		if len(out) > 0 {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	return out
}

func TestComments(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, editorID := h.join(owner, "ed@example.com", "editor")
	commenter, commenterID := h.join(owner, "co@example.com", "commenter")
	viewer, _ := h.join(owner, "vi@example.com", "viewer")
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Email login", "activityId": b.act1}).body, "step", "id")
	comments := b.path + "/steps/" + step + "/comments"

	// Commenters comment and mention people; viewers cannot comment.
	first := commenter.must(201, "POST", comments, map[string]any{"bodyMd": "What about @ed@example.com?", "mentionIds": []string{editorID}})
	if str(first.body, "authorName") != "co@example.com" || first.body["resolved"] != false || len(first.body["mentionIds"].([]any)) != 1 {
		t.Errorf("comment = %v", first.body)
	}
	viewer.must(403, "POST", comments, map[string]any{"bodyMd": "x"})
	commenter.must(403, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "no"})

	// The mention emails the editor once, with a link to the step.
	got := h.mail.mentionMails("ed@example.com")
	if len(got) != 1 || !strings.Contains(got[0], "co@example.com mentioned you on ACM-1") || !strings.Contains(got[0], "/maps/") || !strings.Contains(got[0], "?step="+step) {
		t.Errorf("mention emails = %q", got)
	}

	// Replies go under a first comment of the same step; not under a reply.
	id := str(first.body, "id")
	reply := editor.must(201, "POST", comments, map[string]any{"bodyMd": "Done, @co", "parentId": id, "mentionIds": []string{commenterID, editorID}})
	editor.must(400, "POST", comments, map[string]any{"bodyMd": "x", "parentId": str(reply.body, "id")})
	editor.must(400, "POST", comments, map[string]any{"bodyMd": "x", "mentionIds": []string{b.act1}})
	editor.must(400, "POST", comments, map[string]any{"bodyMd": " "})
	if got := h.mail.mentionMails("co@example.com"); len(got) != 1 {
		t.Errorf("commenter mention emails = %d", len(got))
	}
	time.Sleep(100 * time.Millisecond)
	if got := h.mail.mentionMails("ed@example.com"); len(got) != 1 {
		t.Errorf("the author is not emailed about their own mention: %d", len(got))
	}

	// Only the author edits; editing again with the same mention does not email again.
	path := b.path + "/comments/" + id
	editor.must(403, "PATCH", path, map[string]any{"bodyMd": "hijack"})
	if r := commenter.must(200, "PATCH", path, map[string]any{"bodyMd": "What about magic links, @ed@example.com?", "mentionIds": []string{editorID}}); r.body["edited"] != true {
		t.Errorf("edited = %v", r.body)
	}
	time.Sleep(100 * time.Millisecond)
	if got := h.mail.mentionMails("ed@example.com"); len(got) != 1 {
		t.Errorf("re-mention emailed again: %d", len(got))
	}

	// Editors and the thread's author resolve; a commenter who did not start it cannot; replies are not resolved.
	other := str(owner.must(201, "POST", comments, map[string]any{"bodyMd": "Owner's thread"}).body, "id")
	commenter.must(403, "PATCH", b.path+"/comments/"+other, map[string]any{"resolved": true})
	if r := editor.must(200, "PATCH", b.path+"/comments/"+other, map[string]any{"resolved": true}); r.body["resolved"] != true {
		t.Errorf("resolved = %v", r.body)
	}
	commenter.must(200, "PATCH", path, map[string]any{"resolved": true})
	editor.must(400, "PATCH", b.path+"/comments/"+str(reply.body, "id"), map[string]any{"resolved": true})

	// The step counts its comments; deleting the first comment keeps it, blanked, for its reply.
	if n := owner.must(200, "GET", b.path, nil).body["steps"].([]any)[0].(map[string]any)["commentCount"]; n != 3.0 {
		t.Errorf("commentCount = %v", n)
	}
	editor.must(403, "DELETE", path, nil)
	commenter.must(204, "DELETE", path, nil)
	list := viewer.must(200, "GET", comments, nil).list
	if len(list) != 3 || list[0].(map[string]any)["deleted"] != true || str(list[0], "bodyMd") != "" {
		t.Errorf("after delete = %v", list)
	}
	// Admins delete anyone's; a deleted comment with no replies is left out.
	owner.must(204, "DELETE", b.path+"/comments/"+str(reply.body, "id"), nil)
	if list := viewer.must(200, "GET", comments, nil).list; len(list) != 1 || str(list[0], "id") != other {
		t.Errorf("after deleting the reply = %v", list)
	}

	// Comments are logged on the map, and other workspaces' steps are not found.
	es, _ := entries(owner.must(200, "GET", b.path+"/history?limit=20", nil))
	var kinds []string
	for _, e := range es {
		if e.entity == "comment" {
			kinds = append(kinds, e.action)
		}
	}
	if len(kinds) < 5 {
		t.Errorf("comment events = %v", kinds)
	}
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	o := buildMapIn(owner, "/api/workspaces/other")
	owner.must(404, "GET", acme+"/maps/"+strings.TrimPrefix(o.path, "/api/workspaces/other/maps/")+"/steps/"+step+"/comments", nil)
	owner.must(404, "GET", o.path+"/steps/"+step+"/comments", nil)
}

func TestNodeComments(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, editorID := h.join(owner, "ed@example.com", "editor")
	commenter, _ := h.join(owner, "co@example.com", "commenter")
	viewer, _ := h.join(owner, "vi@example.com", "viewer")
	act := b.path + "/nodes/" + b.act1
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Email login", "activityId": b.act1}).body, "step", "id")
	stepComment := str(owner.must(201, "POST", b.path+"/steps/"+step+"/comments", map[string]any{"bodyMd": "on the step"}).body, "id")
	owner.must(200, "PATCH", act, map[string]any{"title": "Enter your email"})

	// Commenters comment on an activity, mentioning someone; viewers cannot.
	draft := commenter.uploadTo(201, act+"/draft-attachments", "flow.png", pngBytes)
	first := commenter.must(201, "POST", act+"/comments", map[string]any{
		"bodyMd": "@ed@example.com is this right?", "mentionIds": []string{editorID}, "attachmentIds": []string{str(draft, "id")},
	})
	if str(first.body, "nodeId") != b.act1 || first.body["stepId"] != nil || len(first.body["attachments"].([]any)) != 1 {
		t.Errorf("node comment = %s", first.raw)
	}
	viewer.must(403, "POST", act+"/comments", map[string]any{"bodyMd": "x"})
	got := h.mail.mentionMails("ed@example.com")
	if len(got) != 1 || !strings.Contains(got[0], "mentioned you on “Enter your email”") ||
		!strings.Contains(got[0], "on activity “Enter your email”") || !strings.Contains(got[0], "?node="+b.act1) {
		t.Errorf("mention emails = %q", got)
	}

	// Replies stay on their card: a step's comment is not a parent here, nor a step's draft a file.
	editor.must(201, "POST", act+"/comments", map[string]any{"bodyMd": "yes", "parentId": str(first.body, "id")})
	editor.must(400, "POST", act+"/comments", map[string]any{"bodyMd": "no", "parentId": stepComment})
	stepDraft := commenter.uploadTo(201, b.path+"/steps/"+step+"/draft-attachments", "x.txt", []byte("x"))
	commenter.must(400, "POST", act+"/comments", map[string]any{"bodyMd": "no", "attachmentIds": []string{str(stepDraft, "id")}})

	// The card counts its own comments, not its steps'; the step's list is its own.
	if r := viewer.must(200, "GET", act+"/comments", nil); len(r.list) != 2 {
		t.Errorf("node comments = %s", r.raw)
	}
	if r := viewer.must(200, "GET", b.path+"/steps/"+step+"/comments", nil); len(r.list) != 1 {
		t.Errorf("step comments = %s", r.raw)
	}
	view := owner.must(200, "GET", b.path, nil).body
	for _, n := range view["nodes"].([]any) {
		if str(n, "id") == b.act1 && n.(map[string]any)["commentCount"] != 2.0 {
			t.Errorf("activity commentCount = %v", n.(map[string]any)["commentCount"])
		}
	}
	for _, st := range view["steps"].([]any) {
		if st.(map[string]any)["commentCount"] != 1.0 {
			t.Errorf("step commentCount = %v", st.(map[string]any)["commentCount"])
		}
	}

	// The card's history is its own: the rename, the comments and nothing of its step.
	es, _ := entries(viewer.must(200, "GET", act+"/history", nil))
	var kinds []string
	for _, e := range es {
		kinds = append(kinds, e.entity+":"+e.action)
	}
	if strings.Join(kinds, " ") != "comment:create comment:create node:update node:create" {
		t.Errorf("node history = %v", kinds)
	}
	owner.must(404, "GET", b.path+"/nodes/"+uuid.NewString()+"/history", nil)

	// Share links show no comments, so no count either.
	link := str(owner.must(201, "POST", b.path+"/share-links", map[string]any{}).body, "url")
	shared := h.client().must(200, "GET", "/api/shared/"+link[strings.LastIndex(link, "/s/")+3:], nil)
	if strings.Contains(shared.raw, "is this right") || strings.Contains(shared.raw, `"commentCount":2`) {
		t.Errorf("shared map shows node comments: %s", shared.raw)
	}

	// Deleting the activity hides its comments and their files.
	file := acme + "/attachments/" + str(draft, "id")
	owner.must(204, "DELETE", act, nil)
	viewer.must(404, "GET", act+"/comments", nil)
	if r := viewer.send("GET", file, "", nil, nil); r.status != 404 {
		t.Errorf("a deleted card's comment file: %d", r.status)
	}
	owner.must(200, "POST", act+"/restore", map[string]any{})
	if r := viewer.send("GET", file, "", nil, nil); r.status != 200 {
		t.Errorf("a restored card's comment file: %d", r.status)
	}
}
