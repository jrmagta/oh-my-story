package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Workflow struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"isDefault"`
	Statuses  []Status  `json:"statuses,omitempty"`
}

type Status struct {
	ID         uuid.UUID `json:"id"`
	WorkflowID uuid.UUID `json:"workflowId"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	Colour     *string   `json:"colour"`
	Rank       string    `json:"rank"`
}

const statusCols = `id, workflow_id, name, category, colour, rank`

func scanStatus(row pgx.Row) (Status, error) {
	var st Status
	err := row.Scan(&st.ID, &st.WorkflowID, &st.Name, &st.Category, &st.Colour, &st.Rank)
	return st, notFound(err)
}

// Workflows lists the workspace's workflows with their statuses in order.
func (s *Store) Workflows(ctx context.Context, workspaceID uuid.UUID) ([]Workflow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, is_default FROM workflows WHERE workspace_id = $1 ORDER BY is_default DESC, name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Workflow, error) {
		var w Workflow
		err := r.Scan(&w.ID, &w.Name, &w.IsDefault)
		return w, err
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Statuses, err = listStatuses(ctx, s.pool, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func listStatuses(ctx context.Context, q querier, workflowID uuid.UUID) ([]Status, error) {
	rows, err := q.Query(ctx, `SELECT `+statusCols+` FROM statuses WHERE workflow_id = $1 ORDER BY rank`, workflowID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Status, error) { return scanStatus(r) })
}

// lockWorkflow serialises writes to one workflow's statuses.
func lockWorkflow(ctx context.Context, tx pgx.Tx, workspaceID, workflowID uuid.UUID) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT true FROM workflows WHERE id = $1 AND workspace_id = $2 FOR UPDATE`,
		workflowID, workspaceID).Scan(&ok)
	return notFound(err)
}

// CreateWorkflow makes a workflow that starts with the standard four statuses.
func (s *Store) CreateWorkflow(ctx context.Context, actor, workspaceID uuid.UUID, name string) (Workflow, error) {
	var w Workflow
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		if err := tx.QueryRow(ctx, `SELECT oms_seed_workflow($1, $2, false)`, workspaceID, name).Scan(&w.ID); err != nil {
			return nil, err
		}
		w.Name = name
		var err error
		if w.Statuses, err = listStatuses(ctx, tx, w.ID); err != nil {
			return nil, err
		}
		changes := []change{created(uuid.Nil, "workflow", w.ID, Workflow{ID: w.ID, Name: w.Name})}
		for _, st := range w.Statuses {
			changes = append(changes, created(uuid.Nil, "status", st.ID, st))
		}
		return changes, nil
	})
	return w, err
}

func (s *Store) RenameWorkflow(ctx context.Context, actor, workspaceID, workflowID uuid.UUID, name string) (Workflow, error) {
	var w Workflow
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		var before Workflow
		err := tx.QueryRow(ctx, `SELECT id, name, is_default FROM workflows WHERE id = $1 AND workspace_id = $2 FOR UPDATE`,
			workflowID, workspaceID).Scan(&before.ID, &before.Name, &before.IsDefault)
		if err != nil {
			return nil, notFound(err)
		}
		w = before
		w.Name = name
		if _, err := tx.Exec(ctx, `UPDATE workflows SET name = $2, updated_at = now() WHERE id = $1`, workflowID, name); err != nil {
			return nil, err
		}
		if c, ok := updated(uuid.Nil, "workflow", w.ID, "update", before, w); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return w, err
}

type StatusInput struct {
	Name     string
	Category string
	Colour   *string
}

func (s *Store) CreateStatus(ctx context.Context, actor, workspaceID, workflowID uuid.UUID, in StatusInput, pos Position) (Status, error) {
	st := Status{ID: newID(), WorkflowID: workflowID, Name: in.Name, Category: in.Category, Colour: in.Colour}
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		if err := lockWorkflow(ctx, tx, workspaceID, workflowID); err != nil {
			return nil, err
		}
		var err error
		if st.Rank, err = statusSiblings(workflowID).rank(ctx, tx, pos, st.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO statuses (id, workflow_id, name, category, colour, rank) VALUES ($1, $2, $3, $4, $5, $6)`,
			st.ID, st.WorkflowID, st.Name, st.Category, st.Colour, st.Rank); err != nil {
			return nil, err
		}
		return []change{created(uuid.Nil, "status", st.ID, st)}, nil
	})
	return st, err
}

func statusSiblings(workflowID uuid.UUID) siblings {
	return siblings{table: "statuses", where: "workflow_id = $1", args: []any{workflowID}}
}

