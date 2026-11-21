package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Elsewhere says a step on one map is also on another.
type Elsewhere struct {
	StepID  uuid.UUID `json:"stepId"`
	MapID   uuid.UUID `json:"mapId"`
	MapName string    `json:"mapName"`
}

// echoes returns c and a copy of it for every other live map the step is on,
// so each of those maps' feeds and histories has the change.
func echoes(ctx context.Context, tx pgx.Tx, stepID uuid.UUID, c change) ([]change, error) {
	maps, err := stepMaps(ctx, tx, stepID)
	if err != nil {
		return nil, err
	}
	out := []change{c}
	for _, m := range maps {
		if m != c.mapID {
			e := c
			e.mapID, e.echo = m, true
			out = append(out, e)
		}
	}
	return out, nil
}

// mapElsewhere lists, for the steps shown on a map, the other maps showing them.
func mapElsewhere(ctx context.Context, tx pgx.Tx, mapID uuid.UUID) ([]Elsewhere, error) {
	return collect(ctx, tx, func(row pgx.Row) (Elsewhere, error) {
		var e Elsewhere
		err := row.Scan(&e.StepID, &e.MapID, &e.MapName)
		return e, err
	},
		`SELECT p.step_id, m.id, m.name FROM placements p
		 JOIN steps s ON s.id = p.step_id AND s.deleted_at IS NULL
		 JOIN placements q ON q.step_id = p.step_id AND q.map_id <> p.map_id
		 JOIN maps m ON m.id = q.map_id AND m.deleted_at IS NULL
		 JOIN nodes n ON n.id = q.activity_id AND n.deleted_at IS NULL
		 WHERE p.map_id = $1 ORDER BY m.name, m.id`, mapID)
}

// PlaceStep shows an existing step of the workspace on this map too, in a
// cell of it. The map must use the step's workflow, since the step keeps
// its status.
func (s *Store) PlaceStep(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID, activityID uuid.UUID, releaseID *uuid.UUID, pos Position) (Step, Placement, error) {
	var st Step
	p := Placement{ID: newID(), StepID: stepID, ActivityID: activityID, ReleaseID: releaseID}
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		var err error
		if st, err = scanStep(tx.QueryRow(ctx,
			`SELECT `+stepCols+` FROM steps s WHERE s.id = $1 AND s.workspace_id = $2 AND s.deleted_at IS NULL FOR UPDATE OF s`,
			stepID, workspaceID)); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("%w: stepId must be a step of this workspace", ErrInvalid)
			}
			return nil, err
		}
		var sameFlow bool
		var mapName string
		if err := tx.QueryRow(ctx,
			`SELECT m.workflow_id = st.workflow_id, m.name FROM maps m, statuses st WHERE m.id = $1 AND st.id = $2`,
			mapID, st.StatusID).Scan(&sameFlow, &mapName); err != nil {
			return nil, err
		}
		if !sameFlow {
			return nil, fmt.Errorf("%w: this map uses a different workflow from %s's map", ErrInvalid, st.Key)
		}
		var here bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM placements WHERE step_id = $1 AND map_id = $2)`, stepID, mapID).
			Scan(&here); err != nil {
			return nil, err
		}
		if here {
			return nil, fmt.Errorf("%w: %s is already on this map", ErrConflict, st.Key)
		}
		if err := checkCell(ctx, tx, mapID, activityID, releaseID); err != nil {
			return nil, err
		}
		if p.Rank, err = cellSiblings(mapID, activityID, releaseID).rank(ctx, tx, pos, p.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO placements (id, step_id, map_id, activity_id, release_id, rank) VALUES ($1, $2, $3, $4, $5, $6)`,
			p.ID, stepID, mapID, activityID, releaseID, p.Rank); err != nil {
			return nil, err
		}
		after := fields(st)
		after["mapId"], after["mapName"] = mapID, mapName
		changes, err := echoes(ctx, tx, stepID, change{mapID: mapID, entity: "step", id: stepID, action: "place", after: after})
		if err != nil {
			return nil, err
		}
		return append(changes, created(mapID, "placement", p.ID, p)), nil
	})
	return st, p, err
}

// UnplaceStep takes a step off this map. It must still be on another map;
// taking it off its last map is a delete.
func (s *Store) UnplaceStep(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID) (Placement, error) {
	var p Placement
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		st, here, err := mapStep(ctx, tx, mapID, stepID)
		if err != nil {
			return nil, err
		}
		p = here
		var others int
		var mapName string
		if err := tx.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM placements q JOIN maps m ON m.id = q.map_id AND m.deleted_at IS NULL
			         JOIN nodes n ON n.id = q.activity_id AND n.deleted_at IS NULL
			         WHERE q.step_id = $1 AND q.map_id <> $2),
			        (SELECT name FROM maps WHERE id = $2)`, stepID, mapID).Scan(&others, &mapName); err != nil {
			return nil, err
		}
		if others == 0 {
			return nil, fmt.Errorf("%w: %s is only on this map; delete it instead", ErrInvalid, st.Key)
		}
		changes, err := echoes(ctx, tx, stepID, change{mapID: mapID, entity: "step", id: stepID, action: "unplace",
			before: map[string]any{"key": st.Key, "title": st.Title, "mapId": mapID, "mapName": mapName}})
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `DELETE FROM placements WHERE id = $1`, p.ID)
		return changes, err
	})
	return p, err
}
