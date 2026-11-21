package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Comment is one comment on a step: a thread's first comment (ParentID nil)
// or a reply to one. A deleted comment keeps its place, with no body, while
// it has replies; otherwise it is left out.
type Comment struct {
	ID         uuid.UUID   `json:"id"`
	StepID     uuid.UUID   `json:"stepId"`
	ParentID   *uuid.UUID  `json:"parentId"`
	AuthorID   *uuid.UUID  `json:"authorId"`
	AuthorName string      `json:"authorName"`
	BodyMD     string      `json:"bodyMd"`
	MentionIDs []uuid.UUID `json:"mentionIds"`
	Resolved   bool        `json:"resolved"`
	Edited     bool        `json:"edited"`
	Deleted    bool        `json:"deleted"`
	CreatedAt  time.Time   `json:"createdAt"`
}

const commentCols = `c.id, c.step_id, c.parent_id, c.author_id, coalesce(u.display_name, ''),
	CASE WHEN c.deleted_at IS NULL THEN c.body_md ELSE '' END,
	coalesce((SELECT array_agg(mn.user_id ORDER BY mn.user_id) FROM mentions mn WHERE mn.comment_id = c.id), '{}'),
	c.resolved_at IS NOT NULL, c.edited_at IS NOT NULL, c.deleted_at IS NOT NULL, c.created_at`

func scanComment(row pgx.Row) (Comment, error) {
	var c Comment
	err := row.Scan(&c.ID, &c.StepID, &c.ParentID, &c.AuthorID, &c.AuthorName, &c.BodyMD, &c.MentionIDs,
		&c.Resolved, &c.Edited, &c.Deleted, &c.CreatedAt)
	return c, notFound(err)
}

// A comment of a live step placed on a live map of the workspace.
const commentFrom = `FROM comments c
	JOIN steps s ON s.id = c.step_id AND s.deleted_at IS NULL
	JOIN placements p ON p.step_id = s.id
	JOIN maps m ON m.id = p.map_id AND m.deleted_at IS NULL
	LEFT JOIN users u ON u.id = c.author_id
	WHERE m.workspace_id = $1 AND m.id = $2`

// Comments lists a step's comments, oldest first. Deleted comments are left
// out unless they still have replies.
func (s *Store) Comments(ctx context.Context, workspaceID, mapID, stepID uuid.UUID) ([]Comment, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT true FROM steps s JOIN placements p ON p.step_id = s.id JOIN maps m ON m.id = p.map_id
		 WHERE s.id = $3 AND m.id = $2 AND m.workspace_id = $1 AND s.deleted_at IS NULL AND m.deleted_at IS NULL`,
		workspaceID, mapID, stepID).Scan(&ok)
	if err != nil {
		return nil, notFound(err)
	}
	return collect(ctx, s.pool, scanComment, `SELECT `+commentCols+` `+commentFrom+`
		 AND c.step_id = $3
		 AND (c.deleted_at IS NULL OR EXISTS (SELECT 1 FROM comments r WHERE r.parent_id = c.id AND r.deleted_at IS NULL))
		 ORDER BY c.created_at, c.id`, workspaceID, mapID, stepID)
}

// CommentWrite is a saved comment and who to tell about it: the people newly
// mentioned (never the author), with the step's key and title for the email.
type CommentWrite struct {
	Comment   Comment
	Notify    []User
	StepKey   string
	StepTitle string
}

// commentNotice loads the step's key and title and the users in ids.
func commentNotice(ctx context.Context, tx pgx.Tx, w *CommentWrite, ids []uuid.UUID) error {
	if err := tx.QueryRow(ctx, `SELECT key, title FROM steps WHERE id = $1`, w.Comment.StepID).Scan(&w.StepKey, &w.StepTitle); err != nil {
		return err
	}
	var err error
	w.Notify, err = collect(ctx, tx, func(r pgx.Row) (User, error) {
		var u User
		err := r.Scan(&u.ID, &u.Email, &u.DisplayName)
		return u, err
	}, `SELECT id, email, display_name FROM users WHERE id = ANY($1) AND is_active ORDER BY display_name`, ids)
	return err
}

// addMentions records mentions of workspace members and returns the ones not
// mentioned on this comment before, leaving out the author.
func addMentions(ctx context.Context, tx pgx.Tx, workspaceID, commentID, author uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	var members int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_members WHERE workspace_id = $1 AND user_id = ANY($2)`,
		workspaceID, ids).Scan(&members); err != nil {
		return nil, err
	}
	if members != len(ids) {
		return nil, fmt.Errorf("%w: mentionIds must be members of this workspace", ErrInvalid)
	}
	return collect(ctx, tx, func(r pgx.Row) (uuid.UUID, error) {
		var id uuid.UUID
		err := r.Scan(&id)
		return id, err
	}, `INSERT INTO mentions (comment_id, user_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING RETURNING user_id`,
		commentID, ids)
}

