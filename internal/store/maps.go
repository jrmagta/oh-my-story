package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Map struct {
	ID          uuid.UUID `json:"id"`
	WorkflowID  uuid.UUID `json:"workflowId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

type Node struct {
	ID          uuid.UUID  `json:"id"`
	ParentID    *uuid.UUID `json:"parentId"`
	Level       string     `json:"level"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Colour      *string    `json:"colour"`
	Rank        string     `json:"rank"`
	Version     int        `json:"version"`
}

type Release struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TargetDate  *string   `json:"targetDate"` // YYYY-MM-DD
	Rank        string    `json:"rank"`
}

// ---- maps ----

const mapCols = `id, workflow_id, name, description`

func scanMap(row pgx.Row) (Map, error) {
	var m Map
	err := row.Scan(&m.ID, &m.WorkflowID, &m.Name, &m.Description)
	return m, notFound(err)
}

func (s *Store) Maps(ctx context.Context, workspaceID uuid.UUID) ([]Map, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+mapCols+` FROM maps WHERE workspace_id = $1 AND deleted_at IS NULL ORDER BY name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Map, error) { return scanMap(r) })
}

type MapInput struct {
	Name, Description string
	WorkflowID        *uuid.UUID // nil: the workspace's default workflow
}

func (s *Store) CreateMap(ctx context.Context, actor, workspaceID uuid.UUID, in MapInput) (Map, error) {
	m := Map{ID: newID(), Name: in.Name, Description: in.Description}
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		err := tx.QueryRow(ctx,
			`SELECT id FROM workflows WHERE workspace_id = $1 AND (id = $2 OR ($2 IS NULL AND is_default))`,
			workspaceID, in.WorkflowID).Scan(&m.WorkflowID)
		if err != nil {
			return nil, fmt.Errorf("%w: workflow not found in this workspace", ErrInvalid)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO maps (id, workspace_id, workflow_id, name, description) VALUES ($1, $2, $3, $4, $5)`,
			m.ID, workspaceID, m.WorkflowID, m.Name, m.Description); err != nil {
			return nil, err
		}
		return []change{created(m.ID, "map", m.ID, m)}, nil
	})
	return m, err
}

type MapPatch struct {
	Name, Description *string
}

func (s *Store) UpdateMap(ctx context.Context, actor, workspaceID, mapID uuid.UUID, p MapPatch) (Map, error) {
	var m Map
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, err := scanMap(tx.QueryRow(ctx, `SELECT `+mapCols+` FROM maps WHERE id = $1`, mapID))
		if err != nil {
			return nil, err
		}
		if m, err = scanMap(tx.QueryRow(ctx,
			`UPDATE maps SET name = COALESCE($2, name), description = COALESCE($3, description), updated_at = now()
			 WHERE id = $1 RETURNING `+mapCols, mapID, p.Name, p.Description)); err != nil {
			return nil, err
		}
		if c, ok := updated(mapID, "map", mapID, "update", before, m); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return m, err
}

func (s *Store) DeleteMap(ctx context.Context, actor, workspaceID, mapID uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		var name string
		if err := tx.QueryRow(ctx, `UPDATE maps SET deleted_at = now() WHERE id = $1 RETURNING name`, mapID).Scan(&name); err != nil {
			return nil, err
		}
		return []change{gone(mapID, "map", mapID, "delete", map[string]any{"name": name})}, nil
	})
}

// ---- backbone nodes ----

const nodeCols = `id, parent_id, level, title, description, colour, rank, version`

func scanNode(row pgx.Row) (Node, error) {
	var n Node
	err := row.Scan(&n.ID, &n.ParentID, &n.Level, &n.Title, &n.Description, &n.Colour, &n.Rank, &n.Version)
	return n, notFound(err)
}

func nodeSiblings(mapID uuid.UUID, parentID *uuid.UUID) siblings {
	return siblings{table: "nodes", where: "map_id = $1 AND parent_id IS NOT DISTINCT FROM $2", args: []any{mapID, parentID}}
}