// statusForUpdate loads a status of the workspace and locks its workflow.
func statusForUpdate(ctx context.Context, tx pgx.Tx, workspaceID, statusID uuid.UUID) (Status, error) {
	st, err := scanStatus(tx.QueryRow(ctx,
		`SELECT s.id, s.workflow_id, s.name, s.category, s.colour, s.rank
		 FROM statuses s JOIN workflows w ON w.id = s.workflow_id WHERE s.id = $1 AND w.workspace_id = $2`,
		statusID, workspaceID))
	if err != nil {
		return st, err
	}
	return st, lockWorkflow(ctx, tx, workspaceID, st.WorkflowID)
}

type StatusPatch struct {
	Name, Category, Colour *string
}

func (s *Store) UpdateStatus(ctx context.Context, actor, workspaceID, statusID uuid.UUID, p StatusPatch) (Status, error) {
	var st Status
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		before, err := statusForUpdate(ctx, tx, workspaceID, statusID)
		if err != nil {
			return nil, err
		}
		st, err = scanStatus(tx.QueryRow(ctx,
			`UPDATE statuses SET name = COALESCE($2, name), category = COALESCE($3, category),
			   colour = COALESCE($4, colour), updated_at = now()
			 WHERE id = $1 RETURNING `+statusCols, statusID, p.Name, p.Category, p.Colour))
		if err != nil {
			return nil, err
		}
		if c, ok := updated(uuid.Nil, "status", st.ID, "update", before, st); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return st, err
}

func (s *Store) MoveStatus(ctx context.Context, actor, workspaceID, statusID uuid.UUID, pos Position) (Status, error) {
	var st Status
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		before, err := statusForUpdate(ctx, tx, workspaceID, statusID)
		if err != nil {
			return nil, err
		}
		st = before
		if st.Rank, err = statusSiblings(st.WorkflowID).rank(ctx, tx, pos, st.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE statuses SET rank = $2, updated_at = now() WHERE id = $1`, st.ID, st.Rank); err != nil {
			return nil, err
		}
		c, _ := updated(uuid.Nil, "status", st.ID, "move", before, st)
		return []change{c}, nil
	})
	return st, err
}

// DeleteStatus removes a status. Stories in it move to replacement, which must
// be another status of the same workflow; replacement may be nil only when no
// story uses the status. A workflow keeps at least one status.
func (s *Store) DeleteStatus(ctx context.Context, actor, workspaceID, statusID uuid.UUID, replacement *uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		st, err := statusForUpdate(ctx, tx, workspaceID, statusID)
		if err != nil {
			return nil, err
		}
		var siblingsLeft int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM statuses WHERE workflow_id = $1 AND id <> $2`,
			st.WorkflowID, st.ID).Scan(&siblingsLeft); err != nil {
			return nil, err
		}
		if siblingsLeft == 0 {
			return nil, fmt.Errorf("%w: a workflow needs at least one status", ErrInvalid)
		}
		var changes []change
		if replacement == nil {
			var inUse bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stories WHERE status_id = $1)`, st.ID).Scan(&inUse); err != nil {
				return nil, err
			}
			if inUse {
				return nil, fmt.Errorf("%w: stories use this status; choose a replacement", ErrInvalid)
			}
		} else {
			var ok bool
			err := tx.QueryRow(ctx, `SELECT true FROM statuses WHERE id = $1 AND workflow_id = $2 AND id <> $3`,
				*replacement, st.WorkflowID, st.ID).Scan(&ok)
			if err != nil {
				return nil, fmt.Errorf("%w: the replacement must be another status of the same workflow", ErrInvalid)
			}
			rows, err := tx.Query(ctx,
				`UPDATE stories SET status_id = $2, version = version + 1, updated_at = now() WHERE status_id = $1
				 RETURNING id, (SELECT map_id FROM placements p WHERE p.story_id = stories.id LIMIT 1)`,
				st.ID, *replacement)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var id uuid.UUID
				var mapID *uuid.UUID
				if err := rows.Scan(&id, &mapID); err != nil {
					rows.Close()
					return nil, err
				}
				c := change{entity: "story", id: id, action: "update",
					before: map[string]any{"statusId": st.ID.String()}, after: map[string]any{"statusId": replacement.String()}}
				if mapID != nil {
					c.mapID = *mapID
				}
				changes = append(changes, c)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM statuses WHERE id = $1`, st.ID); err != nil {
			return nil, err
		}
		return append(changes, gone(uuid.Nil, "status", st.ID, "delete", map[string]any{"name": st.Name})), nil
	})
}
