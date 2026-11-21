package httpapi_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jrmagta/oh-my-story/internal/config"
)

// TestKeycloakDirectory needs the compose Keycloak (make up), whose realm gives
// the app's client a service account that can search users.
func TestKeycloakDirectory(t *testing.T) {
	issuer := os.Getenv("TEST_OIDC_ISSUER")
	if issuer == "" {
		t.Skip("TEST_OIDC_ISSUER not set; run make up && make test")
	}
	h := newHarness(t, harnessOpts{
		listen: "127.0.0.1:18080",
		base:   "http://localhost:18080",
		oidc: &config.OIDC{Issuer: issuer, DiscoveryURL: issuer, Name: "Keycloak",
			ClientID: "oh-my-story", ClientSecret: "dev-only-client-secret", Directory: "keycloak"},
	})
	owner := h.bootstrap()
	editor, _ := h.join(owner, "ed@example.com", "editor")
	dir := acme + "/directory"
	members := acme + "/members"

	if r := h.client().must(200, "GET", "/api/auth/providers", nil); r.body["oidc"].(map[string]any)["directory"] != true {
		t.Errorf("providers: %s", r.raw)
	}

	// Admins search the directory by name or email; disabled people are left out.
	editor.must(403, "GET", dir+"?q=carol", nil)
	owner.must(400, "GET", dir+"?q=", nil)
	found := owner.must(200, "GET", dir+"?q=car", nil).list
	if len(found) != 1 || str(found[0], "email") != "carol@example.com" || str(found[0], "name") != "Carol Directory" ||
		str(found[0], "firstName") != "Carol" || found[0].(map[string]any)["member"] != false {
		t.Fatalf("search = %v", found)
	}
	if r := owner.must(200, "GET", dir+"?q=dave", nil); len(r.list) != 0 {
		t.Errorf("disabled user listed: %s", r.raw)
	}
	carol := str(found[0], "id")

	// Adding makes her a member at once, named from the directory, with no invite.
	editor.must(403, "POST", members, map[string]any{"directoryId": carol, "role": "editor"})
	owner.must(400, "POST", members, map[string]any{"directoryId": carol, "role": "owner"})
	owner.must(400, "POST", members, map[string]any{"directoryId": "00000000-0000-0000-0000-000000000000", "role": "editor"})
	added := owner.must(201, "POST", members, map[string]any{"directoryId": carol, "role": "editor"}).body
	if str(added, "displayName") != "Carol Directory" || str(added, "email") != "carol@example.com" || str(added, "role") != "editor" ||
		str(added, "firstName") != "Carol" || str(added, "lastName") != "Directory" {
		t.Errorf("added = %v", added)
	}
	// The member list has her first and last name; a password account has neither.
	for _, m := range owner.must(200, "GET", members, nil).list {
		switch str(m, "email") {
		case "carol@example.com":
			if str(m, "firstName") != "Carol" || str(m, "lastName") != "Directory" {
				t.Errorf("carol = %v", m)
			}
		default:
			if mm := m.(map[string]any); mm["firstName"] != nil || mm["lastName"] != nil {
				t.Errorf("password account has names: %v", m)
			}
		}
	}
	owner.must(409, "POST", members, map[string]any{"directoryId": carol, "role": "viewer"})
	if r := owner.must(200, "GET", dir+"?q=carol", nil); r.list[0].(map[string]any)["member"] != true {
		t.Errorf("not shown as a member: %s", r.raw)
	}
	if r := owner.must(200, "GET", acme+"/invites", nil); len(r.list) != 0 {
		t.Errorf("an invite was made: %s", r.raw)
	}
	// She can be assigned before ever signing in.
	b := buildMap(owner)
	step := str(owner.must(201, "POST", b.path+"/steps", map[string]any{"title": "Review", "activityId": b.act1}).body, "step", "id")
	owner.must(200, "PATCH", b.path+"/steps/"+step, map[string]any{"assigneeId": str(added, "userId")})

	// She signs in with Keycloak and lands in the workspace as that same account.
	c := h.client()
	if got := c.ssoLogin(t, "carol", "carol", "/"); got != "/" {
		t.Fatalf("carol's sign-in went to %q", got)
	}
	me := c.must(200, "GET", "/api/me", nil).body
	if str(me, "user", "id") != str(added, "userId") || str(me, "user", "displayName") != "Carol Directory" {
		t.Errorf("me = %v", me)
	}
	if ws := me["workspaces"].([]any); len(ws) != 1 || str(ws[0], "slug") != "acme" || str(ws[0], "role") != "editor" {
		t.Errorf("workspaces = %v", ws)
	}

	// Someone who signed up before is linked, not duplicated: alice, invited and signed in, is found as a member.
	owner.must(201, "POST", acme+"/invites", map[string]any{"email": "alice@example.com", "role": "viewer"})
	a := h.client()
	a.ssoLogin(t, "alice", "alice", "/")
	accept := h.mail.inviteToken(t, "alice@example.com")
	a.must(200, "POST", "/api/invites/"+accept+"/accept", map[string]any{})
	if r := owner.must(200, "GET", dir+"?q=alice", nil); len(r.list) != 1 || r.list[0].(map[string]any)["member"] != true {
		t.Errorf("alice = %s", r.raw)
	}
	// Names come from Keycloak at sign-in.
	if r := owner.must(200, "GET", members, nil); !strings.Contains(r.raw, "Alice Example") {
		t.Errorf("members = %s", r.raw)
	}
}