// liveNode loads a node of the map that is not deleted.
func liveNode(ctx context.Context, tx pgx.Tx, mapID, nodeID uuid.UUID) (Node, error) {
	return scanNode(tx.QueryRow(ctx,
		`SELECT `+nodeCols+` FROM nodes WHERE id = $1 AND map_id = $2 AND deleted_at IS NULL`, nodeID, mapID))
}

func checkParent(ctx context.Context, tx pgx.Tx, mapID uuid.UUID, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	if _, err := liveNode(ctx, tx, mapID, *parentID); err != nil {
		return fmt.Errorf("%w: parent not found on this map", ErrInvalid)
	}
	return nil
}

type NodeInput struct {
	ParentID           *uuid.UUID
	Level              string
	Title, Description string
	Colour             *string
}

// CreateNode adds a backbone node. The database checks that an activity's
// parent is an objective and that objectives have none.
func (s *Store) CreateNode(ctx context.Context, actor, workspaceID, mapID uuid.UUID, in NodeInput, pos Position) (Node, error) {
	n := Node{ID: newID(), ParentID: in.ParentID, Level: in.Level, Title: in.Title, Description: in.Description, Colour: in.Colour}
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if err := checkParent(ctx, tx, mapID, in.ParentID); err != nil {
			return nil, err
		}
		var err error
		if n.Rank, err = nodeSiblings(mapID, in.ParentID).rank(ctx, tx, pos, n.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO nodes (id, map_id, parent_id, level, title, description, colour, rank)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			n.ID, mapID, n.ParentID, n.Level, n.Title, n.Description, n.Colour, n.Rank); err != nil {
			return nil, err
		}
		return []change{created(mapID, "node", n.ID, n)}, nil
	})
	return n, err
}

type NodePatch struct {
	Title, Description *string
	SetColour          bool    // Colour is applied only when set; nil clears it
	Colour             *string // #RRGGBB
}

func (s *Store) UpdateNode(ctx context.Context, actor, workspaceID, mapID, nodeID uuid.UUID, p NodePatch) (Node, error) {
	var n Node
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, err := liveNode(ctx, tx, mapID, nodeID)
		if err != nil {
			return nil, err
		}
		if n, err = scanNode(tx.QueryRow(ctx,
			`UPDATE nodes SET title = COALESCE($2, title), description = COALESCE($3, description),
			   colour = CASE WHEN $4 THEN $5 ELSE colour END, version = version + 1, updated_at = now()
			 WHERE id = $1 RETURNING `+nodeCols, nodeID, p.Title, p.Description, p.SetColour, p.Colour)); err != nil {
			return nil, err
		}
		if c, ok := updated(mapID, "node", nodeID, "update", before, n); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return n, err
}

// MoveNode places a node under parentID (nil for the top level) at pos. A
// node keeps its level, so only a step can change activity, for example.
func (s *Store) MoveNode(ctx context.Context, actor, workspaceID, mapID, nodeID uuid.UUID, parentID *uuid.UUID, pos Position) (Node, error) {
	var n Node
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, err := liveNode(ctx, tx, mapID, nodeID)
		if err != nil {
			return nil, err
		}
		if err := checkParent(ctx, tx, mapID, parentID); err != nil {
			return nil, err
		}
		r, err := nodeSiblings(mapID, parentID).rank(ctx, tx, pos, nodeID)
		if err != nil {
			return nil, err
		}
		if n, err = scanNode(tx.QueryRow(ctx,
			`UPDATE nodes SET parent_id = $2, rank = $3, version = version + 1, updated_at = now()
			 WHERE id = $1 RETURNING `+nodeCols, nodeID, parentID, r)); err != nil {
			return nil, err
		}
		c, _ := updated(mapID, "node", nodeID, "move", before, n)
		return []change{c}, nil
	})
	return n, err
}

