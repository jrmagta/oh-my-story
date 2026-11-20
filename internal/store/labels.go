package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Label tags steps across every map of a workspace.
type Label struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Colour string    `json:"colour"`
}

const labelCols = `id, name, colour`

func scanLabel(row pgx.Row) (Label, error) {
	var l Label
	err := row.Scan(&l.ID, &l.Name, &l.Colour)
	return l, notFound(err)
}

// Labels lists the workspace's labels by name.
func (s *Store) Labels(ctx context.Context, workspaceID uuid.UUID) ([]Label, error) {
	return collect(ctx, s.pool, scanLabel,
		`SELECT `+labelCols+` FROM labels WHERE workspace_id = $1 ORDER BY name, id`, workspaceID)
}

// CreateLabel returns ErrConflict when the workspace has a label of that name,
// ignoring case.
func (s *Store) CreateLabel(ctx context.Context, actor, workspaceID uuid.UUID, name, colour string) (Label, error) {
	l := Label{ID: newID(), Name: name, Colour: colour}
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO labels (id, workspace_id, name, colour) VALUES ($1, $2, $3, $4)`,
			l.ID, workspaceID, l.Name, l.Colour); err != nil {
			return nil, err
		}
		return []change{created(uuid.Nil, "label", l.ID, l)}, nil
	})
	return l, err
}

type LabelPatch struct {
	Name, Colour *string
}

func (s *Store) UpdateLabel(ctx context.Context, actor, workspaceID, labelID uuid.UUID, p LabelPatch) (Label, error) {
	var l Label
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		before, err := scanLabel(tx.QueryRow(ctx,
			`SELECT `+labelCols+` FROM labels WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, labelID, workspaceID))
		if err != nil {
			return nil, err
		}
		if l, err = scanLabel(tx.QueryRow(ctx,
			`UPDATE labels SET name = COALESCE($2, name), colour = COALESCE($3, colour), updated_at = now()
			 WHERE id = $1 RETURNING `+labelCols, labelID, p.Name, p.Colour)); err != nil {
			return nil, err
		}
		if c, ok := updated(uuid.Nil, "label", labelID, "update", before, l); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return l, err
}

// DeleteLabel removes a label from the workspace and from every step that has
// it. The one label event says so; clients drop the id from their steps.
func (s *Store) DeleteLabel(ctx context.Context, actor, workspaceID, labelID uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		var name string
		err := tx.QueryRow(ctx, `DELETE FROM labels WHERE id = $1 AND workspace_id = $2 RETURNING name`, labelID, workspaceID).
			Scan(&name)
		if err != nil {
			return nil, notFound(err)
		}
		return []change{gone(uuid.Nil, "label", labelID, "delete", map[string]any{"name": name})}, nil
	})
}
