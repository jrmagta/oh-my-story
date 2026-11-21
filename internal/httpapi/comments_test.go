package httpapi_test

import (
	"strings"
	"testing"
	"time"
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
