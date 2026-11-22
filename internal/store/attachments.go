package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Attachment is a file's metadata. The bytes are read only by AttachmentFile.
type Attachment struct {
	ID           uuid.UUID  `json:"id"`
	StepID       *uuid.UUID `json:"stepId"`    // a step's file
	NodeID       *uuid.UUID `json:"nodeId"`    // an objective's or activity's file
	CommentID    *uuid.UUID `json:"commentId"` // a comment's file; all three are null for a draft
	Name         string     `json:"name"`
	ContentType  string     `json:"contentType"`
	Size         int64      `json:"size"`
	UploadedBy   *uuid.UUID `json:"uploadedBy"`
	UploaderName string     `json:"uploaderName"` // "" when that account no longer exists
	CreatedAt    time.Time  `json:"createdAt"`
	SHA256       []byte     `json:"-"`
}

const attachmentCols = `a.id, a.step_id, a.node_id, a.comment_id, a.name, a.content_type, a.size_bytes, a.uploaded_by, coalesce(u.display_name, ''), a.created_at, a.sha256`

const attachmentFrom = `FROM attachments a LEFT JOIN users u ON u.id = a.uploaded_by`

func scanAttachment(row pgx.Row) (Attachment, error) { return scanAttachmentAnd(row) }

// scanAttachmentAnd scans attachmentCols and then more columns into more.
func scanAttachmentAnd(row pgx.Row, more ...any) (Attachment, error) {
	var a Attachment
	err := row.Scan(append([]any{&a.ID, &a.StepID, &a.NodeID, &a.CommentID, &a.Name, &a.ContentType, &a.Size, &a.UploadedBy, &a.UploaderName, &a.CreatedAt, &a.SHA256}, more...)...)
	return a, notFound(err)
}

// attachmentEvent is what history, live clients and webhooks get: the
// metadata, never the bytes.
func attachmentEvent(a Attachment) map[string]any {
	ev := map[string]any{"name": a.Name, "contentType": a.ContentType, "size": a.Size}
	if a.NodeID != nil {
		ev["nodeId"] = a.NodeID
	} else {
		ev["stepId"] = a.StepID
	}
	return ev
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
		var err error
		if a, err = insertAttachment(ctx, tx, actor, workspaceID, "step_id", stepID, f); err != nil {
			return nil, err
		}
		return echoes(ctx, tx, stepID, change{mapID: mapID, entity: "attachment", id: a.ID, action: "create", after: attachmentEvent(a)})
	})
	return a, err
}

// AddDraftAttachment stores a file for a comment being written on a step
// shown on the map. Only its uploader sees it until a comment claims it, and
// it makes no event: nobody else can tell it exists.
func (s *Store) AddDraftAttachment(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID, f FileUpload) (Attachment, error) {
	var a Attachment
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
			return err
		}
		if _, _, err := mapStep(ctx, tx, mapID, stepID); err != nil {
			return err
		}
		var err error
		a, err = insertAttachment(ctx, tx, actor, workspaceID, "draft_step_id", stepID, f)
		return err
	})
	return a, err
}

// AddNodeAttachment stores a file on an objective or activity of the map.
func (s *Store) AddNodeAttachment(ctx context.Context, actor, workspaceID, mapID, nodeID uuid.UUID, f FileUpload) (Attachment, error) {
	var a Attachment
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if _, err := liveNode(ctx, tx, mapID, nodeID); err != nil {
			return nil, err
		}
		var err error
		if a, err = insertAttachment(ctx, tx, actor, workspaceID, "node_id", nodeID, f); err != nil {
			return nil, err
		}
		return []change{{mapID: mapID, entity: "attachment", id: a.ID, action: "create", after: attachmentEvent(a)}}, nil
	})
	return a, err
}

// NodeAttachments lists the live files of an objective or activity of the map, oldest first.
func (s *Store) NodeAttachments(ctx context.Context, workspaceID, mapID, nodeID uuid.UUID) ([]Attachment, error) {
	var out []Attachment
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
			return err
		}
		if _, err := liveNode(ctx, tx, mapID, nodeID); err != nil {
			return err
		}
		var err error
		out, err = collect(ctx, tx, scanAttachment,
			`SELECT `+attachmentCols+` `+attachmentFrom+` WHERE a.node_id = $1 AND a.deleted_at IS NULL ORDER BY a.created_at, a.id`, nodeID)
		return err
	})
	return out, err
}

