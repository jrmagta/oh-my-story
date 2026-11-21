package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SavedView is a named map filter. Only its creator sees it until it is
// shared with the workspace.
type SavedView struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Filter      json.RawMessage `json:"filter"`
	Shared      bool            `json:"shared"`
	CreatedBy   uuid.UUID       `json:"createdBy"`
	CreatorName string          `json:"creatorName"`
}

const viewCols = `v.id, v.name, v.filter, v.is_shared, v.created_by, coalesce(u.display_name, '')`

func scanView(row pgx.Row) (SavedView, error) {
	var v SavedView
	err := row.Scan(&v.ID, &v.Name, &v.Filter, &v.Shared, &v.CreatedBy, &v.CreatorName)
	return v, notFound(err)
}

// The views of a live map in the workspace that user may see: shared ones and
// their own.
const visibleViews = `FROM saved_views v JOIN maps m ON m.id = v.map_id LEFT JOIN users u ON u.id = v.created_by
	WHERE m.workspace_id = $1 AND m.id = $2 AND m.deleted_at IS NULL AND (v.is_shared OR v.created_by = $3)`

// SavedViews lists the map's shared views and the user's own, by name.
func (s *Store) SavedViews(ctx context.Context, workspaceID, mapID, user uuid.UUID) ([]SavedView, error) {
	if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
		return nil, err
	}
	return collect(ctx, s.pool, scanView, `SELECT `+viewCols+` `+visibleViews+` ORDER BY lower(v.name), v.id`, workspaceID, mapID, user)
}

func (s *Store) CreateSavedView(ctx context.Context, workspaceID, mapID, user uuid.UUID, name string, filter json.RawMessage, shared bool) (SavedView, error) {
	if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
		return SavedView{}, err
	}
	id := newID()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO saved_views (id, map_id, created_by, name, filter, is_shared) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, mapID, user, name, filter, shared); err != nil {
		return SavedView{}, err
	}
	return s.savedView(ctx, workspaceID, mapID, user, id)
}

func (s *Store) savedView(ctx context.Context, workspaceID, mapID, user, id uuid.UUID) (SavedView, error) {
	return scanView(s.pool.QueryRow(ctx, `SELECT `+viewCols+` `+visibleViews+` AND v.id = $4`, workspaceID, mapID, user, id))
}

type SavedViewPatch struct {
	Name   *string
	Filter json.RawMessage // nil leaves it
	Shared *bool
}

// UpdateSavedView changes a view the user created. Anyone else's, shared or
// not, is ErrNotFound for this.
func (s *Store) UpdateSavedView(ctx context.Context, workspaceID, mapID, user, id uuid.UUID, p SavedViewPatch) (SavedView, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE saved_views v SET name = COALESCE($5, v.name), filter = COALESCE($6, v.filter),
		   is_shared = COALESCE($7, v.is_shared), updated_at = now()
		 FROM maps m WHERE m.id = v.map_id AND m.workspace_id = $1 AND m.id = $2 AND m.deleted_at IS NULL
		   AND v.created_by = $3 AND v.id = $4`,
		workspaceID, mapID, user, id, p.Name, p.Filter, p.Shared)
	if err != nil {
		return SavedView{}, err
	}
	if tag.RowsAffected() == 0 {
		return SavedView{}, ErrNotFound
	}
	return s.savedView(ctx, workspaceID, mapID, user, id)
}

// DeleteSavedView removes the user's own view, or any shared view when
// anyShared is true (admins).
func (s *Store) DeleteSavedView(ctx context.Context, workspaceID, mapID, user, id uuid.UUID, anyShared bool) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM saved_views v USING maps m
		 WHERE m.id = v.map_id AND m.workspace_id = $1 AND m.id = $2 AND v.id = $4
		   AND (v.created_by = $3 OR ($5 AND v.is_shared))`,
		workspaceID, mapID, user, id, anyShared)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
