package httpapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/config"
	"github.com/jrmagta/oh-my-story/internal/store"
)

const (
	maxFileName = 255
	// maxTransfers caps the uploads and downloads one process holds in memory
	// at once; more wait their turn. Each can hold two or three copies of a file.
	maxTransfers = 4
)

// inlineTypes are the only files sent for the browser to show; the rest
// download, so an uploaded page or SVG never runs in the app's origin.
var inlineTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func (s *Server) maxFile() int64 {
	if s.cfg.AttachmentMaxBytes > 0 {
		return s.cfg.AttachmentMaxBytes
	}
	return config.DefaultAttachmentMaxBytes
}

// transfer waits for a free transfer slot; call the returned func to give it back.
func (s *Server) transfer(ctx context.Context) (func(), error) {
	select {
	case s.transfers <- struct{}{}:
		return func() { <-s.transfers }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Server) listStepAttachments(q mapRequest) (int, any, error) {
	as, err := s.store.StepAttachments(q.r.Context(), q.ws, q.mapID, q.id)
	return http.StatusOK, as, err
}

// uploadStepAttachment stores the request body as a file on step {id}.
func (s *Server) uploadStepAttachment(q mapRequest) (int, any, error) {
	return s.upload(q, s.store.AddStepAttachment)
}

func (s *Server) listNodeAttachments(q mapRequest) (int, any, error) {
	as, err := s.store.NodeAttachments(q.r.Context(), q.ws, q.mapID, q.id)
	return http.StatusOK, as, err
}

// uploadNodeAttachment stores the request body as a file on objective or activity {id}.
func (s *Server) uploadNodeAttachment(q mapRequest) (int, any, error) {
	return s.upload(q, s.store.AddNodeAttachment)
}

// uploadDraft stores the request body as a draft for a comment on step or
// node {id}: only the uploader sees it until a comment claims it.
func (s *Server) uploadDraft(owner func(uuid.UUID) store.Owner) func(mapRequest) (int, any, error) {
	return func(q mapRequest) (int, any, error) {
		return s.upload(q, func(ctx context.Context, actor, workspaceID, mapID, id uuid.UUID, f store.FileUpload) (store.Attachment, error) {
			return s.store.AddDraftAttachment(ctx, actor, workspaceID, mapID, owner(id), f)
		})
	}
}

// upload reads a file from the request and stores it with add. The body is
// the file's bytes, sent as application/octet-stream (a type no cross-site
// form can send); ?name= is its file name. The type is sniffed from the bytes.
func (s *Server) upload(q mapRequest,
	add func(ctx context.Context, actor, workspaceID, mapID, stepID uuid.UUID, f store.FileUpload) (store.Attachment, error)) (int, any, error) {
	if !strings.HasPrefix(q.r.Header.Get("Content-Type"), "application/octet-stream") {
		return 0, nil, errBadRequest("send the file's bytes as the body, with Content-Type: application/octet-stream")
	}
	name, err := fileName(q.r.URL.Query().Get("name"))
	if err != nil {
		return 0, nil, err
	}
	release, err := s.transfer(q.r.Context())
	if err != nil {
		return 0, nil, err
	}
	defer release()
	data, err := readFile(q.w, q.r, s.maxFile())
	if err != nil {
		return 0, nil, err
	}
	a, err := add(q.r.Context(), q.user, q.ws, q.mapID, q.id, store.FileUpload{Name: name, ContentType: http.DetectContentType(data), Data: data})
	return http.StatusCreated, a, err
}

func (s *Server) deleteAttachment(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteAttachment(q.r.Context(), q.user, q.ws, q.mapID, q.id)
}

func (s *Server) restoreAttachment(q mapRequest) (int, any, error) {
	return 0, nil, s.store.RestoreAttachment(q.r.Context(), q.user, q.ws, q.mapID, q.id)
}

// downloadAttachment sends a file of the workspace.
func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request, u store.User, m store.Membership) {
	s.sendFile(w, r,
		func(id uuid.UUID) ([]byte, error) { return s.store.AttachmentETag(r.Context(), m.ID, u.ID, id) },
		func(id uuid.UUID) (store.Attachment, []byte, error) {
			return s.store.AttachmentFile(r.Context(), m.ID, u.ID, id)
		})
}

// sharedAttachments lists the files of a step on a share link's map. No session needed.
func (s *Server) sharedAttachments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	as, err := s.store.SharedStepAttachments(r.Context(), auth.HashToken(r.PathValue("token")), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

// sharedNodeAttachments lists the files of an objective or activity on a share link's map.
func (s *Server) sharedNodeAttachments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	as, err := s.store.SharedNodeAttachments(r.Context(), auth.HashToken(r.PathValue("token")), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

// sharedAttachment sends a file of a step on a share link's map.
func (s *Server) sharedAttachment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	hash := auth.HashToken(r.PathValue("token"))
	s.sendFile(w, r,
		func(id uuid.UUID) ([]byte, error) { return s.store.SharedAttachmentETag(r.Context(), hash, id) },
		func(id uuid.UUID) (store.Attachment, []byte, error) {
			return s.store.SharedAttachmentFile(r.Context(), hash, id)
		})
}

// sendFile sends file {id}'s bytes. Files never change, so a cached copy
// stays good and If-None-Match is answered without reading the bytes.
func (s *Server) sendFile(w http.ResponseWriter, r *http.Request,
	etagOf func(uuid.UUID) ([]byte, error), file func(uuid.UUID) (store.Attachment, []byte, error)) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeError(w, err)
		return
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if sum, err := etagOf(id); err == nil && inm == etag(sum) {
			cacheForever(w, sum)
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	release, err := s.transfer(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer release()
	a, data, err := file(id)
	if err != nil {
		writeError(w, err)
		return
	}
	serveFile(w, a, data)
}

func serveFile(w http.ResponseWriter, a store.Attachment, data []byte) {
	h := w.Header()
	h.Set("Content-Type", a.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	disposition := "attachment"
	if inlineTypes[a.ContentType] {
		disposition = "inline"
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}))
	cacheForever(w, a.SHA256)
	_, _ = w.Write(data)
}

func etag(sum []byte) string { return `"` + hex.EncodeToString(sum) + `"` }

func cacheForever(w http.ResponseWriter, sum []byte) {
	w.Header().Set("ETag", etag(sum))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
}

// readFile reads a request body of at most max bytes; a larger one is 413.
func readFile(w http.ResponseWriter, r *http.Request, max int64) ([]byte, error) {
	tooLarge := &apiError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("files can be at most %d MB", max>>20)}
	if r.ContentLength > max {
		return nil, tooLarge
	}
	var buf bytes.Buffer
	if r.ContentLength > 0 {
		buf.Grow(int(r.ContentLength))
	}
	if _, err := io.Copy(&buf, http.MaxBytesReader(w, r.Body, max)); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, tooLarge
		}
		return nil, errBadRequest("could not read the file: " + err.Error())
	}
	return buf.Bytes(), nil
}

// fileName keeps the last path element of a name and drops control
// characters, so a name is safe to show and to send back in a header.
func fileName(raw string) (string, error) {
	if i := strings.LastIndexAny(raw, `/\`); i >= 0 {
		raw = raw[i+1:]
	}
	name := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, raw))
	switch {
	case name == "" || name == "." || name == "..":
		return "", errBadRequest("?name= must give the file's name")
	case len([]rune(name)) > maxFileName:
		return "", errBadRequest(fmt.Sprintf("file names can be at most %d characters", maxFileName))
	}
	return name, nil
}
