package httpapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestAPITokens(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	editor, _ := h.join(owner, "ed@example.com", "editor")
	tokens := acme + "/api-tokens"

	// Admins make tokens; the token comes back once, with a visible prefix.
	editor.must(403, "POST", tokens, map[string]any{"name": "CI bot", "scope": "write"})
	owner.must(400, "POST", tokens, map[string]any{"name": "CI bot", "scope": "admin"})
	owner.must(400, "POST", tokens, map[string]any{"name": "CI bot", "scope": "read", "expiresInDays": 7})
	owner.must(400, "POST", tokens, map[string]any{"name": "", "scope": "read"})
	w := owner.must(201, "POST", tokens, map[string]any{"name": "CI bot", "scope": "write", "expiresInDays": 90}).body
	write := str(w, "token")
	if !strings.HasPrefix(write, "oms_") || !strings.HasPrefix(write, str(w, "prefix")) || w["expiresAt"] == nil || str(w, "creatorName") == "" {
		t.Errorf("created = %v", w)
	}
	read := str(owner.must(201, "POST", tokens, map[string]any{"name": "Dashboard", "scope": "read"}).body, "token")
	list := owner.must(200, "GET", tokens, nil)
	if len(list.list) != 2 || strings.Contains(list.raw, write) || strings.Contains(list.raw, read) {
		t.Errorf("list = %s", list.raw)
	}

	// A write token works like an editor in its workspace, with no cookie, from any origin.
	bot := h.tokenClient(write)
	made := bot.must(201, "POST", b.path+"/steps", map[string]any{"title": "From CI", "activityId": b.act1}).body
	step := str(made, "step", "id")
	bot.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"statusId": b.doing})
	bot.must(200, "GET", b.path, nil)
	// History names the token, and the event log says a token did it.
	hist := owner.must(200, "GET", b.path+"/history", nil).body["entries"].([]any)
	if str(hist[0], "actorName") != "CI bot (token)" {
		t.Errorf("history actor = %v", hist[0])
	}
	conn, err := pgx.Connect(context.Background(), h.database)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var kinds []string
	if err := conn.QueryRow(context.Background(), `SELECT array_agg(DISTINCT actor_kind) FROM events WHERE entity_id = $1`, step).Scan(&kinds); err != nil || len(kinds) != 1 || kinds[0] != "token" {
		t.Errorf("actor kinds = %v %v", kinds, err)
	}
	// It cannot do admin things, see account endpoints, or reach other workspaces.
	bot.must(403, "POST", tokens, map[string]any{"name": "More", "scope": "write"})
	bot.must(403, "PATCH", acme, map[string]any{"name": "Renamed"})
	bot.must(403, "GET", "/api/me", nil)
	bot.must(403, "GET", "/api/workspaces", nil)
	owner.must(201, "POST", "/api/workspaces", map[string]string{"name": "Other", "slug": "other"})
	bot.must(404, "GET", "/api/workspaces/other", nil)
	// The bot is not a member: not listed, not assignable.
	if r := owner.must(200, "GET", acme+"/members", nil); strings.Contains(r.raw, "CI bot") {
		t.Errorf("bot listed as member: %s", r.raw)
	}

	// A read token reads and cannot write.
	reader := h.tokenClient(read)
	reader.must(200, "GET", b.path, nil)
	reader.must(403, "PATCH", b.path+"/steps/"+step, map[string]any{"title": "Nope"})

	// Last use is recorded; only the hash is stored.
	if r := owner.must(200, "GET", tokens, nil); r.list[1].(map[string]any)["lastUsedAt"] == nil {
		t.Errorf("last use not recorded: %s", r.raw)
	}
	var plain int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM api_tokens WHERE token_hash = convert_to($1, 'UTF8') OR prefix = $1`, write).Scan(&plain); err != nil || plain != 0 {
		t.Errorf("token stored in plain: %d %v", plain, err)
	}

	// Bad, revoked and expired tokens are refused; a bad token never falls back to the cookie.
	h.tokenClient("oms_nonsense").must(401, "GET", b.path, nil)
	owner.must(204, "DELETE", tokens+"/"+str(w, "id"), nil)
	owner.must(404, "DELETE", tokens+"/"+str(w, "id"), nil)
	bot.must(401, "GET", b.path, nil)
	if _, err := conn.Exec(context.Background(), `UPDATE api_tokens SET expires_at = now() - interval '1 minute' WHERE prefix = $1`, read[:10]); err != nil {
		t.Fatal(err)
	}
	reader.must(401, "GET", b.path, nil)
	withCookie := h.clientWithSession(owner.sessionToken())
	withCookie.headers = map[string]string{"Authorization": "Bearer oms_nonsense"}
	withCookie.must(401, "GET", b.path, nil)
	if r := owner.must(200, "GET", tokens, nil); len(r.list) != 1 {
		t.Errorf("revoked token still listed: %s", r.raw)
	}

	// Cookie requests from another origin are still refused.
	cross := h.clientWithSession(owner.sessionToken())
	cross.headers = map[string]string{"Origin": "https://evil.example.org"}
	cross.must(403, "POST", b.path+"/steps", map[string]any{"title": "Forged", "activityId": b.act1})
}