// DeleteNode soft-deletes a node, its descendants and the stories placed on
// any of those steps, all with one timestamp so RestoreNode can bring back
// exactly that set.
func (s *Store) DeleteNode(ctx context.Context, actor, workspaceID, mapID, nodeID uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if _, err := liveNode(ctx, tx, mapID, nodeID); err != nil {
			return nil, err
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
			return nil, err
		}
		return setSubtreeDeleted(ctx, tx, mapID, nodeID, nil, &now)
	})
}

// RestoreNode undoes DeleteNode. The node's parent must not be deleted.
func (s *Store) RestoreNode(ctx context.Context, actor, workspaceID, mapID, nodeID uuid.UUID) (Node, error) {
	var n Node
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		var deletedAt *time.Time
		var parentID *uuid.UUID
		err := tx.QueryRow(ctx, `SELECT deleted_at, parent_id FROM nodes WHERE id = $1 AND map_id = $2`, nodeID, mapID).
			Scan(&deletedAt, &parentID)
		if err != nil {
			return nil, notFound(err)
		}
		if deletedAt == nil {
			return nil, fmt.Errorf("%w: node is not deleted", ErrInvalid)
		}
		if err := checkParent(ctx, tx, mapID, parentID); err != nil {
			return nil, err
		}
		changes, err := setSubtreeDeleted(ctx, tx, mapID, nodeID, deletedAt, nil)
		if err != nil {
			return nil, err
		}
		n, err = liveNode(ctx, tx, mapID, nodeID)
		return changes, err
	})
	return n, err
}

// setSubtreeDeleted moves the subtree under nodeID whose deleted_at equals
// from (NULL for live rows) to to, along with the stories placed under its activities.
func setSubtreeDeleted(ctx context.Context, tx pgx.Tx, mapID, nodeID uuid.UUID, from, to *time.Time) ([]change, error) {
	action := "delete"
	if to == nil {
		action = "restore"
	}
	rows, err := tx.Query(ctx,
		`WITH RECURSIVE sub AS (
		   SELECT id FROM nodes WHERE id = $1
		   UNION ALL
		   SELECT n.id FROM nodes n JOIN sub ON n.parent_id = sub.id WHERE n.deleted_at IS NOT DISTINCT FROM $2
		 ),
		 nodes_done AS (
		   UPDATE nodes SET deleted_at = $3, updated_at = now() WHERE id IN (SELECT id FROM sub)
		   RETURNING 'node'::text AS entity, id, jsonb_build_object('title', title, 'level', level) AS label
		 ),
		 stories_done AS (
		   UPDATE stories SET deleted_at = $3, updated_at = now()
		   WHERE deleted_at IS NOT DISTINCT FROM $2
		     AND id IN (SELECT story_id FROM placements WHERE map_id = $4 AND activity_id IN (SELECT id FROM sub))
		   RETURNING 'story'::text AS entity, id, jsonb_build_object('key', key, 'title', title) AS label
		 )
		 SELECT entity, id, label FROM nodes_done UNION ALL SELECT entity, id, label FROM stories_done`,
		nodeID, from, to, mapID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (change, error) {
		var entity string
		var id uuid.UUID
		var label map[string]any
		err := r.Scan(&entity, &id, &label)
		return gone(mapID, entity, id, action, label), err
	})
}

// ---- releases ----

const releaseCols = `id, name, description, target_date::text, rank`

func scanRelease(row pgx.Row) (Release, error) {
	var r Release
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.TargetDate, &r.Rank)
	return r, notFound(err)
}

func releaseSiblings(mapID uuid.UUID) siblings {
	return siblings{table: "releases", where: "map_id = $1", args: []any{mapID}}
}

func mapRelease(ctx context.Context, tx pgx.Tx, mapID, releaseID uuid.UUID) (Release, error) {
	return scanRelease(tx.QueryRow(ctx, `SELECT `+releaseCols+` FROM releases WHERE id = $1 AND map_id = $2`, releaseID, mapID))
}