// insertAttachment stores a file and its bytes, owned through ownerCol.
func insertAttachment(ctx context.Context, tx pgx.Tx, actor, workspaceID uuid.UUID, ownerCol string, owner uuid.UUID, f FileUpload) (Attachment, error) {
	id := newID()
	sum := sha256.Sum256(f.Data)
	if _, err := tx.Exec(ctx,
		`INSERT INTO attachments (id, workspace_id, `+ownerCol+`, name, content_type, size_bytes, sha256, uploaded_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, workspaceID, owner, f.Name, f.ContentType, len(f.Data), sum[:], actor); err != nil {
		return Attachment{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO attachment_data (attachment_id, data) VALUES ($1, $2)`, id, f.Data); err != nil {
		return Attachment{}, err
	}
	return scanAttachment(tx.QueryRow(ctx, `SELECT `+attachmentCols+` `+attachmentFrom+` WHERE a.id = $1`, id))
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

// liveAttachment is a file of workspace $1 that user $3 can see: a draft of
// their own, or a file whose owner is live (a step on a live map, a live
// comment on one, or a live node of a live map). Files of deleted steps and comments are hidden with them.
const liveAttachment = `a.deleted_at IS NULL AND a.workspace_id = $1 AND (
	(a.draft_step_id IS NOT NULL AND a.uploaded_by = $3) OR EXISTS (
	SELECT 1 FROM steps s JOIN placements p ON p.step_id = s.id
	JOIN maps m ON m.id = p.map_id AND m.deleted_at IS NULL
	JOIN nodes n ON n.id = p.activity_id AND n.deleted_at IS NULL
	WHERE s.deleted_at IS NULL AND s.id = coalesce(a.step_id,
		(SELECT c.step_id FROM comments c WHERE c.id = a.comment_id AND c.deleted_at IS NULL)))
	OR EXISTS (SELECT 1 FROM nodes n JOIN maps m ON m.id = n.map_id AND m.deleted_at IS NULL
		WHERE n.id = a.node_id AND n.deleted_at IS NULL))`

// AttachmentFile returns a file of the workspace, with its bytes, that viewer can see.
func (s *Store) AttachmentFile(ctx context.Context, workspaceID, viewer, id uuid.UUID) (Attachment, []byte, error) {
	var data []byte
	a, err := scanAttachmentAnd(s.pool.QueryRow(ctx,
		`SELECT `+attachmentCols+`, d.data `+attachmentFrom+` JOIN attachment_data d ON d.attachment_id = a.id
		 WHERE a.id = $2 AND `+liveAttachment, workspaceID, id, viewer), &data)
	return a, data, err
}

// AttachmentETag returns the sha256 of a file viewer can see without reading
// its bytes, so a cached copy can be confirmed cheaply.
func (s *Store) AttachmentETag(ctx context.Context, workspaceID, viewer, id uuid.UUID) ([]byte, error) {
	var sum []byte
	err := s.pool.QueryRow(ctx, `SELECT a.sha256 FROM attachments a WHERE a.id = $2 AND `+liveAttachment, workspaceID, id, viewer).Scan(&sum)
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
		switch {
		case a.StepID != nil:
			_, _, err = mapStep(ctx, tx, mapID, *a.StepID)
		case a.NodeID != nil:
			_, err = liveNode(ctx, tx, mapID, *a.NodeID)
		default:
			err = ErrNotFound // a comment's file goes with the comment's edit
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE attachments SET deleted_at = CASE WHEN $2 THEN NULL ELSE now() END WHERE id = $1`, id, deleted); err != nil {
			return nil, err
		}
		c := gone(mapID, "attachment", id, action, attachmentEvent(a))
		if a.NodeID != nil {
			return []change{c}, nil
		}
		return echoes(ctx, tx, *a.StepID, c)
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

// onSharedMap is a live file of a live step placed on map $1, or of a live
// node of it, so a share link reaches only the files of its own map.
const onSharedMap = `a.deleted_at IS NULL AND (EXISTS (
	SELECT 1 FROM steps s JOIN placements p ON p.step_id = s.id AND p.map_id = $1
	JOIN nodes n ON n.id = p.activity_id AND n.deleted_at IS NULL
	WHERE s.id = a.step_id AND s.deleted_at IS NULL)
	OR EXISTS (SELECT 1 FROM nodes n WHERE n.id = a.node_id AND n.map_id = $1 AND n.deleted_at IS NULL))`

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

// SharedNodeAttachments lists the files of an objective or activity of a share link's map.
func (s *Store) SharedNodeAttachments(ctx context.Context, tokenHash []byte, nodeID uuid.UUID) ([]Attachment, error) {
	var out []Attachment
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		mapID, err := sharedMapOf(ctx, tx, tokenHash)
		if err != nil {
			return err
		}
		if _, err := liveNode(ctx, tx, mapID, nodeID); err != nil {
			return err
		}
		out, err = collect(ctx, tx, scanAttachment,
			`SELECT `+attachmentCols+` `+attachmentFrom+` WHERE a.node_id = $2 AND `+onSharedMap+` ORDER BY a.created_at, a.id`, mapID, nodeID)
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
		a, err = scanAttachmentAnd(tx.QueryRow(ctx,
			`SELECT `+attachmentCols+`, d.data `+attachmentFrom+` JOIN attachment_data d ON d.attachment_id = a.id
			 WHERE a.id = $2 AND `+onSharedMap, mapID, id), &data)
		return err
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

// claimDrafts makes actor's drafts on stepID, listed in ids, files of
// commentID. Each id must be such a draft.
func claimDrafts(ctx context.Context, tx pgx.Tx, actor, stepID, commentID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx,
		`UPDATE attachments SET comment_id = $1, draft_step_id = NULL
		 WHERE id = ANY($2) AND draft_step_id = $3 AND uploaded_by = $4 AND deleted_at IS NULL`,
		commentID, ids, stepID, actor)
	if err != nil {
		return err
	}
	if int(tag.RowsAffected()) != len(ids) {
		return fmt.Errorf("%w: attachmentIds must be files you added for a comment on this step", ErrInvalid)
	}
	return nil
}

// commentFiles loads the live files of the given comments, oldest first.
func commentFiles(ctx context.Context, q querier, cs []Comment) error {
	ids := make([]uuid.UUID, 0, len(cs))
	at := map[uuid.UUID]*Comment{}
	for i := range cs {
		cs[i].Attachments = []Attachment{}
		if !cs[i].Deleted {
			ids = append(ids, cs[i].ID)
			at[cs[i].ID] = &cs[i]
		}
	}
	files, err := collect(ctx, q, scanAttachment,
		`SELECT `+attachmentCols+` `+attachmentFrom+` WHERE a.comment_id = ANY($1) AND a.deleted_at IS NULL ORDER BY a.created_at, a.id`, ids)
	for _, f := range files {
		c := at[*f.CommentID]
		c.Attachments = append(c.Attachments, f)
	}
	return err
}

// Sweep limits: drafts nobody posted, and deleted files past undo.
const (
	DraftLifetime  = 24 * time.Hour
	DeletedPurge   = 30 * 24 * time.Hour
	sweepBatchSize = 200
)

// SweepAttachments removes, for good, drafts older than DraftLifetime and
// files deleted (or whose comment was deleted) more than DeletedPurge before
// now. Several sweepers can run at once: each batch locks its rows with SKIP
// LOCKED, so every file is removed by exactly one. It returns how many it removed.
func (s *Store) SweepAttachments(ctx context.Context, now time.Time) (int, error) {
	total := 0
	for {
		tag, err := s.pool.Exec(ctx,
			`DELETE FROM attachments WHERE id IN (
			   SELECT a.id FROM attachments a
			   WHERE (a.draft_step_id IS NOT NULL AND a.created_at < $1)
			      OR a.deleted_at < $2
			      OR a.comment_id IN (SELECT c.id FROM comments c WHERE c.deleted_at < $2)
			   LIMIT $3 FOR UPDATE SKIP LOCKED)`,
			now.Add(-DraftLifetime), now.Add(-DeletedPurge), sweepBatchSize)
		if err != nil {
			return total, err
		}
		total += int(tag.RowsAffected())
		if tag.RowsAffected() < sweepBatchSize {
			return total, nil
		}
	}
}

// RunSweeper calls SweepAttachments every interval until ctx ends.
func (s *Store) RunSweeper(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := s.SweepAttachments(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Error("sweep attachments", "err", err)
		} else if n > 0 {
			slog.Info("swept attachments", "removed", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
