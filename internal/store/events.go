package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Event is one row of the event log, as live clients and history read it.
type Event struct {
	ID          int64          `json:"id"`
	WorkspaceID uuid.UUID      `json:"-"`
	MapID       *uuid.UUID     `json:"mapId"`
	ActorID     *uuid.UUID     `json:"actorId"`
	Entity      string         `json:"entity"`
	EntityID    uuid.UUID      `json:"entityId"`
	Action      string         `json:"action"`
	Before      map[string]any `json:"before"`
	After       map[string]any `json:"after"`
	At          time.Time      `json:"at"`
}

const eventCols = `id, workspace_id, map_id, actor_id, entity_type, entity_id, action, before, after, created_at`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.WorkspaceID, &e.MapID, &e.ActorID, &e.Entity, &e.EntityID, &e.Action, &e.Before, &e.After, &e.At)
	return e, err
}

// LastEventID is the newest event id, 0 when the log is empty.
func (s *Store) LastEventID(ctx context.Context) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(max(id), 0) FROM events`).Scan(&id)
	return id, err
}

// EventsSince returns up to limit events with ids above after, oldest first,
// across all workspaces. Events commit in id order, so following ids misses none.
func (s *Store) EventsSince(ctx context.Context, after int64, limit int) ([]Event, error) {
	return collect(ctx, s.pool, scanEvent, `SELECT `+eventCols+` FROM events WHERE id > $1 ORDER BY id LIMIT $2`, after, limit)
}

// MapEventsSince returns what a client showing the map needs after event
// `after`: the map's own events and the workspace-wide ones (workflows and
// statuses), oldest first, up to limit.
func (s *Store) MapEventsSince(ctx context.Context, workspaceID, mapID uuid.UUID, after int64, limit int) ([]Event, error) {
	return collect(ctx, s.pool, scanEvent,
		`SELECT `+eventCols+` FROM events
		 WHERE workspace_id = $1 AND (map_id = $2 OR map_id IS NULL) AND id > $3 ORDER BY id LIMIT $4`,
		workspaceID, mapID, after, limit)
}

// CheckMap returns ErrNotFound unless the map is live and in the workspace.
func (s *Store) CheckMap(ctx context.Context, workspaceID, mapID uuid.UUID) error {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT true FROM maps WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`,
		mapID, workspaceID).Scan(&ok)
	return notFound(err)
}

// HistoryEntry is an event with the name of who made it ("" when that
// account no longer exists).
type HistoryEntry struct {
	Event
	ActorName string `json:"actorName"`
}

// A page of history, newest first. Next is the cursor for the page after it
// (pass it as before), nil on the last page.
type History struct {
	Entries []HistoryEntry `json:"entries"`
	Next    *int64         `json:"next"`
}

func (s *Store) history(ctx context.Context, where string, before int64, limit int, args ...any) (History, error) {
	n := len(args)
	cond := where
	if before > 0 {
		cond += fmt.Sprintf(" AND e.id < $%d", n+1)
		args = append(args, before)
	}
	args = append(args, limit+1)
	rows, err := s.pool.Query(ctx,
		`SELECT e.id, e.workspace_id, e.map_id, e.actor_id, e.entity_type, e.entity_id, e.action, e.before, e.after, e.created_at,
		        coalesce(u.display_name, '')
		 FROM events e LEFT JOIN users u ON u.id = e.actor_id
		 WHERE `+cond+fmt.Sprintf(` ORDER BY e.id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return History{}, err
	}
	entries, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (HistoryEntry, error) {
		var h HistoryEntry
		e := &h.Event
		err := r.Scan(&e.ID, &e.WorkspaceID, &e.MapID, &e.ActorID, &e.Entity, &e.EntityID, &e.Action, &e.Before, &e.After, &e.At, &h.ActorName)
		return h, err
	})
	if err != nil {
		return History{}, err
	}
	out := History{Entries: entries}
	if len(entries) > limit {
		out.Entries = entries[:limit]
		next := out.Entries[limit-1].ID
		out.Next = &next
	}
	return out, nil
}

// MapHistory is the map's activity: every event on the map, newest first.
func (s *Store) MapHistory(ctx context.Context, workspaceID, mapID uuid.UUID, before int64, limit int) (History, error) {
	if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
		return History{}, err
	}
	return s.history(ctx, `e.workspace_id = $1 AND e.map_id = $2`, before, limit, workspaceID, mapID)
}

// NodeHistory is an objective's or activity's own history: its changes, its
// files and its comments. Its steps' changes are in theirs.
func (s *Store) NodeHistory(ctx context.Context, workspaceID, mapID, nodeID uuid.UUID, before int64, limit int) (History, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT true FROM nodes n JOIN maps m ON m.id = n.map_id
		 WHERE n.id = $1 AND n.map_id = $2 AND m.workspace_id = $3 AND m.deleted_at IS NULL`,
		nodeID, mapID, workspaceID).Scan(&ok)
	if err != nil {
		return History{}, notFound(err)
	}
	return s.history(ctx,
		`e.workspace_id = $1 AND ((e.entity_type = 'node' AND e.entity_id = $2)
		   OR (e.entity_type IN ('attachment', 'comment') AND coalesce(e.after, e.before)->>'nodeId' = $2::text))`,
		before, limit, workspaceID, nodeID)
}

// StepHistory is one step's events, its placements' (moves), its links and
// its files, newest first. Deleted steps keep their history.
func (s *Store) StepHistory(ctx context.Context, workspaceID, mapID, stepID uuid.UUID, before int64, limit int) (History, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT true FROM steps s JOIN placements p ON p.step_id = s.id JOIN maps m ON m.id = p.map_id
		 WHERE s.id = $1 AND p.map_id = $2 AND s.workspace_id = $3 AND m.deleted_at IS NULL`,
		stepID, mapID, workspaceID).Scan(&ok)
	if err != nil {
		return History{}, notFound(err)
	}
	return s.history(ctx,
		`e.workspace_id = $1 AND ((e.entity_type = 'step' AND e.entity_id = $2 AND NOT e.echo)
		   OR (e.entity_type = 'placement' AND e.entity_id IN (SELECT id FROM placements WHERE step_id = $2))
		   OR (e.entity_type = 'attachment' AND NOT e.echo AND coalesce(e.after, e.before)->>'stepId' = $2::text)
		   OR (e.entity_type = 'dependency' AND e.map_id = $3
		       AND $2::text IN (coalesce(e.after, e.before)->>'blockerId', coalesce(e.after, e.before)->>'blockedId')))`,
		before, limit, workspaceID, stepID, mapID)
}
