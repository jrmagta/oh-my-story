package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Owner is what a comment, or a draft file for one, is on: a step, or a node
// (an objective or activity).
type Owner struct {
	Node bool
	ID   uuid.UUID
}

func StepOwner(id uuid.UUID) Owner { return Owner{ID: id} }
func NodeOwner(id uuid.UUID) Owner { return Owner{Node: true, ID: id} }

// col is the owner's column on comments; draftCol on attachments.
func (o Owner) col() string {
	if o.Node {
		return "node_id"
	}
	return "step_id"
}

func (o Owner) draftCol() string { return "draft_" + o.col() }

// check fails unless the owner is live on the map.
func (o Owner) check(ctx context.Context, tx pgx.Tx, mapID uuid.UUID) error {
	if o.Node {
		_, err := liveNode(ctx, tx, mapID, o.ID)
		return err
	}
	_, _, err := mapStep(ctx, tx, mapID, o.ID)
	return err
}

// changes sends c where the owner is seen: every map a step is on, or a node's one map.
func (o Owner) changes(ctx context.Context, tx pgx.Tx, c change) ([]change, error) {
	if o.Node {
		return []change{c}, nil
	}
	return echoes(ctx, tx, o.ID, c)
}

// Comment is one comment on a step or node: a thread's first comment (ParentID nil)
// or a reply to one. A deleted comment keeps its place, with no body, while
// it has replies; otherwise it is left out.
type Comment struct {
	ID         uuid.UUID   `json:"id"`
	StepID     *uuid.UUID  `json:"stepId"` // one of these two is set
	NodeID     *uuid.UUID  `json:"nodeId"`
	ParentID   *uuid.UUID  `json:"parentId"`
	AuthorID   *uuid.UUID  `json:"authorId"`
	AuthorName string      `json:"authorName"`
	BodyMD     string      `json:"bodyMd"`
	MentionIDs []uuid.UUID `json:"mentionIds"`
	Resolved   bool        `json:"resolved"`
	Edited     bool        `json:"edited"`
	Deleted    bool        `json:"deleted"`
	CreatedAt  time.Time   `json:"createdAt"`
	// Attachments are the comment's files, oldest first; none once deleted.
	Attachments []Attachment `json:"attachments"`
}

const commentCols = `c.id, c.step_id, c.node_id, c.parent_id, c.author_id, coalesce(u.display_name, ''),
	CASE WHEN c.deleted_at IS NULL THEN c.body_md ELSE '' END,
	coalesce((SELECT array_agg(mn.user_id ORDER BY mn.user_id) FROM mentions mn WHERE mn.comment_id = c.id), '{}'),
	c.resolved_at IS NOT NULL, c.edited_at IS NOT NULL, c.deleted_at IS NOT NULL, c.created_at`

func scanComment(row pgx.Row) (Comment, error) {
	var c Comment
	err := row.Scan(&c.ID, &c.StepID, &c.NodeID, &c.ParentID, &c.AuthorID, &c.AuthorName, &c.BodyMD, &c.MentionIDs,
		&c.Resolved, &c.Edited, &c.Deleted, &c.CreatedAt)
	return c, notFound(err)
}

func (c Comment) owner() Owner {
	if c.NodeID != nil {
		return NodeOwner(*c.NodeID)
	}
	return StepOwner(*c.StepID)
}

// ownerFields names a comment's owner in its events.
func (c Comment) ownerFields(m map[string]any) map[string]any {
	m["stepId"], m["nodeId"] = c.StepID, c.NodeID
	return m
}

// A comment of a live step placed on map $2, or of a live node of it; the map
// is live and in workspace $1.
const commentFrom = `FROM comments c
	LEFT JOIN users u ON u.id = c.author_id
	WHERE EXISTS (SELECT 1 FROM maps m WHERE m.id = $2 AND m.workspace_id = $1 AND m.deleted_at IS NULL)
	  AND (EXISTS (SELECT 1 FROM steps s JOIN placements p ON p.step_id = s.id
	         WHERE s.id = c.step_id AND s.deleted_at IS NULL AND p.map_id = $2)
	    OR EXISTS (SELECT 1 FROM nodes n WHERE n.id = c.node_id AND n.deleted_at IS NULL AND n.map_id = $2))`

// Comments lists a step's or node's comments, oldest first. Deleted comments
// are left out unless they still have replies.
func (s *Store) Comments(ctx context.Context, workspaceID, mapID uuid.UUID, o Owner) ([]Comment, error) {
	var cs []Comment
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.CheckMap(ctx, workspaceID, mapID); err != nil {
			return err
		}
		if err := o.check(ctx, tx, mapID); err != nil {
			return err
		}
		var err error
		cs, err = collect(ctx, tx, scanComment, `SELECT `+commentCols+` `+commentFrom+`
			 AND c.`+o.col()+` = $3
			 AND (c.deleted_at IS NULL OR EXISTS (SELECT 1 FROM comments r WHERE r.parent_id = c.id AND r.deleted_at IS NULL))
			 ORDER BY c.created_at, c.id`, workspaceID, mapID, o.ID)
		if err != nil {
			return err
		}
		return commentFiles(ctx, tx, cs)
	})
	return cs, err
}

// loadComment reads one comment, with its files, as the map shows it.
func loadComment(ctx context.Context, tx pgx.Tx, workspaceID, mapID, id uuid.UUID) (Comment, error) {
	c, err := scanComment(tx.QueryRow(ctx, `SELECT `+commentCols+` `+commentFrom+` AND c.id = $3`, workspaceID, mapID, id))
	if err != nil {
		return c, err
	}
	cs := []Comment{c}
	err = commentFiles(ctx, tx, cs)
	return cs[0], err
}

