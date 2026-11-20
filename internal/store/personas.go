package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Persona is someone a map serves. Personas are shared by the workspace's
// maps and link to objectives.
type Persona struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Colour      string    `json:"colour"`
}

const personaCols = `id, name, description, colour`

func scanPersona(row pgx.Row) (Persona, error) {
	var per Persona
	err := row.Scan(&per.ID, &per.Name, &per.Description, &per.Colour)
	return per, notFound(err)
}

// Personas lists the workspace's personas by name.
func (s *Store) Personas(ctx context.Context, workspaceID uuid.UUID) ([]Persona, error) {
	return collect(ctx, s.pool, scanPersona,
		`SELECT `+personaCols+` FROM personas WHERE workspace_id = $1 ORDER BY name, id`, workspaceID)
}

// CreatePersona returns ErrConflict when the workspace has a persona of that name,
// ignoring case.
func (s *Store) CreatePersona(ctx context.Context, actor, workspaceID uuid.UUID, name, description, colour string) (Persona, error) {
	per := Persona{ID: newID(), Name: name, Description: description, Colour: colour}
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO personas (id, workspace_id, name, description, colour) VALUES ($1, $2, $3, $4, $5)`,
			per.ID, workspaceID, per.Name, per.Description, per.Colour); err != nil {
			return nil, err
		}
		return []change{created(uuid.Nil, "persona", per.ID, per)}, nil
	})
	return per, err
}

type PersonaPatch struct {
	Name, Description, Colour *string
}

func (s *Store) UpdatePersona(ctx context.Context, actor, workspaceID, personaID uuid.UUID, p PersonaPatch) (Persona, error) {
	var per Persona
	err := s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		before, err := scanPersona(tx.QueryRow(ctx,
			`SELECT `+personaCols+` FROM personas WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, personaID, workspaceID))
		if err != nil {
			return nil, err
		}
		if per, err = scanPersona(tx.QueryRow(ctx,
			`UPDATE personas SET name = COALESCE($2, name), description = COALESCE($3, description),
			   colour = COALESCE($4, colour), updated_at = now()
			 WHERE id = $1 RETURNING `+personaCols, personaID, p.Name, p.Description, p.Colour)); err != nil {
			return nil, err
		}
		if c, ok := updated(uuid.Nil, "persona", personaID, "update", before, per); ok {
			return []change{c}, nil
		}
		return nil, nil
	})
	return per, err
}

// DeletePersona removes a persona from the workspace and from every objective
// it is linked to. The one persona event says so; clients drop the id from
// their objectives.
func (s *Store) DeletePersona(ctx context.Context, actor, workspaceID, personaID uuid.UUID) error {
	return s.write(ctx, actor, workspaceID, uuid.Nil, func(tx pgx.Tx) ([]change, error) {
		var name string
		err := tx.QueryRow(ctx, `DELETE FROM personas WHERE id = $1 AND workspace_id = $2 RETURNING name`, personaID, workspaceID).
			Scan(&name)
		if err != nil {
			return nil, notFound(err)
		}
		return []change{gone(uuid.Nil, "persona", personaID, "delete", map[string]any{"name": name})}, nil
	})
}
