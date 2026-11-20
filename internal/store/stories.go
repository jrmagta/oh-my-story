package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Story struct {
	ID            uuid.UUID  `json:"id"`
	Key           string     `json:"key"`
	Title         string     `json:"title"`
	DescriptionMD string     `json:"descriptionMd"`
	StatusID      uuid.UUID  `json:"statusId"`
	AssigneeID    *uuid.UUID `json:"assigneeId"`
	Estimate      *float64   `json:"estimate"`
	Version       int        `json:"version"`
}

// Placement is where a story sits on a map: an activity, a release (nil for
// Unscheduled) and a rank within that cell.
type Placement struct {
	ID         uuid.UUID  `json:"id"`
	StoryID    uuid.UUID  `json:"storyId"`
	ActivityID uuid.UUID  `json:"activityId"`
	ReleaseID  *uuid.UUID `json:"releaseId"`
	Rank       string     `json:"rank"`
}

const storyCols = `s.id, s.key, s.title, s.description_md, s.status_id, s.assignee_id, s.estimate, s.version`

func scanStory(row pgx.Row) (Story, error) {
	var st Story
	err := row.Scan(&st.ID, &st.Key, &st.Title, &st.DescriptionMD, &st.StatusID, &st.AssigneeID, &st.Estimate, &st.Version)
	return st, notFound(err)
}

const placementCols = `id, story_id, activity_id, release_id, rank`

func scanPlacement(row pgx.Row) (Placement, error) {
	var p Placement
	err := row.Scan(&p.ID, &p.StoryID, &p.ActivityID, &p.ReleaseID, &p.Rank)
	return p, notFound(err)
}

// cellSiblings orders the placements in one cell. Positions name stories, not placements.
func cellSiblings(mapID, activityID uuid.UUID, releaseID *uuid.UUID) siblings {
	return siblings{table: "placements", where: "map_id = $1 AND activity_id = $2 AND release_id IS NOT DISTINCT FROM $3",
		args: []any{mapID, activityID, releaseID}, anchor: "story_id"}
}

// checkCell verifies that an activity and release can hold stories on this map.
func checkCell(ctx context.Context, tx pgx.Tx, mapID, activityID uuid.UUID, releaseID *uuid.UUID) error {
	n, err := liveNode(ctx, tx, mapID, activityID)
	if err != nil || n.Level != "activity" {
		return fmt.Errorf("%w: stories sit under an activity of this map", ErrInvalid)
	}
	if releaseID != nil {
		if _, err := mapRelease(ctx, tx, mapID, *releaseID); err != nil {
			return fmt.Errorf("%w: release not found on this map", ErrInvalid)
		}
	}
	return nil
}

// mapStory loads a live story placed on the map, with its placement.
func mapStory(ctx context.Context, tx pgx.Tx, mapID, storyID uuid.UUID) (Story, Placement, error) {
	var st Story
	var p Placement
	err := tx.QueryRow(ctx,
		`SELECT `+storyCols+`, p.id, p.story_id, p.activity_id, p.release_id, p.rank
		 FROM stories s JOIN placements p ON p.story_id = s.id
		 WHERE s.id = $1 AND p.map_id = $2 AND s.deleted_at IS NULL FOR UPDATE OF s`, storyID, mapID).
		Scan(&st.ID, &st.Key, &st.Title, &st.DescriptionMD, &st.StatusID, &st.AssigneeID, &st.Estimate, &st.Version,
			&p.ID, &p.StoryID, &p.ActivityID, &p.ReleaseID, &p.Rank)
	return st, p, notFound(err)
}

type StoryInput struct {
	Title, DescriptionMD string
	ActivityID           uuid.UUID
	ReleaseID            *uuid.UUID
}

