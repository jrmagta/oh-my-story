// Package live fans the event log out to the browsers watching each map, and
// tracks who is watching. Every app instance runs one Hub. Hubs share nothing
// but Postgres: events arrive through LISTEN on "events" (the log is the
// source of truth, the notification only says "read on"), and presence
// through LISTEN on "presence".
package live

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jrmagta/oh-my-story/internal/store"
)

const (
	// A subscriber this far behind is dropped and told to reload.
	bufferSize = 256
	batchSize  = 500

	heartbeat = 30 * time.Second
	expiry    = 75 * time.Second
)

// User is a person shown as present on a map.
type User struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Message is what a subscriber receives.
type Message struct {
	Type  string       `json:"type"` // "event", "presence" or "resync"
	Event *store.Event `json:"event,omitempty"`
	Users []User       `json:"users,omitempty"`
}

// Sub is one browser watching one map.
type Sub struct {
	workspace uuid.UUID
	mapID     uuid.UUID
	user      User
	key       string // unique per connection, across instances
	ch        chan Message
	dropped   bool
}

// C delivers messages. It is closed when the hub drops a subscriber that fell
// too far behind; the client must then reload the map.
func (s *Sub) C() <-chan Message { return s.ch }

type Hub struct {
	pool     *pgxpool.Pool
	store    *store.Store
	instance string

	mu       sync.Mutex
	subs     map[uuid.UUID]map[*Sub]struct{} // by map
	last     int64                           // newest event id handed out
	started  bool                            // last has been read from the log
	presence map[uuid.UUID]map[string]seen   // by map, then connection key
}

type seen struct {
	user    User
	expires time.Time
}

func New(pool *pgxpool.Pool) *Hub {
	return &Hub{
		pool:     pool,
		store:    store.New(pool),
		instance: uuid.NewString(),
		subs:     map[uuid.UUID]map[*Sub]struct{}{},
		presence: map[uuid.UUID]map[string]seen{},
	}
}

// Start begins following the event log and keeps at it until ctx ends,
// reconnecting after errors. When the database is reachable and migrated,
// Start returns with the listener up, so events written afterwards are
// delivered. When it is not (on a first install the schema can arrive after
// the app starts), Start logs, returns, and keeps trying in the background:
// the server must come up either way.
func (h *Hub) Start(ctx context.Context) {
	go h.beat(ctx)
	conn, err := h.connect(ctx)
	if err != nil {
		slog.Warn("live: not following events yet, retrying", "err", err)
	}
	go func() {
		for {
			if conn != nil {
				err := h.listen(ctx, conn)
				conn.Close(context.Background())
				if ctx.Err() != nil {
					return
				}
				slog.Error("live: listener stopped, reconnecting", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			if conn, err = h.connect(ctx); err != nil {
				slog.Warn("live: reconnect", "err", err)
				conn = nil
			}
		}
	}()
}

// connect opens a connection that LISTENs for good, so it leaves the pool.
// The first time it succeeds it also sets the cursor to the log's end; later
// reconnects keep the cursor and catch up from it.
func (h *Hub) connect(ctx context.Context) (*pgx.Conn, error) {
	h.mu.Lock()
	started := h.started
	h.mu.Unlock()
	if !started {
		last, err := h.store.LastEventID(ctx)
		var pg *pgconn.PgError
		switch {
		case errors.As(err, &pg) && pg.Code == "42P01": // undefined_table
			// Not migrated yet, so no events exist: follow the log from its
			// start, or events written as the migration lands would be skipped.
			h.mu.Lock()
			h.last, h.started = 0, true
			h.mu.Unlock()
			return nil, err
		case err != nil:
			return nil, err
		}
		h.mu.Lock()
		h.last, h.started = last, true
		h.mu.Unlock()
	}
	pc, err := h.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	c := pc.Hijack()
	for _, ch := range []string{"events", "presence"} {
		if _, err := c.Exec(ctx, "LISTEN "+ch); err != nil {
			c.Close(context.Background())
			return nil, err
		}
	}
	return c, nil
}

func (h *Hub) listen(ctx context.Context, c *pgx.Conn) error {
	// Anything written while no listener was up is still in the log.
	if err := h.catchUp(ctx); err != nil {
		return err
	}
	for {
		n, err := c.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		switch n.Channel {
		case "events":
			if err := h.catchUp(ctx); err != nil {
				return err
			}
		case "presence":
			h.onPresence(ctx, n.Payload)
		}
	}
}

// catchUp hands out every event after the last one handed out.
func (h *Hub) catchUp(ctx context.Context) error {
	for {
		h.mu.Lock()
		after := h.last
		h.mu.Unlock()
		evs, err := h.store.EventsSince(ctx, after, batchSize)
		if err != nil {
			return err
		}
		h.mu.Lock()
		for i := range evs {
			h.dispatch(&evs[i])
			h.last = evs[i].ID
		}
		h.mu.Unlock()
		if len(evs) < batchSize {
			return nil
		}
	}
}

// dispatch sends an event to the map's subscribers; workspace-wide events
// (workflows, statuses) go to every subscriber in the workspace. Holds h.mu.
func (h *Hub) dispatch(ev *store.Event) {
	for mapID, subs := range h.subs {
		if ev.MapID != nil && *ev.MapID != mapID {
			continue
		}
		for s := range subs {
			if s.workspace == ev.WorkspaceID {
				h.send(s, Message{Type: "event", Event: ev})
			}
		}
	}
}

// send never blocks: a subscriber that cannot keep up is dropped. Holds h.mu.
func (h *Hub) send(s *Sub, m Message) {
	if s.dropped {
		return
	}
	select {
	case s.ch <- m:
	default:
		s.dropped = true
		close(s.ch)
	}
}

// Subscribe starts delivering the map's events and presence. Subscribe before
// reading the backlog, so nothing written in between is missed; the caller
// skips events it already sent.
func (h *Hub) Subscribe(ctx context.Context, workspace, mapID uuid.UUID, user User) *Sub {
	s := &Sub{workspace: workspace, mapID: mapID, user: user, key: h.instance + "/" + uuid.NewString(), ch: make(chan Message, bufferSize)}
	h.mu.Lock()
	if h.subs[mapID] == nil {
		h.subs[mapID] = map[*Sub]struct{}{}
	}
	h.subs[mapID][s] = struct{}{}
	h.mu.Unlock()
	h.announce(ctx, "join", s)
	return s
}

func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	delete(h.subs[s.mapID], s)
	if len(h.subs[s.mapID]) == 0 {
		delete(h.subs, s.mapID)
	}
	if !s.dropped {
		s.dropped = true
		close(s.ch)
	}
	h.mu.Unlock()
	// The request context is gone by now; leaving is still worth announcing.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h.announce(ctx, "leave", s)
}

// ---- presence ----

type presenceNote struct {
	Instance string    `json:"i"`
	Op       string    `json:"op"` // join, leave, beat
	Map      uuid.UUID `json:"m"`
	Key      string    `json:"k"`
	User     User      `json:"u"`
}

func (h *Hub) announce(ctx context.Context, op string, s *Sub) {
	b, _ := json.Marshal(presenceNote{Instance: h.instance, Op: op, Map: s.mapID, Key: s.key, User: s.user})
	if _, err := h.pool.Exec(ctx, `SELECT pg_notify('presence', $1)`, string(b)); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("live: announce presence", "err", err)
	}
}

