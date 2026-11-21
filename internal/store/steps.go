package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Step struct {
	ID            uuid.UUID   `json:"id"`
	Key           string      `json:"key"`
	Title         string      `json:"title"`
	DescriptionMD string      `json:"descriptionMd"`
	StatusID      uuid.UUID   `json:"statusId"`
	AssigneeID    *uuid.UUID  `json:"assigneeId"`
	Estimate      *float64    `json:"estimate"`
	LabelIDs      []uuid.UUID `json:"labelIds"`     // sorted, never null
	CommentCount  int         `json:"commentCount"` // live comments, replies included
	Version       int         `json:"version"`
}

// Placement is where a step sits on a map: an activity, a release (nil for
// Unscheduled) and a rank within that cell.
type Placement struct {
	ID         uuid.UUID  `json:"id"`
	StepID     uuid.UUID  `json:"stepId"`
	ActivityID uuid.UUID  `json:"activityId"`
	ReleaseID  *uuid.UUID `json:"releaseId"`
	Rank       string     `json:"rank"`
}

// stepCols reads a step aliased s, with its label ids. In UPDATE ... RETURNING
// the label subquery sees step_labels as it was before that statement, so
// change labels first.
const stepCols = `s.id, s.key, s.title, s.description_md, s.status_id, s.assignee_id, s.estimate,
	coalesce((SELECT array_agg(sl.label_id ORDER BY sl.label_id) FROM step_labels sl WHERE sl.step_id = s.id), '{}'),
	(SELECT count(*) FROM comments cm WHERE cm.step_id = s.id AND cm.deleted_at IS NULL),
	s.version`

func scanStep(row pgx.Row) (Step, error) {
	var st Step
	err := row.Scan(&st.ID, &st.Key, &st.Title, &st.DescriptionMD, &st.StatusID, &st.AssigneeID, &st.Estimate, &st.LabelIDs, &st.CommentCount, &st.Version)
	return st, notFound(err)
}

const placementCols = `id, step_id, activity_id, release_id, rank`

func scanPlacement(row pgx.Row) (Placement, error) {
	var p Placement
	err := row.Scan(&p.ID, &p.StepID, &p.ActivityID, &p.ReleaseID, &p.Rank)
	return p, notFound(err)
}

// cellSiblings orders the placements in one cell. Positions name steps, not placements.
func cellSiblings(mapID, activityID uuid.UUID, releaseID *uuid.UUID) siblings {
	return siblings{table: "placements", where: "map_id = $1 AND activity_id = $2 AND release_id IS NOT DISTINCT FROM $3",
		args: []any{mapID, activityID, releaseID}, anchor: "step_id"}
}

// checkCell verifies that an activity and release can hold steps on this map.
func checkCell(ctx context.Context, tx pgx.Tx, mapID, activityID uuid.UUID, releaseID *uuid.UUID) error {
	n, err := liveNode(ctx, tx, mapID, activityID)
	if err != nil || n.Level != "activity" {
		return fmt.Errorf("%w: steps sit under an activity of this map", ErrInvalid)
	}
	if releaseID != nil {
		if _, err := mapRelease(ctx, tx, mapID, *releaseID); err != nil {
			return fmt.Errorf("%w: release not found on this map", ErrInvalid)
		}
	}
	return nil
}

// mapStep loads a live step placed on the map, with its placement.
func mapStep(ctx context.Context, tx pgx.Tx, mapID, stepID uuid.UUID) (Step, Placement, error) {
	var st Step
	var p Placement
	err := tx.QueryRow(ctx,
		`SELECT `+stepCols+`, p.id, p.step_id, p.activity_id, p.release_id, p.rank
		 FROM steps s JOIN placements p ON p.step_id = s.id JOIN nodes n ON n.id = p.activity_id AND n.deleted_at IS NULL
		 WHERE s.id = $1 AND p.map_id = $2 AND s.deleted_at IS NULL FOR UPDATE OF s`, stepID, mapID).
		Scan(&st.ID, &st.Key, &st.Title, &st.DescriptionMD, &st.StatusID, &st.AssigneeID, &st.Estimate, &st.LabelIDs, &st.CommentCount, &st.Version,
			&p.ID, &p.StepID, &p.ActivityID, &p.ReleaseID, &p.Rank)
	return st, p, notFound(err)
}

type StepInput struct {
	Title, DescriptionMD string
	ActivityID           uuid.UUID
	ReleaseID            *uuid.UUID
}

