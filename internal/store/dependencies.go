package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Dependency says the blocker step blocks the blocked step.
type Dependency struct {
	ID        uuid.UUID `json:"id"`
	BlockerID uuid.UUID `json:"blockerId"`
	BlockedID uuid.UUID `json:"blockedId"`
}

// StepRef names a step a map's dependencies point at that is not on that map.
type StepRef struct {
	ID      uuid.UUID `json:"id"`
	Key     string    `json:"key"`
	Title   string    `json:"title"`
	Done    bool      `json:"done"`
	MapID   uuid.UUID `json:"mapId"`
	MapName string    `json:"mapName"`
}

const dependencyCols = `d.id, d.blocker_id, d.blocked_id`

func scanDependency(row pgx.Row) (Dependency, error) {
	var d Dependency
	err := row.Scan(&d.ID, &d.BlockerID, &d.BlockedID)
	return d, notFound(err)
}

// stepMaps lists the live maps a step is placed on.
func stepMaps(ctx context.Context, tx pgx.Tx, stepID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx,
		`SELECT p.map_id FROM placements p JOIN maps m ON m.id = p.map_id AND m.deleted_at IS NULL WHERE p.step_id = $1`, stepID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// dependencyChanges builds one event per map either step is on, so every open
// map showing one end hears about the link.
func dependencyChanges(ctx context.Context, tx pgx.Tx, d Dependency, action string, label map[string]any) ([]change, error) {
	seen := map[uuid.UUID]bool{}
	var out []change
	for _, step := range []uuid.UUID{d.BlockerID, d.BlockedID} {
		maps, err := stepMaps(ctx, tx, step)
		if err != nil {
			return nil, err
		}
		for _, m := range maps {
			if seen[m] {
				continue
			}
			seen[m] = true
			if action == "create" {
				out = append(out, change{mapID: m, entity: "dependency", id: d.ID, action: action, after: label})
			} else {
				out = append(out, change{mapID: m, entity: "dependency", id: d.ID, action: action, before: label})
			}
		}
	}
	return out, nil
}

// CreateDependency records that blocker blocks blocked. Both must be live
// steps of the workspace and at least one must be on the map. A link that
// would close a cycle is refused.
func (s *Store) CreateDependency(ctx context.Context, actor, workspaceID, mapID, blockerID, blockedID uuid.UUID) (Dependency, error) {
	d := Dependency{ID: newID(), BlockerID: blockerID, BlockedID: blockedID}
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if blockerID == blockedID {
			return nil, fmt.Errorf("%w: a step cannot block itself", ErrInvalid)
		}
		// Serialise dependency writes in the workspace so two links cannot close a cycle together.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('oms.deps.' || $1::text))`, workspaceID); err != nil {
			return nil, err
		}
		keys := map[uuid.UUID]string{}
		var onMap bool
		for _, id := range []uuid.UUID{blockerID, blockedID} {
			var key string
			var here bool
			err := tx.QueryRow(ctx,
				`SELECT s.key, EXISTS (SELECT 1 FROM placements p WHERE p.step_id = s.id AND p.map_id = $3)
				 FROM steps s WHERE s.id = $1 AND s.workspace_id = $2 AND s.deleted_at IS NULL`, id, workspaceID, mapID).Scan(&key, &here)
			if err != nil {
				return nil, fmt.Errorf("%w: steps must be steps of this workspace", ErrInvalid)
			}
			keys[id] = key
			onMap = onMap || here
		}
		if !onMap {
			return nil, fmt.Errorf("%w: one of the steps must be on this map", ErrInvalid)
		}
		var cycle bool
		if err := tx.QueryRow(ctx,
			`WITH RECURSIVE down(id) AS (
			   SELECT blocked_id FROM dependencies WHERE blocker_id = $1
			   UNION SELECT d.blocked_id FROM dependencies d JOIN down ON d.blocker_id = down.id)
			 SELECT EXISTS (SELECT 1 FROM down WHERE id = $2)`, blockedID, blockerID).Scan(&cycle); err != nil {
			return nil, err
		}
		if cycle {
			return nil, fmt.Errorf("%w: %s already depends on %s, so this would make a loop", ErrInvalid, keys[blockerID], keys[blockedID])
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO dependencies (id, workspace_id, blocker_id, blocked_id, created_by) VALUES ($1, $2, $3, $4, $5)`,
			d.ID, workspaceID, blockerID, blockedID, actor); err != nil {
			return nil, err
		}
		return dependencyChanges(ctx, tx, d, "create", map[string]any{
			"id": d.ID, "blockerId": blockerID, "blockedId": blockedID, "blockerKey": keys[blockerID], "blockedKey": keys[blockedID]})
	})
	return d, err
}

// DeleteDependency removes a link that touches a step on the map.
func (s *Store) DeleteDependency(ctx context.Context, actor, workspaceID, mapID, id uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		var blockerKey, blockedKey string
		var d Dependency
		err := tx.QueryRow(ctx,
			`SELECT `+dependencyCols+`, a.key, b.key FROM dependencies d
			 JOIN steps a ON a.id = d.blocker_id JOIN steps b ON b.id = d.blocked_id
			 WHERE d.id = $1 AND d.workspace_id = $2
			   AND EXISTS (SELECT 1 FROM placements p WHERE p.map_id = $3 AND p.step_id IN (d.blocker_id, d.blocked_id))
			 FOR UPDATE OF d`, id, workspaceID, mapID).Scan(&d.ID, &d.BlockerID, &d.BlockedID, &blockerKey, &blockedKey)
		if err != nil {
			return nil, notFound(err)
		}
		changes, err := dependencyChanges(ctx, tx, d, "delete", map[string]any{
			"blockerId": d.BlockerID, "blockedId": d.BlockedID, "blockerKey": blockerKey, "blockedKey": blockedKey})
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `DELETE FROM dependencies WHERE id = $1`, id)
		return changes, err
	})
}

// linkedElsewhere tells maps that show st only as the far end of a dependency
// (it is not placed on them) whether it is now done, as a stepref update.
func linkedElsewhere(ctx context.Context, tx pgx.Tx, stepID uuid.UUID, st Step) ([]change, error) {
	var done bool
	if err := tx.QueryRow(ctx, `SELECT category = 'done' FROM statuses WHERE id = $1`, st.StatusID).Scan(&done); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx,
		`SELECT DISTINCT p.map_id FROM dependencies d
		 JOIN placements p ON p.step_id = CASE WHEN d.blocker_id = $1 THEN d.blocked_id ELSE d.blocker_id END
		 JOIN maps m ON m.id = p.map_id AND m.deleted_at IS NULL
		 WHERE $1 IN (d.blocker_id, d.blocked_id)
		   AND NOT EXISTS (SELECT 1 FROM placements q WHERE q.step_id = $1 AND q.map_id = p.map_id)`, stepID)
	if err != nil {
		return nil, err
	}
	maps, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	out := make([]change, 0, len(maps))
	for _, m := range maps {
		out = append(out, change{mapID: m, entity: "stepref", id: stepID, action: "update", after: map[string]any{"done": done, "key": st.Key}})
	}
	return out, nil
}

// mapDependencies loads the links touching live steps on a map, and the
// steps at their far ends that are not on it.
func mapDependencies(ctx context.Context, tx pgx.Tx, mapID uuid.UUID) ([]Dependency, []StepRef, error) {
	deps, err := collect(ctx, tx, scanDependency,
		`SELECT `+dependencyCols+` FROM dependencies d
		 JOIN steps a ON a.id = d.blocker_id AND a.deleted_at IS NULL
		 JOIN steps b ON b.id = d.blocked_id AND b.deleted_at IS NULL
		 WHERE EXISTS (SELECT 1 FROM placements p WHERE p.map_id = $1 AND p.step_id IN (d.blocker_id, d.blocked_id))
		 ORDER BY d.id`, mapID)
	if err != nil {
		return nil, nil, err
	}
	refs, err := collect(ctx, tx, func(row pgx.Row) (StepRef, error) {
		var r StepRef
		err := row.Scan(&r.ID, &r.Key, &r.Title, &r.Done, &r.MapID, &r.MapName)
		return r, err
	},
		`SELECT DISTINCT ON (s.id) s.id, s.key, s.title, st.category = 'done', m.id, m.name
		 FROM dependencies d
		 JOIN steps s ON s.id IN (d.blocker_id, d.blocked_id) AND s.deleted_at IS NULL
		 JOIN statuses st ON st.id = s.status_id
		 JOIN placements p ON p.step_id = s.id
		 JOIN maps m ON m.id = p.map_id AND m.deleted_at IS NULL
		 WHERE EXISTS (SELECT 1 FROM placements q WHERE q.map_id = $1 AND q.step_id IN (d.blocker_id, d.blocked_id))
		   AND NOT EXISTS (SELECT 1 FROM placements q WHERE q.map_id = $1 AND q.step_id = s.id)
		 ORDER BY s.id, m.name`, mapID)
	return deps, refs, err
}
