package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MapView is everything the map screen shows, read in one snapshot. Clients
// keep it current by applying events with ids above LastEventID. Deleted
// nodes and steps are left out.
type MapView struct {
	Map        Map         `json:"map"`
	Workflow   Workflow    `json:"workflow"`
	Labels     []Label     `json:"labels"`   // the workspace's, for naming steps' labelIds
	Personas   []Persona   `json:"personas"` // the workspace's, for naming objectives' personaIds
	Nodes      []Node      `json:"nodes"`
	Releases   []Release   `json:"releases"`
	Steps      []Step      `json:"steps"`
	Placements []Placement `json:"placements"`
	// Dependencies touching steps on this map; OtherSteps names their ends on other maps.
	Dependencies []Dependency `json:"dependencies"`
	OtherSteps   []StepRef    `json:"otherSteps"`
	LastEventID  int64        `json:"lastEventId"`
}

func (s *Store) MapView(ctx context.Context, workspaceID, mapID uuid.UUID) (MapView, error) {
	var v MapView
	// Repeatable read gives one snapshot. Events commit in id order (see
	// eventLogLock), so every event up to LastEventID is in that snapshot.
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		if v.Map, err = scanMap(tx.QueryRow(ctx,
			`SELECT `+mapCols+` FROM maps WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`, mapID, workspaceID)); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(id), 0) FROM events`).Scan(&v.LastEventID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id, name, is_default FROM workflows WHERE id = $1`, v.Map.WorkflowID).
			Scan(&v.Workflow.ID, &v.Workflow.Name, &v.Workflow.IsDefault); err != nil {
			return err
		}
		if v.Workflow.Statuses, err = listStatuses(ctx, tx, v.Map.WorkflowID); err != nil {
			return err
		}
		if v.Labels, err = collect(ctx, tx, scanLabel,
			`SELECT `+labelCols+` FROM labels WHERE workspace_id = $1 ORDER BY name, id`, workspaceID); err != nil {
			return err
		}
		if v.Personas, err = collect(ctx, tx, scanPersona,
			`SELECT `+personaCols+` FROM personas WHERE workspace_id = $1 ORDER BY name, id`, workspaceID); err != nil {
			return err
		}
		if v.Nodes, err = collect(ctx, tx, scanNode,
			`SELECT `+nodeCols+` FROM nodes WHERE map_id = $1 AND deleted_at IS NULL ORDER BY rank`, mapID); err != nil {
			return err
		}
		if v.Releases, err = collect(ctx, tx, scanRelease,
			`SELECT `+releaseCols+` FROM releases WHERE map_id = $1 ORDER BY rank`, mapID); err != nil {
			return err
		}
		if v.Steps, err = collect(ctx, tx, scanStep,
			`SELECT `+stepCols+` FROM steps s JOIN placements p ON p.step_id = s.id
			 WHERE p.map_id = $1 AND s.deleted_at IS NULL ORDER BY s.key`, mapID); err != nil {
			return err
		}
		v.Placements, err = collect(ctx, tx, scanPlacement,
			`SELECT p.id, p.step_id, p.activity_id, p.release_id, p.rank FROM placements p JOIN steps s ON s.id = p.step_id
			 WHERE p.map_id = $1 AND s.deleted_at IS NULL ORDER BY p.rank`, mapID)
		if err != nil {
			return err
		}
		v.Dependencies, v.OtherSteps, err = mapDependencies(ctx, tx, mapID)
		return err
	})
	return v, err
}

func collect[T any](ctx context.Context, q querier, scan func(pgx.Row) (T, error), sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (T, error) { return scan(r) })
}