// CreateStep adds a step to the workspace and places it on the map. It gets
// the next workspace key and the first status of the map's workflow.
func (s *Store) CreateStep(ctx context.Context, actor, workspaceID, mapID uuid.UUID, in StepInput, pos Position) (Step, Placement, error) {
	st := Step{ID: newID(), Title: in.Title, DescriptionMD: in.DescriptionMD, LabelIDs: []uuid.UUID{}}
	p := Placement{ID: newID(), StepID: st.ID, ActivityID: in.ActivityID, ReleaseID: in.ReleaseID}
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if err := checkCell(ctx, tx, mapID, in.ActivityID, in.ReleaseID); err != nil {
			return nil, err
		}
		var err error
		if p.Rank, err = cellSiblings(mapID, in.ActivityID, in.ReleaseID).rank(ctx, tx, pos, p.ID); err != nil {
			return nil, err
		}
		err = tx.QueryRow(ctx,
			`SELECT s.id FROM statuses s JOIN maps m ON m.workflow_id = s.workflow_id WHERE m.id = $1 ORDER BY s.rank LIMIT 1`,
			mapID).Scan(&st.StatusID)
		if err != nil {
			return nil, err
		}
		// The row lock on the workspace serialises key allocation.
		err = tx.QueryRow(ctx,
			`UPDATE workspaces SET next_step_seq = next_step_seq + 1 WHERE id = $1
			 RETURNING key_prefix || '-' || (next_step_seq - 1)`, workspaceID).Scan(&st.Key)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO steps (id, workspace_id, key, title, description_md, status_id, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			st.ID, workspaceID, st.Key, st.Title, st.DescriptionMD, st.StatusID, actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO placements (id, step_id, map_id, activity_id, release_id, rank) VALUES ($1, $2, $3, $4, $5, $6)`,
			p.ID, p.StepID, mapID, p.ActivityID, p.ReleaseID, p.Rank); err != nil {
			return nil, err
		}
		return []change{created(mapID, "step", st.ID, st), created(mapID, "placement", p.ID, p)}, nil
	})
	return st, p, err
}

type StepPatch struct {
	Title, DescriptionMD *string
	StatusID             *uuid.UUID   // a status of the map's workflow
	SetAssignee          bool         // AssigneeID is applied only when set; nil unassigns
	AssigneeID           *uuid.UUID   // a member of the workspace
	SetEstimate          bool         // Estimate is applied only when set; nil clears it
	Estimate             *float64     // 0 to 9999.99, at most two decimals
	LabelIDs             *[]uuid.UUID // replaces the step's labels; labels of the workspace, no duplicates
}

func (s *Store) UpdateStep(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID, p StepPatch) (Step, error) {
	var st Step
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, _, err := mapStep(ctx, tx, mapID, stepID)
		if err != nil {
			return nil, err
		}
		if p.StatusID != nil {
			var ok bool
			err := tx.QueryRow(ctx,
				`SELECT true FROM statuses s JOIN maps m ON m.workflow_id = s.workflow_id WHERE s.id = $1 AND m.id = $2`,
				*p.StatusID, mapID).Scan(&ok)
			if err != nil {
				return nil, fmt.Errorf("%w: status is not in this map's workflow", ErrInvalid)
			}
		}
		if p.SetAssignee && p.AssigneeID != nil {
			var ok bool
			err := tx.QueryRow(ctx, `SELECT true FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`,
				workspaceID, *p.AssigneeID).Scan(&ok)
			if err != nil {
				return nil, fmt.Errorf("%w: assignee is not a member of this workspace", ErrInvalid)
			}
		}
		if p.LabelIDs != nil {
			if err := setStepLabels(ctx, tx, workspaceID, stepID, *p.LabelIDs); err != nil {
				return nil, err
			}
		}
		if st, err = scanStep(tx.QueryRow(ctx,
			`UPDATE steps s SET title = COALESCE($2, title), description_md = COALESCE($3, description_md),
			   status_id = COALESCE($4, status_id), assignee_id = CASE WHEN $5 THEN $6 ELSE assignee_id END,
			   estimate = CASE WHEN $7 THEN $8 ELSE estimate END,
			   version = version + 1, updated_at = now()
			 WHERE id = $1 RETURNING `+stepCols,
			stepID, p.Title, p.DescriptionMD, p.StatusID, p.SetAssignee, p.AssigneeID, p.SetEstimate, p.Estimate)); err != nil {
			return nil, err
		}
		c, ok := updated(mapID, "step", stepID, "update", before, st)
		if !ok {
			return nil, nil
		}
		changes, err := echoes(ctx, tx, stepID, c)
		if err != nil {
			return nil, err
		}
		if before.StatusID != st.StatusID {
			refs, err := linkedElsewhere(ctx, tx, stepID, st)
			if err != nil {
				return nil, err
			}
			changes = append(changes, refs...)
		}
		return changes, nil
	})
	return st, err
}

type StepMove struct {
	ActivityID uuid.UUID
	ReleaseID  *uuid.UUID // nil: Unscheduled
	Position   Position
	// SeenVersion is the step version the mover's client showed. The move
	// applies either way; MoveStep reports whether it was stale.
	SeenVersion int
}

// MoveStep places a step in a cell. stale is true when someone else changed
// the step after the mover last saw it.
func (s *Store) MoveStep(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID, m StepMove) (p Placement, stale bool, err error) {
	err = s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		st, before, err := mapStep(ctx, tx, mapID, stepID)
		if err != nil {
			return nil, err
		}
		stale = st.Version != m.SeenVersion
		if err := checkCell(ctx, tx, mapID, m.ActivityID, m.ReleaseID); err != nil {
			return nil, err
		}
		p = before
		p.ActivityID, p.ReleaseID = m.ActivityID, m.ReleaseID
		if p.Rank, err = cellSiblings(mapID, m.ActivityID, m.ReleaseID).rank(ctx, tx, m.Position, p.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE placements SET activity_id = $2, release_id = $3, rank = $4 WHERE id = $1`,
			p.ID, p.ActivityID, p.ReleaseID, p.Rank); err != nil {
			return nil, err
		}
		var version int
		if err := tx.QueryRow(ctx, `UPDATE steps SET version = version + 1, updated_at = now() WHERE id = $1 RETURNING version`,
			stepID).Scan(&version); err != nil {
			return nil, err
		}
		c, _ := updated(mapID, "placement", p.ID, "move", before, p)
		// A move changes the step's version even when it lands where it was
		// (the same rank); live clients need the new version either way.
		if c.before == nil {
			c.before, c.after = map[string]any{}, map[string]any{}
		}
		c.before["stepVersion"], c.after["stepVersion"] = st.Version, version
		return []change{c}, nil
	})
	return p, stale, err
}

