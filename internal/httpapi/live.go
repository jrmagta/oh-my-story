package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jrmagta/oh-my-story/internal/live"
	"github.com/jrmagta/oh-my-story/internal/store"
)

const (
	// A client further behind than this reloads the map instead of replaying.
	maxBacklog   = 1000
	pingEvery    = 25 * time.Second
	recheckEvery = time.Minute
	writeTimeout = 10 * time.Second
)

// liveMap streams a map's events and presence over a WebSocket. after is the
// lastEventId of the map the client holds; the stream starts right after it.
//
// The session cookie authenticates the upgrade, so the Origin must be our own
// address: otherwise any site could open a socket as the visitor. Membership
// is checked again every minute, so a removed member stops receiving events.
func (s *Server) liveMap(w http.ResponseWriter, r *http.Request, u store.User, m store.Membership) {
	mapID, err := pathUUID(r, "mapId")
	if err != nil {
		writeError(w, err)
		return
	}
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil || after < 0 {
		writeError(w, errBadRequest("after must be the lastEventId of the map you loaded"))
		return
	}
	if err := s.store.CheckMap(r.Context(), m.ID, mapID); err != nil {
		writeError(w, err)
		return
	}
	base, _ := url.Parse(s.cfg.BaseURL)
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{base.Host}})
	if err != nil {
		return // Accept has answered the request.
	}
	defer conn.CloseNow()
	// The client never sends; reading only notices it leave.
	ctx := conn.CloseRead(r.Context())

	sub := s.hub.Subscribe(ctx, m.ID, mapID, live.User{ID: u.ID, Name: u.DisplayName})
	defer s.hub.Unsubscribe(sub)

	write := func(msg live.Message) error {
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		return wsjson.Write(wctx, conn, msg)
	}
	resync := func() {
		_ = write(live.Message{Type: "resync"})
		conn.Close(websocket.StatusNormalClosure, "reload the map")
	}

	// Subscribed first, so events written while the backlog is read arrive on
	// the channel; those the backlog already covered are skipped below.
	backlog, err := s.store.MapEventsSince(ctx, m.ID, mapID, after, maxBacklog+1)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "")
		return
	}
	if len(backlog) > maxBacklog {
		resync()
		return
	}
	sent := after
	for i := range backlog {
		if write(live.Message{Type: "event", Event: &backlog[i]}) != nil {
			return
		}
		sent = backlog[i].ID
	}

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	recheck := time.NewTicker(recheckEvery)
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.C():
			if !ok {
				resync() // fell too far behind
				return
			}
			if msg.Event != nil {
				if msg.Event.ID <= sent {
					continue
				}
				sent = msg.Event.ID
			}
			if write(msg) != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-recheck.C:
			if _, err := s.store.Membership(ctx, u.ID, r.PathValue("slug")); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					conn.Close(websocket.StatusPolicyViolation, "no longer a member")
				}
				return
			}
		}
	}
}
