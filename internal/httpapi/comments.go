package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/jrmagta/oh-my-story/internal/mail"
	"github.com/jrmagta/oh-my-story/internal/role"
	"github.com/jrmagta/oh-my-story/internal/store"
)

const (
	maxComment      = 10_000
	maxCommentFiles = 20
)

// listComments lists the comments on step or node {id}; owner says which.
func (s *Server) listComments(owner func(uuid.UUID) store.Owner) func(mapRequest) (int, any, error) {
	return func(q mapRequest) (int, any, error) {
		cs, err := s.store.Comments(q.r.Context(), q.ws, q.mapID, owner(q.id))
		return http.StatusOK, cs, err
	}
}

// createComment adds a comment to step or node {id}, or a reply with
// parentId. mentionIds are workspace members the body mentions; each is
// emailed once.
func (s *Server) createComment(owner func(uuid.UUID) store.Owner) func(mapRequest) (int, any, error) {
	return func(q mapRequest) (int, any, error) { return s.addComment(q, owner(q.id)) }
}

func (s *Server) addComment(q mapRequest, o store.Owner) (int, any, error) {
	var in struct {
		BodyMD        string
		ParentID      *uuid.UUID
		MentionIDs    []uuid.UUID
		AttachmentIDs []uuid.UUID
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	body, err := text("bodyMd", in.BodyMD, maxComment, false)
	if err != nil {
		return 0, nil, err
	}
	mentions := in.MentionIDs
	if mentions == nil {
		mentions = []uuid.UUID{}
	}
	if err := idSet("mentionIds", &mentions, 50); err != nil {
		return 0, nil, err
	}
	files := in.AttachmentIDs
	if files == nil {
		files = []uuid.UUID{}
	}
	if err := idSet("attachmentIds", &files, maxCommentFiles); err != nil {
		return 0, nil, err
	}
	w, err := s.store.CreateComment(q.r.Context(), q.user, q.ws, q.mapID, o, body, in.ParentID, mentions, files)
	if err != nil {
		return 0, nil, err
	}
	s.notifyMentions(q, w)
	return http.StatusCreated, w.Comment, nil
}

// patchComment edits (author only) or resolves (the thread's author, or an editor) a comment.
func (s *Server) patchComment(q mapRequest) (int, any, error) {
	var in struct {
		BodyMD        *string
		MentionIDs    *[]uuid.UUID
		Resolved      *bool
		AttachmentIDs *[]uuid.UUID
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var p store.CommentPatch
	var err error
	if p.BodyMD, err = textPtr("bodyMd", in.BodyMD, maxComment, false); err != nil {
		return 0, nil, err
	}
	if err := idSet("mentionIds", in.MentionIDs, 50); err != nil {
		return 0, nil, err
	}
	if err := idSet("attachmentIds", in.AttachmentIDs, maxCommentFiles); err != nil {
		return 0, nil, err
	}
	p.MentionIDs, p.Resolved, p.AttachmentIDs = in.MentionIDs, in.Resolved, in.AttachmentIDs
	w, err := s.store.UpdateComment(q.r.Context(), q.user, q.ws, q.mapID, q.id, p, q.role.Can(role.Edit))
	if err != nil {
		return 0, nil, err
	}
	s.notifyMentions(q, w)
	return http.StatusOK, w.Comment, nil
}

// deleteComment removes the caller's own comment; admins can remove any.
func (s *Server) deleteComment(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteComment(q.r.Context(), q.user, q.ws, q.mapID, q.id, q.role.Can(role.ManageSettings))
}

// notifyMentions emails the people a comment newly mentions, after the reply.
func (s *Server) notifyMentions(q mapRequest, w store.CommentWrite) {
	if len(w.Notify) == 0 {
		return
	}
	open := "step=" + uuidString(w.Comment.StepID)
	if w.Comment.NodeID != nil {
		open = "node=" + w.Comment.NodeID.String()
	}
	link := fmt.Sprintf("%s/w/%s/maps/%s?%s", s.cfg.BaseURL, q.r.PathValue("slug"), q.mapID, open)
	excerpt := w.Comment.BodyMD
	if utf8.RuneCountInString(excerpt) > 500 {
		excerpt = string([]rune(excerpt)[:500]) + "…"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, u := range w.Notify {
			msg := mail.Message{
				To:      u.Email,
				Subject: fmt.Sprintf("%s mentioned you on %s", w.Comment.AuthorName, w.WhereShort),
				Body: fmt.Sprintf("%s mentioned you in a comment on %s:\n\n%s\n\nOpen it:\n%s\n",
					w.Comment.AuthorName, w.Where, indent(excerpt), link),
			}
			if err := s.mail.Send(ctx, msg); err != nil {
				slog.Error("send mention email", "to", u.Email, "err", err)
			}
		}
	}()
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}

func uuidString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
