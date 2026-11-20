package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type liveMsg struct {
	Type  string `json:"type"`
	Event *struct {
		ID       int64          `json:"id"`
		Entity   string         `json:"entity"`
		EntityID string         `json:"entityId"`
		Action   string         `json:"action"`
		After    map[string]any `json:"after"`
	} `json:"event"`
	Users []struct {
		Name string `json:"name"`
	} `json:"users"`
}

// dial opens the map's live socket as this browser. origin "" sends none.
func (c *client) dial(mapPath string, after int64, origin string) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	url := strings.Replace(c.h.base, "http", "ws", 1) + fmt.Sprintf("%s/live?after=%d", mapPath, after)
	return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: h})
}

func (c *client) mustDial(mapPath string, after int64) *websocket.Conn {
	c.h.t.Helper()
	conn, _, err := c.dial(mapPath, after, c.h.base)
	if err != nil {
		c.h.t.Fatal(err)
	}
	c.h.t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// until reads messages until one satisfies ok, failing after a few seconds.
func until(t *testing.T, conn *websocket.Conn, what string, ok func(liveMsg) bool) liveMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var m liveMsg
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if ok(m) {
			return m
		}
	}
}

// events reads n event messages, skipping presence.
func events(t *testing.T, conn *websocket.Conn, n int) []liveMsg {
	t.Helper()
	var out []liveMsg
	for len(out) < n {
		out = append(out, until(t, conn, fmt.Sprintf("event %d of %d", len(out)+1, n), func(m liveMsg) bool { return m.Type == "event" }))
	}
	return out
}

func lastEventID(c *client, mapPath string) int64 {
	return int64(c.must(200, "GET", mapPath, nil).body["lastEventId"].(float64))
}

// A write through one app instance reaches a browser connected to another.
func TestLiveAcrossInstances(t *testing.T) {
	h1 := newHarness(t, harnessOpts{})
	h2 := newHarness(t, harnessOpts{database: h1.database})
	owner := h1.bootstrap()
	b := buildMap(owner)
	onTwo := h2.clientWithSession(owner.sessionToken())

	conn := onTwo.mustDial(b.path, lastEventID(onTwo, b.path))
	created := owner.must(201, "POST", b.path+"/stories", map[string]any{"title": "Live story", "activityId": b.act1})

	evs := events(t, conn, 2)
	if evs[0].Event.Entity != "story" || evs[0].Event.Action != "create" || evs[0].Event.After["title"] != "Live story" ||
		evs[1].Event.Entity != "placement" || evs[1].Event.EntityID != str(created.body, "placement", "id") {
		t.Errorf("events = %+v, %+v", evs[0].Event, evs[1].Event)
	}

	// Workspace-wide changes, such as a renamed status, reach the map's watchers too.
	owner.must(200, "PATCH", acme+"/statuses/"+b.todo, map[string]any{"name": "Backlog"})
	if ev := events(t, conn, 1)[0].Event; ev.Entity != "status" || ev.After["name"] != "Backlog" {
		t.Errorf("status event = %+v", ev)
	}
}

// A reconnect with the last id it saw replays what it missed, once each, in order.
func TestLiveResume(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)
	from := lastEventID(owner, b.path)
	for _, title := range []string{"one", "two"} {
		owner.must(201, "POST", b.path+"/stories", map[string]any{"title": title, "activityId": b.act1})
	}
	other := owner.must(201, "POST", acme+"/maps", map[string]any{"name": "Other"})
	owner.must(201, "POST", acme+"/maps/"+str(other.body, "id")+"/nodes", map[string]any{"level": "objective", "title": "x"})

	conn := owner.mustDial(b.path, from)
	evs := events(t, conn, 4)
	var got []string
	var ids []int64
	for _, e := range evs {
		got = append(got, e.Event.Entity+":"+fmt.Sprint(e.Event.After["title"]))
		ids = append(ids, e.Event.ID)
	}
	if !slices.Equal(got, []string{"story:one", "placement:<nil>", "story:two", "placement:<nil>"}) || !slices.IsSorted(ids) {
		t.Errorf("replayed %v (ids %v)", got, ids)
	}

	// The next live event follows straight on; the other map's events never came.
	owner.must(201, "POST", b.path+"/stories", map[string]any{"title": "three", "activityId": b.act1})
	if ev := events(t, conn, 1)[0].Event; ev.After["title"] != "three" || ev.ID <= ids[3] {
		t.Errorf("live after replay = %+v", ev)
	}
}

// People watching the same map see each other, whichever instance they are on.
func TestLivePresence(t *testing.T) {
	h1 := newHarness(t, harnessOpts{})
	h2 := newHarness(t, harnessOpts{database: h1.database})
	owner := h1.bootstrap()
	b := buildMap(owner)
	_, _ = h1.join(owner, "bob@example.com", "viewer")
	bob := h2.client()
	bob.must(200, "POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "member-password-123"})

	both := func(m liveMsg) bool {
		var names []string
		for _, u := range m.Users {
			names = append(names, u.Name)
		}
		return m.Type == "presence" && slices.Equal(names, []string{"Olive Owner", "bob@example.com"})
	}
	ownerConn := owner.mustDial(b.path, lastEventID(owner, b.path))
	until(t, ownerConn, "the owner alone", func(m liveMsg) bool { return m.Type == "presence" && len(m.Users) == 1 })
	bobConn := bob.mustDial(b.path, lastEventID(bob, b.path))
	until(t, bobConn, "both, seen by Bob", both)
	until(t, ownerConn, "both, seen by the owner", both)

	bobConn.Close(websocket.StatusNormalClosure, "")
	until(t, ownerConn, "Bob leaving", func(m liveMsg) bool { return m.Type == "presence" && len(m.Users) == 1 })
}

func TestLiveRefusals(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	owner := h.bootstrap()
	b := buildMap(owner)

	// Another site cannot open a socket with the visitor's cookie.
	if _, res, err := owner.dial(b.path, 0, "http://evil.example"); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin dial: err=%v res=%v", err, res)
	}
	for _, c := range []struct {
		who    *client
		path   string
		after  string
		status int
	}{
		{h.client(), b.path, "0", 401},
		{owner, b.path, "x", 400},
		{owner, b.path, "-1", 400},
		{owner, acme + "/maps/00000000-0000-7000-8000-000000000000", "0", 404},
	} {
		if r := c.who.do("GET", c.path+"/live?after="+c.after, nil); r.status != c.status {
			t.Errorf("GET %s/live?after=%s: %d, want %d", c.path, c.after, r.status, c.status)
		}
	}
}
