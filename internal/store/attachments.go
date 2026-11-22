package store

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Attachment is a file's metadata. The bytes are read only by AttachmentFile.
type Attachment struct {
	ID           uuid.UUID  `json:"id"`
	StepID       *uuid.UUID `json:"stepId"`
	Name         string     `json:"name"`
	ContentType  string     `json:"contentType"`
	Size         int64      `json:"size"`
	UploadedBy   *uuid.UUID `json:"uploadedBy"`
	UploaderName string     `json:"uploaderName"` // "" when that account no longer exists
	CreatedAt    time.Time  `json:"createdAt"`
	SHA256       []byte     `json:"-"`
}

const attachmentCols = `a.id, a.step_id, a.name, a.content_type, a.size_bytes, a.uploaded_by, coalesce(u.display_name, ''), a.created_at, a.sha256`

const attachmentFrom = `FROM attachments a LEFT JOIN users u ON u.id = a.uploaded_by`

func scanAttachment(row pgx.Row) (Attachment, error) {
	var a Attachment
	err := row.Scan(&a.ID, &a.StepID, &a.Name, &a.ContentType, &a.Size, &a.UploadedBy, &a.UploaderName, &a.CreatedAt, &a.SHA256)
	return a, notFound(err)
}

// attachmentEvent is what history, live clients and webhooks get: the
// metadata, never the bytes.
func attachmentEvent(a Attachment) map[string]any {
	return map[string]any{"stepId": a.StepID, "name": a.Name, "contentType": a.ContentType, "size": a.Size}
}

// FileUpload is a file as the handler read it; ContentType is already sniffed.
type FileUpload struct {
	Name, ContentType string
	Data              []byte
}

// AddStepAttachment stores a file on a step shown on the map.
func (s *Store) AddStepAttachment(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID, f FileUpload) (Attachment, error) {
	var a Attachment
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if _, _, err := mapStep(ctx, tx, mapID, stepID); err != nil {
			return nil, err
		}
		id := newID()
		sum := sha256.Sum256(f.Data)
		if _, err := tx.Exec(ctx,
			`INSERT INTO attachments (id, workspace_id, step_id, name, content_type, size_bytes, sha256, uploaded_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, workspaceID, stepID, f.Name, f.ContentType, len(f.Data), sum[:], actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO attachment_data (attachment_id, data) VALUES ($1, $2)`, id, f.Data); err != nil {
			return nil, err
		}
		var err error
		if a, err = scanAttachment(tx.QueryRow(ctx, `SELECT `+attachmentCols+` `+attachmentFrom+` WHERE a.id = $1`, id)); err != nil {
			return nil, err
		}
		return echoes(ctx, tx, stepID, change{mapID: mapID, entity: "attachment", id: id, action: "create", after: attachmentEvent(a)})
	})
	return a, err
}

// StepAttachments lists a step's live files, oldest first.
func (s *Store) StepAttachments(ctx context.Context, workspaceID, mapID, stepID uuid.UUID) ([]Attachment, error) {
	var out []Attachment
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
			return err
		}
		if _, _, err := mapStep(ctx, tx, mapID, stepID); err != nil {
			return err
		}
		var err error
		out, err = collect(ctx, tx, scanAttachment,
			`SELECT `+attachmentCols+` `+attachmentFrom+` WHERE a.step_id = $1 AND a.deleted_at IS NULL ORDER BY a.created_at, a.id`, stepID)
		return err
	})
	return out, err
}

// liveAttachment is a file whose owner is live: a step on a live map. Files
// of deleted steps are hidden with them.
const liveAttachment = `a.deleted_at IS NULL AND a.workspace_id = $1 AND EXISTS (
	SELECT 1 FROM steps s JOIN placements p ON p.step_id = s.id
	JOIN maps m ON m.id = p.map_id AND m.deleted_at IS NULL
	JOIN nodes n ON n.id = p.activity_id AND n.deleted_at IS NULL
	WHERE s.id = a.step_id AND s.deleted_at IS NULL)`

// AttachmentFile returns a live file of the workspace with its bytes.
func (s *Store) AttachmentFile(ctx context.Context, workspaceID, id uuid.UUID) (Attachment, []byte, error) {
	var a Attachment
	var data []byte
	err := s.pool.QueryRow(ctx,
		`SELECT `+attachmentCols+`, d.data `+attachmentFrom+` JOIN attachment_data d ON d.attachment_id = a.id
		 WHERE a.id = $2 AND `+liveAttachment, workspaceID, id).
		Scan(&a.ID, &a.StepID, &a.Name, &a.ContentType, &a.Size, &a.UploadedBy, &a.UploaderName, &a.CreatedAt, &a.SHA256, &data)
	return a, data, notFound(err)
}

// AttachmentETag returns a live file's sha256 without reading its bytes, so a
// cached copy can be confirmed cheaply.
func (s *Store) AttachmentETag(ctx context.Context, workspaceID, id uuid.UUID) ([]byte, error) {
	var sum []byte
	err := s.pool.QueryRow(ctx, `SELECT a.sha256 FROM attachments a WHERE a.id = $2 AND `+liveAttachment, workspaceID, id).Scan(&sum)
	return sum, notFound(err)
}