type ReleaseInput struct {
	Name, Description string
	TargetDate        *string
}

func (s *Store) CreateRelease(ctx context.Context, actor, workspaceID, mapID uuid.UUID, in ReleaseInput, pos Position) (Release, error) {
	var r Release
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		id := newID()
		key, err := releaseSiblings(mapID).rank(ctx, tx, pos, id)
		if err != nil {
			return nil, err
		}
		if r, err = scanRelease(tx.QueryRow(ctx,
			`INSERT INTO releases (id, map_id, name, description, target_date, rank) VALUES ($1, $2, $3, $4, $5::date, $6)
			 RETURNING `+releaseCols, id, mapID, in.Name, in.Description, in.TargetDate, key)); err != nil {
			return nil, err
		}
		return []change{created(mapID, "release", r.ID, r)}, nil
	})
	return r, err
}

type ReleasePatch struct {
	Name, Description *string
	SetTargetDate     bool    // TargetDate is applied only when set; nil clears it
	TargetDate        *string // YYYY-MM-DD
}

func (s *Store) UpdateRelease(ctx context.Context, actor, workspaceID, mapID, releaseID uuid.UUID, p ReleasePatch) (Release, error) {
	var r Release
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, err := mapRelease(ctx, tx, mapID, releaseID)
		if err != nil {
			return nil, err
		}
		if r, err = scanRelease(tx.QueryRow(ctx,
			`UPDATE releases SET name = COALESCE($2, name), description = COALESCE($3, description),
			   target_date = CASE WHEN $4 THEN $5::date ELSE target_date END, updated_at = now()
			 WHERE id = $1 RETURNING `+releaseCols, releaseID, p.Name, p.Description, p.SetTargetDate, p.TargetDate)); err != nil {
			return nil, err
		}
		if c, ok := updated(mapID, "release", releaseID, "update", before, r); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return r, err
}

func (s *Store) MoveRelease(ctx context.Context, actor, workspaceID, mapID, releaseID uuid.UUID, pos Position) (Release, error) {
	var r Release
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, err := mapRelease(ctx, tx, mapID, releaseID)
		if err != nil {
			return nil, err
		}
		key, err := releaseSiblings(mapID).rank(ctx, tx, pos, releaseID)
		if err != nil {
			return nil, err
		}
		if r, err = scanRelease(tx.QueryRow(ctx,
			`UPDATE releases SET rank = $2, updated_at = now() WHERE id = $1 RETURNING `+releaseCols, releaseID, key)); err != nil {
			return nil, err
		}
		c, _ := updated(mapID, "release", releaseID, "move", before, r)
		return []change{c}, nil
	})
	return r, err
}

// DeleteRelease removes a release. Its stories move to the end of the
// Unscheduled cell of their activity, keeping their order.
func (s *Store) DeleteRelease(ctx context.Context, actor, workspaceID, mapID, releaseID uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		rel, err := mapRelease(ctx, tx, mapID, releaseID)
		if err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx,
			`SELECT `+placementCols+` FROM placements WHERE release_id = $1 ORDER BY activity_id, rank`, releaseID)
		if err != nil {
			return nil, err
		}
		moving, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Placement, error) { return scanPlacement(r) })
		if err != nil {
			return nil, err
		}
		var changes []change
		for _, before := range moving {
			p := before
			p.ReleaseID = nil
			if p.Rank, err = cellSiblings(mapID, p.ActivityID, nil).rank(ctx, tx, Position{}, p.ID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE placements SET release_id = NULL, rank = $2 WHERE id = $1`, p.ID, p.Rank); err != nil {
				return nil, err
			}
			c, _ := updated(mapID, "placement", p.ID, "move", before, p)
			changes = append(changes, c)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM releases WHERE id = $1`, releaseID); err != nil {
			return nil, err
		}
		return append(changes, gone(mapID, "release", releaseID, "delete", map[string]any{"name": rel.Name})), nil
	})
}
