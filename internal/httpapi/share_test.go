package httpapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestShareLinks(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, editorID := h.join(owner, "ed@example.com", "editor")
	commenter, _ := h.join(owner, "co@example.com", "commenter")
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Email login", "descriptionMd": "Magic links", "activityId": b.act1}).body, "step", "id")
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"assigneeId": editorID})
	owner.must(201, "POST", b.path+"/steps/"+step+"/comments", map[string]any{"bodyMd": "Secret discussion"})
	links := b.path + "/share-links"

	// Editors make links; the URL (with the token) comes back once.
	commenter.must(403, "POST", links, map[string]any{})
	made := editor.must(201, "POST", links, map[string]any{"expiresInDays": 30})
	url := str(made.body, "url")
	if !strings.Contains(url, "/s/") || made.body["expiresAt"] == nil || str(made.body, "creatorName") != "ed@example.com" {
		t.Errorf("created = %v", made.body)
	}
	token := url[strings.LastIndex(url, "/s/")+3:]
	editor.must(400, "POST", links, map[string]any{"expiresInDays": 5})
	editor.must(201, "POST", links, map[string]any{})
	if r := editor.must(200, "GET", links, nil); len(r.list) != 2 || strings.Contains(r.raw, token) {
		t.Errorf("list = %s", r.raw)
	}
	commenter.must(403, "GET", links, nil)

	// Anyone with the link reads the map, without assignees or comments.
	anon := h.client()
	shared := anon.must(200, "GET", "/api/shared/"+token, nil)
	view := shared.body["view"].(map[string]any)
	st := view["steps"].([]any)[0].(map[string]any)
	if str(shared.body, "workspaceName") != "Acme" || st["assigneeId"] != nil || st["commentCount"] != 0.0 || str(st, "descriptionMd") != "Magic links" {
		t.Errorf("shared = %v", shared.body)
	}
	if strings.Contains(shared.raw, "Secret discussion") || strings.Contains(shared.raw, "ed@example.com") {
		t.Errorf("shared payload leaks people or comments: %s", shared.raw)
	}
	if r := editor.must(200, "GET", links, nil); r.list[1].(map[string]any)["lastUsedAt"] == nil && r.list[0].(map[string]any)["lastUsedAt"] == nil {
		t.Errorf("last used not recorded: %s", r.raw)
	}
	anon.must(404, "GET", "/api/shared/not-a-token", nil)

	// Only the hash is stored.
	conn, err := pgx.Connect(context.Background(), h.database)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var plain int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM share_links WHERE token_hash = convert_to($1, 'UTF8')`, token).Scan(&plain); err != nil || plain != 0 {
		t.Errorf("token stored in plain: %d %v", plain, err)
	}

	// Revoked and expired links stop working.
	editor.must(204, "DELETE", links+"/"+str(made.body, "id"), nil)
	anon.must(404, "GET", "/api/shared/"+token, nil)
	editor.must(404, "DELETE", links+"/"+str(made.body, "id"), nil)
	late := editor.must(201, "POST", links, map[string]any{"expiresInDays": 7})
	lateToken := str(late.body, "url")[strings.LastIndex(str(late.body, "url"), "/s/")+3:]
	anon.must(200, "GET", "/api/shared/"+lateToken, nil)
	if _, err := conn.Exec(context.Background(), `UPDATE share_links SET expires_at = now() - interval '1 minute' WHERE id = $1`, str(late.body, "id")); err != nil {
		t.Fatal(err)
	}
	anon.must(404, "GET", "/api/shared/"+lateToken, nil)

	// A deleted map's links stop working; another workspace's map is not found.
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	o := buildMapIn(owner, "/api/workspaces/other")
	owner.must(404, "GET", acme+"/maps/"+strings.TrimPrefix(o.path, "/api/workspaces/other/maps/")+"/share-links", nil)
	keep := editor.must(201, "POST", links, map[string]any{})
	keepToken := str(keep.body, "url")[strings.LastIndex(str(keep.body, "url"), "/s/")+3:]
	owner.must(204, "DELETE", b.path, nil)
	anon.must(404, "GET", "/api/shared/"+keepToken, nil)
}