func without(ids []uuid.UUID, drop uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{}
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

// CreateComment adds a comment, or a reply when parentID is set. A reply's
// parent must be a live first comment of the same step.
func (s *Store) CreateComment(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID, body string, parentID *uuid.UUID, mentionIDs []uuid.UUID) (CommentWrite, error) {
	var w CommentWrite
	id := newID()
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if _, _, err := mapStep(ctx, tx, mapID, stepID); err != nil {
			return nil, err
		}
		if parentID != nil {
			var ok bool
			err := tx.QueryRow(ctx, `SELECT true FROM comments WHERE id = $1 AND step_id = $2 AND parent_id IS NULL AND deleted_at IS NULL`,
				*parentID, stepID).Scan(&ok)
			if err != nil {
				return nil, fmt.Errorf("%w: replies go to a comment on the same step that is not a reply", ErrInvalid)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO comments (id, step_id, parent_id, author_id, body_md) VALUES ($1, $2, $3, $4, $5)`,
			id, stepID, parentID, actor, body); err != nil {
			return nil, err
		}
		added, err := addMentions(ctx, tx, workspaceID, id, actor, mentionIDs)
		if err != nil {
			return nil, err
		}
		if w.Comment, err = scanComment(tx.QueryRow(ctx, `SELECT `+commentCols+` `+commentFrom+` AND c.id = $3`, workspaceID, mapID, id)); err != nil {
			return nil, err
		}
		if err := commentNotice(ctx, tx, &w, without(added, actor)); err != nil {
			return nil, err
		}
		return []change{created(mapID, "comment", id, w.Comment)}, nil
	})
	return w, err
}

type CommentPatch struct {
	BodyMD     *string      // author only
	MentionIDs *[]uuid.UUID // author only; adds to the comment's mentions
	Resolved   *bool        // a first comment only; its author, or anyone allowed to resolve any
}

// UpdateComment edits or resolves a comment. canResolveAny is true for
// editors and up.
func (s *Store) UpdateComment(ctx context.Context, actor, workspaceID, mapID, commentID uuid.UUID, p CommentPatch, canResolveAny bool) (CommentWrite, error) {
	var w CommentWrite
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		before, err := scanComment(tx.QueryRow(ctx, `SELECT `+commentCols+` `+commentFrom+` AND c.id = $3 AND c.deleted_at IS NULL FOR UPDATE OF c`,
			workspaceID, mapID, commentID))
		if err != nil {
			return nil, err
		}
		mine := before.AuthorID != nil && *before.AuthorID == actor
		if (p.BodyMD != nil || p.MentionIDs != nil) && !mine {
			return nil, ErrForbidden
		}
		if p.Resolved != nil {
			if before.ParentID != nil {
				return nil, fmt.Errorf("%w: resolve the thread's first comment, not a reply", ErrInvalid)
			}
			if !mine && !canResolveAny {
				return nil, ErrForbidden
			}
		}
		var added []uuid.UUID
		if p.MentionIDs != nil {
			if added, err = addMentions(ctx, tx, workspaceID, commentID, actor, *p.MentionIDs); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE comments SET
			   edited_at = CASE WHEN $2::text IS NOT NULL AND $2 <> body_md THEN now() ELSE edited_at END,
			   body_md = COALESCE($2, body_md),
			   resolved_at = CASE WHEN $3::bool IS NULL THEN resolved_at WHEN $3 THEN coalesce(resolved_at, now()) ELSE NULL END,
			   resolved_by = CASE WHEN $3::bool IS NULL THEN resolved_by WHEN $3 THEN $4 ELSE NULL END
			 WHERE id = $1`, commentID, p.BodyMD, p.Resolved, actor); err != nil {
			return nil, err
		}
		if w.Comment, err = scanComment(tx.QueryRow(ctx, `SELECT `+commentCols+` `+commentFrom+` AND c.id = $3`, workspaceID, mapID, commentID)); err != nil {
			return nil, err
		}
		if err := commentNotice(ctx, tx, &w, without(added, actor)); err != nil {
			return nil, err
		}
		if c, ok := updated(mapID, "comment", commentID, "update", before, w.Comment); ok {
			c.before["stepId"], c.after["stepId"] = before.StepID, before.StepID
			return []change{c}, nil
		}
		return nil, nil
	})
	return w, err
}

// DeleteComment soft-deletes a comment: its author's, or anyone's when
// anyComment is true (admins).
func (s *Store) DeleteComment(ctx context.Context, actor, workspaceID, mapID, commentID uuid.UUID, anyComment bool) error {
	return s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		c, err := scanComment(tx.QueryRow(ctx, `SELECT `+commentCols+` `+commentFrom+` AND c.id = $3 AND c.deleted_at IS NULL FOR UPDATE OF c`,
			workspaceID, mapID, commentID))
		if err != nil {
			return nil, err
		}
		if (c.AuthorID == nil || *c.AuthorID != actor) && !anyComment {
			return nil, ErrForbidden
		}
		if _, err := tx.Exec(ctx, `UPDATE comments SET deleted_at = now() WHERE id = $1`, commentID); err != nil {
			return nil, err
		}
		return []change{gone(mapID, "comment", commentID, "delete", map[string]any{"stepId": c.StepID, "parentId": c.ParentID})}, nil
	})
}