// CommentWrite is a saved comment and who to tell about it: the people newly
// mentioned (never the author), with what the comment is on for the email:
// Where in full ("CHC-12 Card form", "activity “Pay”") and WhereShort ("CHC-12", "“Pay”").
type CommentWrite struct {
	Comment           Comment
	Notify            []User
	Where, WhereShort string
}

// commentNotice names the comment's owner and loads the users in ids.
func commentNotice(ctx context.Context, tx pgx.Tx, w *CommentWrite, ids []uuid.UUID) error {
	if o := w.Comment.owner(); o.Node {
		var level, title string
		if err := tx.QueryRow(ctx, `SELECT level, title FROM nodes WHERE id = $1`, o.ID).Scan(&level, &title); err != nil {
			return err
		}
		w.WhereShort = "“" + title + "”"
		w.Where = level + " " + w.WhereShort
	} else {
		var key, title string
		if err := tx.QueryRow(ctx, `SELECT key, title FROM steps WHERE id = $1`, o.ID).Scan(&key, &title); err != nil {
			return err
		}
		w.WhereShort, w.Where = key, key+" "+title
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

// CreateComment adds a comment to a step or node, or a reply when parentID is
// set. A reply's parent must be a live first comment on the same owner.
// attachmentIDs are the author's drafts on the owner, which become the
// comment's files.
func (s *Store) CreateComment(ctx context.Context, actor, workspaceID, mapID uuid.UUID, o Owner, body string, parentID *uuid.UUID, mentionIDs, attachmentIDs []uuid.UUID) (CommentWrite, error) {
	var w CommentWrite
	id := newID()
	err := s.write(ctx, actor, workspaceID, mapID, func(tx pgx.Tx) ([]change, error) {
		if err := o.check(ctx, tx, mapID); err != nil {
			return nil, err
		}
		if parentID != nil {
			var ok bool
			err := tx.QueryRow(ctx, `SELECT true FROM comments WHERE id = $1 AND `+o.col()+` = $2 AND parent_id IS NULL AND deleted_at IS NULL`,
				*parentID, o.ID).Scan(&ok)
			if err != nil {
				return nil, fmt.Errorf("%w: replies go to a comment on the same card that is not a reply", ErrInvalid)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO comments (id, `+o.col()+`, parent_id, author_id, body_md) VALUES ($1, $2, $3, $4, $5)`,
			id, o.ID, parentID, actor, body); err != nil {
			return nil, err
		}
		added, err := addMentions(ctx, tx, workspaceID, id, actor, mentionIDs)
		if err != nil {
			return nil, err
		}
		if err := claimDrafts(ctx, tx, actor, o, id, attachmentIDs); err != nil {
			return nil, err
		}
		if w.Comment, err = loadComment(ctx, tx, workspaceID, mapID, id); err != nil {
			return nil, err
		}
		if err := commentNotice(ctx, tx, &w, without(added, actor)); err != nil {
			return nil, err
		}
		return o.changes(ctx, tx, created(mapID, "comment", id, w.Comment))
	})
	return w, err
}

type CommentPatch struct {
	BodyMD     *string      // author only
	MentionIDs *[]uuid.UUID // author only; adds to the comment's mentions
	Resolved   *bool        // a first comment only; its author, or anyone allowed to resolve any
	// AttachmentIDs, author only, is every file the comment keeps: its files
	// left out are deleted, and the author's drafts on the step are claimed.
	AttachmentIDs *[]uuid.UUID
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
		cs := []Comment{before}
		if err := commentFiles(ctx, tx, cs); err != nil {
			return nil, err
		}
		before = cs[0]
		mine := before.AuthorID != nil && *before.AuthorID == actor
		if (p.BodyMD != nil || p.MentionIDs != nil || p.AttachmentIDs != nil) && !mine {
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
		if p.AttachmentIDs != nil {
			if err := keepFiles(ctx, tx, actor, before, *p.AttachmentIDs); err != nil {
				return nil, err
			}
		}
		if w.Comment, err = loadComment(ctx, tx, workspaceID, mapID, commentID); err != nil {
			return nil, err
		}
		if err := commentNotice(ctx, tx, &w, without(added, actor)); err != nil {
			return nil, err
		}
		if c, ok := updated(mapID, "comment", commentID, "update", before, w.Comment); ok {
			before.ownerFields(c.before)
			before.ownerFields(c.after)
			return before.owner().changes(ctx, tx, c)
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
		return c.owner().changes(ctx, tx, gone(mapID, "comment", commentID, "delete", c.ownerFields(map[string]any{"parentId": c.ParentID})))
	})
}

// keepFiles makes c's files exactly ids: the ones left out are deleted, and
// the rest that are not c's yet must be actor's drafts on c's step or node.
func keepFiles(ctx context.Context, tx pgx.Tx, actor uuid.UUID, c Comment, ids []uuid.UUID) error {
	keep := map[uuid.UUID]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	var gone, fresh []uuid.UUID
	have := map[uuid.UUID]bool{}
	for _, a := range c.Attachments {
		have[a.ID] = true
		if !keep[a.ID] {
			gone = append(gone, a.ID)
		}
	}
	for _, id := range ids {
		if !have[id] {
			fresh = append(fresh, id)
		}
	}
	if len(gone) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE attachments SET deleted_at = now() WHERE id = ANY($1)`, gone); err != nil {
			return err
		}
	}
	return claimDrafts(ctx, tx, actor, c.owner(), c.ID, fresh)
}