// CreateStory adds a story to the workspace and places it on the map. It gets
// the next workspace key and the first status of the map's workflow.
func (s *Store) CreateStory(ctx context.Context, actor, workspaceID, mapID uuid.UUID, in StoryInput, pos Position) (Story, Placement, error) {
	st := Story{ID: newID(), Title: in.Title, DescriptionMD: in.DescriptionMD}
	p := Placement{ID: newID(), StoryID: st.ID, ActivityID: in.ActivityID, ReleaseID: in.ReleaseID}
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
			`UPDATE workspaces SET next_story_seq = next_story_seq + 1 WHERE id = $1
			 RETURNING key_prefix || '-' || (next_story_seq - 1)`, workspaceID).Scan(&st.Key)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO stories (id, workspace_id, key, title, description_md, status_id, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			st.ID, workspaceID, st.Key, st.Title, st.DescriptionMD, st.StatusID, actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO placements (id, story_id, map_id, activity_id, release_id, rank) VALUES ($1, $2, $3, $4, $5, $6)`,
			p.ID, p.StoryID, mapID, p.ActivityID, p.ReleaseID, p.Rank); err != nil {
			return nil, err
		}
		return []change{created(mapID, "story", st.ID, st), created(mapID, "placement", p.ID, p)}, nil
	})
	return st, p, err
}

type StoryPatch struct {
	Title, DescriptionMD *string
	StatusID             *uuid.UUID // a status of the map's workflow
	SetAssignee          bool       // AssigneeID is applied only when set; nil unassigns
	AssigneeID           *uuid.UUID // a member of the workspace
	SetEstimate          bool       // Estimate is applied only when set; nil clears it
	Estimate             *float64   // 0 to 9999.99, at most two decimals
}

func (s *Store) UpdateStory(ctx context.Context, actor, workspaceID, mapID, storyID uuid.UUID, p StoryPatch) (Story, error) {
	var st Story
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, _, err := mapStory(ctx, tx, mapID, storyID)
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
		if st, err = scanStory(tx.QueryRow(ctx,
			`UPDATE stories s SET title = COALESCE($2, title), description_md = COALESCE($3, description_md),
			   status_id = COALESCE($4, status_id), assignee_id = CASE WHEN $5 THEN $6 ELSE assignee_id END,
			   estimate = CASE WHEN $7 THEN $8 ELSE estimate END,
			   version = version + 1, updated_at = now()
			 WHERE id = $1 RETURNING `+storyCols,
			storyID, p.Title, p.DescriptionMD, p.StatusID, p.SetAssignee, p.AssigneeID, p.SetEstimate, p.Estimate)); err != nil {
			return nil, err
		}
		if c, ok := updated(mapID, "story", storyID, "update", before, st); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return st, err
}

type StoryMove struct {
	ActivityID uuid.UUID
	ReleaseID  *uuid.UUID // nil: Unscheduled
	Position   Position
	// SeenVersion is the story version the mover's client showed. The move
	// applies either way; MoveStory reports whether it was stale.
	SeenVersion int
}

// MoveStory places a story in a cell. stale is true when someone else changed
// the story after the mover last saw it.
func (s *Store) MoveStory(ctx context.Context, actor, workspaceID, mapID, storyID uuid.UUID, m StoryMove) (p Placement, stale bool, err error) {
	err = s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		st, before, err := mapStory(ctx, tx, mapID, storyID)
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
		if err := tx.QueryRow(ctx, `UPDATE stories SET version = version + 1, updated_at = now() WHERE id = $1 RETURNING version`,
			storyID).Scan(&version); err != nil {
			return nil, err
		}
		c, _ := updated(mapID, "placement", p.ID, "move", before, p)
		// A move changes the story's version even when it lands where it was
		// (the same rank); live clients need the new version either way.
		if c.before == nil {
			c.before, c.after = map[string]any{}, map[string]any{}
		}
		c.before["storyVersion"], c.after["storyVersion"] = st.Version, version
		return []change{c}, nil
	})
	return p, stale, err
}

func (s *Store) DeleteStory(ctx context.Context, actor, workspaceID, mapID, storyID uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		st, _, err := mapStory(ctx, tx, mapID, storyID)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE stories SET deleted_at = now(), updated_at = now() WHERE id = $1`, storyID); err != nil {
			return nil, err
		}
		return []change{gone(mapID, "story", storyID, "delete", map[string]any{"key": st.Key, "title": st.Title})}, nil
	})
}

// RestoreStory undoes DeleteStory. The story's activity must not be deleted.
func (s *Store) RestoreStory(ctx context.Context, actor, workspaceID, mapID, storyID uuid.UUID) (Story, error) {
	var st Story
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		var activityID uuid.UUID
		err := tx.QueryRow(ctx,
			`SELECT p.activity_id FROM stories s JOIN placements p ON p.story_id = s.id
			 WHERE s.id = $1 AND p.map_id = $2 AND s.workspace_id = $3 AND s.deleted_at IS NOT NULL`,
			storyID, mapID, workspaceID).Scan(&activityID)
		if err != nil {
			return nil, notFound(err)
		}
		if _, err := liveNode(ctx, tx, mapID, activityID); err != nil {
			return nil, fmt.Errorf("%w: the story's activity is deleted; restore it first", ErrInvalid)
		}
		if st, err = scanStory(tx.QueryRow(ctx,
			`UPDATE stories s SET deleted_at = NULL, updated_at = now() WHERE id = $1 RETURNING `+storyCols, storyID)); err != nil {
			return nil, err
		}
		return []change{gone(mapID, "story", storyID, "restore", map[string]any{"key": st.Key, "title": st.Title})}, nil
	})
	return st, err
}