func (s *Store) DeleteStep(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		st, _, err := mapStep(ctx, tx, mapID, stepID)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE steps SET deleted_at = now(), updated_at = now() WHERE id = $1`, stepID); err != nil {
			return nil, err
		}
		return echoes(ctx, tx, stepID, gone(mapID, "step", stepID, "delete", map[string]any{"key": st.Key, "title": st.Title}))
	})
}

// RestoreStep undoes DeleteStep. The step's activity must not be deleted.
func (s *Store) RestoreStep(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID) (Step, error) {
	var st Step
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		var activityID uuid.UUID
		err := tx.QueryRow(ctx,
			`SELECT p.activity_id FROM steps s JOIN placements p ON p.step_id = s.id
			 WHERE s.id = $1 AND p.map_id = $2 AND s.workspace_id = $3 AND s.deleted_at IS NOT NULL`,
			stepID, mapID, workspaceID).Scan(&activityID)
		if err != nil {
			return nil, notFound(err)
		}
		if _, err := liveNode(ctx, tx, mapID, activityID); err != nil {
			return nil, fmt.Errorf("%w: the step's activity is deleted; restore it first", ErrInvalid)
		}
		if st, err = scanStep(tx.QueryRow(ctx,
			`UPDATE steps s SET deleted_at = NULL, updated_at = now() WHERE id = $1 RETURNING `+stepCols, stepID)); err != nil {
			return nil, err
		}
		return echoes(ctx, tx, stepID, gone(mapID, "step", stepID, "restore", map[string]any{"key": st.Key, "title": st.Title}))
	})
	return st, err
}

// setStepLabels replaces a step's labels with ids, which must all be labels of
// the workspace.
func setStepLabels(ctx context.Context, tx pgx.Tx, workspaceID, stepID uuid.UUID, ids []uuid.UUID) error {
	var found int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM labels WHERE workspace_id = $1 AND id = ANY($2)`, workspaceID, ids).
		Scan(&found); err != nil {
		return err
	}
	if found != len(ids) {
		return fmt.Errorf("%w: labelIds must be labels of this workspace", ErrInvalid)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM step_labels WHERE step_id = $1 AND NOT (label_id = ANY($2))`, stepID, ids); err != nil {
		return err
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO step_labels (step_id, label_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, stepID, ids)
	return err
}