// DeleteAttachment soft-deletes a file of a step shown on the map.
func (s *Store) DeleteAttachment(ctx context.Context, actor, workspaceID, mapID, id uuid.UUID) error {
	return s.setAttachmentDeleted(ctx, actor, workspaceID, mapID, id, "delete")
}

// RestoreAttachment undoes DeleteAttachment.
func (s *Store) RestoreAttachment(ctx context.Context, actor, workspaceID, mapID, id uuid.UUID) error {
	return s.setAttachmentDeleted(ctx, actor, workspaceID, mapID, id, "restore")
}

func (s *Store) setAttachmentDeleted(ctx context.Context, actor, workspaceID, mapID, id uuid.UUID, action string) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		deleted := action == "restore" // the state it must be in now
		a, err := scanAttachment(tx.QueryRow(ctx,
			`SELECT `+attachmentCols+` `+attachmentFrom+`
			 WHERE a.id = $1 AND a.workspace_id = $2 AND (a.deleted_at IS NOT NULL) = $3 FOR UPDATE OF a`,
			id, workspaceID, deleted))
		if err != nil {
			return nil, err
		}
		if a.StepID == nil {
			return nil, ErrNotFound
		}
		if _, _, err := mapStep(ctx, tx, mapID, *a.StepID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE attachments SET deleted_at = CASE WHEN $2 THEN NULL ELSE now() END WHERE id = $1`, id, deleted); err != nil {
			return nil, err
		}
		return echoes(ctx, tx, *a.StepID, gone(mapID, "attachment", id, action, attachmentEvent(a)))
	})
}

// sharedMapOf resolves a live, unexpired, unrevoked share link to its map
// without recording a use: one page view fetches many files.
func sharedMapOf(ctx context.Context, q pgx.Tx, tokenHash []byte) (uuid.UUID, error) {
	var mapID uuid.UUID
	err := q.QueryRow(ctx,
		`SELECT m.id FROM share_links l JOIN maps m ON m.id = l.map_id AND m.deleted_at IS NULL
		 WHERE l.token_hash = $1 AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())`,
		tokenHash).Scan(&mapID)
	return mapID, notFound(err)
}

// onSharedMap is a live file of a live step placed on map $1, so a share link
// reaches only the files of its own map.
const onSharedMap = `a.deleted_at IS NULL AND EXISTS (
	SELECT 1 FROM steps s JOIN placements p ON p.step_id = s.id AND p.map_id = $1
	JOIN nodes n ON n.id = p.activity_id AND n.deleted_at IS NULL
	WHERE s.id = a.step_id AND s.deleted_at IS NULL)`

// forShare leaves out who uploaded a file; share links show no people.
func forShare(a Attachment) Attachment {
	a.UploadedBy, a.UploaderName = nil, ""
	return a
}

// SharedStepAttachments lists the files of a step on a share link's map.
func (s *Store) SharedStepAttachments(ctx context.Context, tokenHash []byte, stepID uuid.UUID) ([]Attachment, error) {
	var out []Attachment
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		mapID, err := sharedMapOf(ctx, tx, tokenHash)
		if err != nil {
			return err
		}
		if _, _, err := mapStep(ctx, tx, mapID, stepID); err != nil {
			return err
		}
		out, err = collect(ctx, tx, scanAttachment,
			`SELECT `+attachmentCols+` `+attachmentFrom+` WHERE a.step_id = $2 AND `+onSharedMap+` ORDER BY a.created_at, a.id`, mapID, stepID)
		for i := range out {
			out[i] = forShare(out[i])
		}
		return err
	})
	return out, err
}

// SharedAttachmentFile returns a file, with its bytes, of a step on a share
// link's map.
func (s *Store) SharedAttachmentFile(ctx context.Context, tokenHash []byte, id uuid.UUID) (Attachment, []byte, error) {
	var a Attachment
	var data []byte
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		mapID, err := sharedMapOf(ctx, tx, tokenHash)
		if err != nil {
			return err
		}
		return notFound(tx.QueryRow(ctx,
			`SELECT `+attachmentCols+`, d.data `+attachmentFrom+` JOIN attachment_data d ON d.attachment_id = a.id
			 WHERE a.id = $2 AND `+onSharedMap, mapID, id).
			Scan(&a.ID, &a.StepID, &a.Name, &a.ContentType, &a.Size, &a.UploadedBy, &a.UploaderName, &a.CreatedAt, &a.SHA256, &data))
	})
	return forShare(a), data, err
}

// SharedAttachmentETag is AttachmentETag for a share link.
func (s *Store) SharedAttachmentETag(ctx context.Context, tokenHash []byte, id uuid.UUID) ([]byte, error) {
	var sum []byte
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		mapID, err := sharedMapOf(ctx, tx, tokenHash)
		if err != nil {
			return err
		}
		return notFound(tx.QueryRow(ctx, `SELECT a.sha256 FROM attachments a WHERE a.id = $2 AND `+onSharedMap, mapID, id).Scan(&sum))
	})
	return sum, err
}
