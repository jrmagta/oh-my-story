package store

import (
	"context"
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