// onPresence records a join, leave or heartbeat from any instance, this one
// included. A join from elsewhere makes this instance re-announce its own
// watchers of that map, so the newcomer sees them without waiting a beat.
func (h *Hub) onPresence(ctx context.Context, payload string) {
	var n presenceNote
	if err := json.Unmarshal([]byte(payload), &n); err != nil {
		return
	}
	h.mu.Lock()
	if n.Op == "leave" {
		delete(h.presence[n.Map], n.Key)
	} else {
		if h.presence[n.Map] == nil {
			h.presence[n.Map] = map[string]seen{}
		}
		h.presence[n.Map][n.Key] = seen{user: n.User, expires: time.Now().Add(expiry)}
	}
	h.publishPresence(n.Map)
	var mine []*Sub
	if n.Op == "join" && n.Instance != h.instance {
		for s := range h.subs[n.Map] {
			mine = append(mine, s)
		}
	}
	h.mu.Unlock()
	for _, s := range mine {
		h.announce(ctx, "beat", s)
	}
}

// publishPresence sends the map's present users, once each, by name. Holds h.mu.
func (h *Hub) publishPresence(mapID uuid.UUID) {
	byID := map[uuid.UUID]User{}
	for _, p := range h.presence[mapID] {
		byID[p.user.ID] = p.user
	}
	users := make([]User, 0, len(byID))
	for _, u := range byID {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Name < users[j].Name })
	for s := range h.subs[mapID] {
		h.send(s, Message{Type: "presence", Users: users})
	}
}

// beat re-announces this instance's watchers and forgets watchers whose
// instance stopped announcing them.
func (h *Hub) beat(ctx context.Context) {
	t := time.NewTicker(heartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.mu.Lock()
		var mine []*Sub
		for _, subs := range h.subs {
			for s := range subs {
				mine = append(mine, s)
			}
		}
		now := time.Now()
		for mapID, ps := range h.presence {
			changed := false
			for k, p := range ps {
				if now.After(p.expires) {
					delete(ps, k)
					changed = true
				}
			}
			if changed {
				h.publishPresence(mapID)
			}
			if len(ps) == 0 {
				delete(h.presence, mapID)
			}
		}
		h.mu.Unlock()
		for _, s := range mine {
			h.announce(ctx, "beat", s)
		}
	}
}
