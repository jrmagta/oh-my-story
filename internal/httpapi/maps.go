package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/jrmagta/oh-my-story/internal/role"
	"github.com/jrmagta/oh-my-story/internal/store"
)

// mapRoutes registers workflows, maps and everything on a map. Viewers read,
// editors change maps, admins change workflows.
func (s *Server) mapRoutes(mux *http.ServeMux) {
	const ws = "/api/workspaces/{slug}"
	const m = ws + "/maps/{mapId}"
	routes := []struct {
		pattern string
		action  role.Action
		h       mapHandler
	}{
		{"GET " + ws + "/workflows", role.View, s.listWorkflows},
		{"POST " + ws + "/workflows", role.ManageSettings, s.createWorkflow},
		{"PATCH " + ws + "/workflows/{id}", role.ManageSettings, s.patchWorkflow},
		{"POST " + ws + "/workflows/{id}/statuses", role.ManageSettings, s.createStatus},
		{"PATCH " + ws + "/statuses/{id}", role.ManageSettings, s.patchStatus},
		{"POST " + ws + "/statuses/{id}/move", role.ManageSettings, s.moveStatus},
		{"DELETE " + ws + "/statuses/{id}", role.ManageSettings, s.deleteStatus},

		{"GET " + ws + "/maps", role.View, s.listMaps},
		{"POST " + ws + "/maps", role.Edit, s.createMap},
		{"GET " + m, role.View, s.getMap},
		{"PATCH " + m, role.Edit, s.patchMap},
		{"DELETE " + m, role.Edit, s.deleteMap},

		{"POST " + m + "/nodes", role.Edit, s.createNode},
		{"PATCH " + m + "/nodes/{id}", role.Edit, s.patchNode},
		{"POST " + m + "/nodes/{id}/move", role.Edit, s.moveNode},
		{"DELETE " + m + "/nodes/{id}", role.Edit, s.deleteNode},
		{"POST " + m + "/nodes/{id}/restore", role.Edit, s.restoreNode},

		{"POST " + m + "/releases", role.Edit, s.createRelease},
		{"PATCH " + m + "/releases/{id}", role.Edit, s.patchRelease},
		{"POST " + m + "/releases/{id}/move", role.Edit, s.moveRelease},
		{"DELETE " + m + "/releases/{id}", role.Edit, s.deleteRelease},

		{"POST " + m + "/stories", role.Edit, s.createStory},
		{"PATCH " + m + "/stories/{id}", role.Edit, s.patchStory},
		{"POST " + m + "/stories/{id}/move", role.Edit, s.moveStory},
		{"DELETE " + m + "/stories/{id}", role.Edit, s.deleteStory},
		{"POST " + m + "/stories/{id}/restore", role.Edit, s.restoreStory},
	}
	for _, rt := range routes {
		mux.HandleFunc(rt.pattern, s.inWorkspace(rt.action, mapEndpoint(rt.h)))
	}
}

// mapRequest is what every map handler needs: the caller, the workspace and
// the ids in the path (uuid.Nil where the pattern has none).
type mapRequest struct {
	r     *http.Request
	w     http.ResponseWriter
	user  uuid.UUID
	ws    uuid.UUID
	mapID uuid.UUID
	id    uuid.UUID
}

// mapHandler returns the status and body to send. A nil body sends 204.
type mapHandler func(q mapRequest) (int, any, error)

func mapEndpoint(h mapHandler) workspaceHandler {
	return func(w http.ResponseWriter, r *http.Request, u store.User, m store.Membership) {
		q := mapRequest{r: r, w: w, user: u.ID, ws: m.ID}
		for name, dst := range map[string]*uuid.UUID{"mapId": &q.mapID, "id": &q.id} {
			if r.PathValue(name) == "" {
				continue
			}
			id, err := pathUUID(r, name)
			if err != nil {
				writeError(w, err)
				return
			}
			*dst = id
		}
		status, body, err := h(q)
		switch {
		case err != nil:
			writeError(w, err)
		case body == nil:
			w.WriteHeader(http.StatusNoContent)
		default:
			writeJSON(w, status, body)
		}
	}
}

func (q mapRequest) read(dst any) error { return readJSON(q.w, q.r, dst) }

type positionIn struct {
	AfterID  *uuid.UUID `json:"afterId"`
	BeforeID *uuid.UUID `json:"beforeId"`
}

func (p positionIn) position() store.Position {
	return store.Position{After: p.AfterID, Before: p.BeforeID}
}

// ---- workflows ----

func (s *Server) listWorkflows(q mapRequest) (int, any, error) {
	wfs, err := s.store.Workflows(q.r.Context(), q.ws)
	return http.StatusOK, wfs, err
}

func (s *Server) createWorkflow(q mapRequest) (int, any, error) {
	var in struct{ Name string }
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	name, err := text("name", in.Name, maxName, false)
	if err != nil {
		return 0, nil, err
	}
	wf, err := s.store.CreateWorkflow(q.r.Context(), q.user, q.ws, name)
	return http.StatusCreated, wf, err
}

func (s *Server) patchWorkflow(q mapRequest) (int, any, error) {
	var in struct{ Name string }
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	name, err := text("name", in.Name, maxName, false)
	if err != nil {
		return 0, nil, err
	}
	wf, err := s.store.RenameWorkflow(q.r.Context(), q.user, q.ws, q.id, name)
	return http.StatusOK, wf, err
}

var categories = []string{"todo", "doing", "done", "blocked"}

func (s *Server) createStatus(q mapRequest) (int, any, error) {
	var in struct {
		Name, Category string
		Colour         *string
		positionIn
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var st store.StatusInput
	var err error
	if st.Name, err = text("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if st.Category, err = oneOf("category", in.Category, categories...); err != nil {
		return 0, nil, err
	}
	if st.Colour, err = parseColour(in.Colour); err != nil {
		return 0, nil, err
	}
	out, err := s.store.CreateStatus(q.r.Context(), q.user, q.ws, q.id, st, in.position())
	return http.StatusCreated, out, err
}

func (s *Server) patchStatus(q mapRequest) (int, any, error) {
	var in struct{ Name, Category, Colour *string }
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var p store.StatusPatch
	var err error
	if p.Name, err = textPtr("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if in.Category != nil {
		c, err := oneOf("category", *in.Category, categories...)
		if err != nil {
			return 0, nil, err
		}
		p.Category = &c
	}
	if p.Colour, err = parseColour(in.Colour); err != nil {
		return 0, nil, err
	}
	out, err := s.store.UpdateStatus(q.r.Context(), q.user, q.ws, q.id, p)
	return http.StatusOK, out, err
}

func (s *Server) moveStatus(q mapRequest) (int, any, error) {
	var in positionIn
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	out, err := s.store.MoveStatus(q.r.Context(), q.user, q.ws, q.id, in.position())
	return http.StatusOK, out, err
}

// deleteStatus takes ?replacementId=<status> when stories use the status.
func (s *Server) deleteStatus(q mapRequest) (int, any, error) {
	var replacement *uuid.UUID
	if v := q.r.URL.Query().Get("replacementId"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return 0, nil, errBadRequest("replacementId is not a valid id")
		}
		replacement = &id
	}
	return 0, nil, s.store.DeleteStatus(q.r.Context(), q.user, q.ws, q.id, replacement)
}

// ---- maps ----

func (s *Server) listMaps(q mapRequest) (int, any, error) {
	ms, err := s.store.Maps(q.r.Context(), q.ws)
	return http.StatusOK, ms, err
}

func (s *Server) createMap(q mapRequest) (int, any, error) {
	var in struct {
		Name, Description string
		Levels            int
		WorkflowID        *uuid.UUID
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	mi := store.MapInput{Levels: in.Levels, WorkflowID: in.WorkflowID}
	var err error
	if mi.Name, err = text("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if mi.Description, err = text("description", in.Description, maxDescription, true); err != nil {
		return 0, nil, err
	}
	if in.Levels != 3 && in.Levels != 4 {
		return 0, nil, errBadRequest("levels must be 3 or 4")
	}
	m, err := s.store.CreateMap(q.r.Context(), q.user, q.ws, mi)
	return http.StatusCreated, m, err
}

func (s *Server) getMap(q mapRequest) (int, any, error) {
	v, err := s.store.MapView(q.r.Context(), q.ws, q.mapID)
	return http.StatusOK, v, err
}

func (s *Server) patchMap(q mapRequest) (int, any, error) {
	var in struct {
		Name, Description *string
		Levels            *int
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	p := store.MapPatch{Levels: in.Levels}
	var err error
	if p.Name, err = textPtr("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if p.Description, err = textPtr("description", in.Description, maxDescription, true); err != nil {
		return 0, nil, err
	}
	if in.Levels != nil && *in.Levels != 3 && *in.Levels != 4 {
		return 0, nil, errBadRequest("levels must be 3 or 4")
	}
	m, err := s.store.UpdateMap(q.r.Context(), q.user, q.ws, q.mapID, p)
	return http.StatusOK, m, err
}

func (s *Server) deleteMap(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteMap(q.r.Context(), q.user, q.ws, q.mapID)
}

// ---- backbone nodes ----

func (s *Server) createNode(q mapRequest) (int, any, error) {
	var in struct {
		ParentID                  *uuid.UUID
		Level, Title, Description string
		Colour                    *string
		positionIn
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	n := store.NodeInput{ParentID: in.ParentID}
	var err error
	if n.Level, err = oneOf("level", in.Level, "objective", "activity", "step"); err != nil {
		return 0, nil, err
	}
	if n.Title, err = text("title", in.Title, maxTitle, false); err != nil {
		return 0, nil, err
	}
	if n.Description, err = text("description", in.Description, maxDescription, true); err != nil {
		return 0, nil, err
	}
	if n.Colour, err = parseColour(in.Colour); err != nil {
		return 0, nil, err
	}
	out, err := s.store.CreateNode(q.r.Context(), q.user, q.ws, q.mapID, n, in.position())
	return http.StatusCreated, out, err
}

func (s *Server) patchNode(q mapRequest) (int, any, error) {
	var in struct{ Title, Description, Colour *string }
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var p store.NodePatch
	var err error
	if p.Title, err = textPtr("title", in.Title, maxTitle, false); err != nil {
		return 0, nil, err
	}
	if p.Description, err = textPtr("description", in.Description, maxDescription, true); err != nil {
		return 0, nil, err
	}
	if p.Colour, err = parseColour(in.Colour); err != nil {
		return 0, nil, err
	}
	out, err := s.store.UpdateNode(q.r.Context(), q.user, q.ws, q.mapID, q.id, p)
	return http.StatusOK, out, err
}

// moveNode needs parentId: the target parent, or null for the top level.
func (s *Server) moveNode(q mapRequest) (int, any, error) {
	var in struct {
		ParentID optional[uuid.UUID]
		positionIn
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	if !in.ParentID.Set {
		return 0, nil, errBadRequest("parentId is required (null for the top level)")
	}
	out, err := s.store.MoveNode(q.r.Context(), q.user, q.ws, q.mapID, q.id, in.ParentID.Value, in.position())
	return http.StatusOK, out, err
}

func (s *Server) deleteNode(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteNode(q.r.Context(), q.user, q.ws, q.mapID, q.id)
}

func (s *Server) restoreNode(q mapRequest) (int, any, error) {
	out, err := s.store.RestoreNode(q.r.Context(), q.user, q.ws, q.mapID, q.id)
	return http.StatusOK, out, err
}

// ---- releases ----

func (s *Server) createRelease(q mapRequest) (int, any, error) {
	var in struct {
		Name, Description string
		TargetDate        *string
		positionIn
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var ri store.ReleaseInput
	var err error
	if ri.Name, err = text("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if ri.Description, err = text("description", in.Description, maxDescription, true); err != nil {
		return 0, nil, err
	}
	if ri.TargetDate, err = parseDate(in.TargetDate); err != nil {
		return 0, nil, err
	}
	out, err := s.store.CreateRelease(q.r.Context(), q.user, q.ws, q.mapID, ri, in.position())
	return http.StatusCreated, out, err
}

// patchRelease clears the target date when targetDate is null.
func (s *Server) patchRelease(q mapRequest) (int, any, error) {
	var in struct {
		Name, Description *string
		TargetDate        optional[string]
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	p := store.ReleasePatch{SetTargetDate: in.TargetDate.Set}
	var err error
	if p.Name, err = textPtr("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if p.Description, err = textPtr("description", in.Description, maxDescription, true); err != nil {
		return 0, nil, err
	}
	if p.TargetDate, err = parseDate(in.TargetDate.Value); err != nil {
		return 0, nil, err
	}
	out, err := s.store.UpdateRelease(q.r.Context(), q.user, q.ws, q.mapID, q.id, p)
	return http.StatusOK, out, err
}

func (s *Server) moveRelease(q mapRequest) (int, any, error) {
	var in positionIn
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	out, err := s.store.MoveRelease(q.r.Context(), q.user, q.ws, q.mapID, q.id, in.position())
	return http.StatusOK, out, err
}

func (s *Server) deleteRelease(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteRelease(q.r.Context(), q.user, q.ws, q.mapID, q.id)
}

// ---- stories ----

type storyAndPlacement struct {
	Story     store.Story     `json:"story"`
	Placement store.Placement `json:"placement"`
}

func (s *Server) createStory(q mapRequest) (int, any, error) {
	var in struct {
		Title, DescriptionMD string
		StepID               uuid.UUID
		ReleaseID            *uuid.UUID
		positionIn
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	si := store.StoryInput{StepID: in.StepID, ReleaseID: in.ReleaseID}
	var err error
	if si.Title, err = text("title", in.Title, maxTitle, false); err != nil {
		return 0, nil, err
	}
	if si.DescriptionMD, err = text("descriptionMd", in.DescriptionMD, maxDescription, true); err != nil {
		return 0, nil, err
	}
	if in.StepID == uuid.Nil {
		return 0, nil, errBadRequest("stepId is required")
	}
	st, p, err := s.store.CreateStory(q.r.Context(), q.user, q.ws, q.mapID, si, in.position())
	return http.StatusCreated, storyAndPlacement{st, p}, err
}

// patchStory unassigns when assigneeId is null.
func (s *Server) patchStory(q mapRequest) (int, any, error) {
	var in struct {
		Title, DescriptionMD *string
		StatusID             *uuid.UUID
		AssigneeID           optional[uuid.UUID]
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	p := store.StoryPatch{StatusID: in.StatusID, SetAssignee: in.AssigneeID.Set, AssigneeID: in.AssigneeID.Value}
	var err error
	if p.Title, err = textPtr("title", in.Title, maxTitle, false); err != nil {
		return 0, nil, err
	}
	if p.DescriptionMD, err = textPtr("descriptionMd", in.DescriptionMD, maxDescription, true); err != nil {
		return 0, nil, err
	}
	out, err := s.store.UpdateStory(q.r.Context(), q.user, q.ws, q.mapID, q.id, p)
	return http.StatusOK, out, err
}

// moveStory places a story in a cell; releaseId null is Unscheduled. version
// is the story version the client showed; the reply says whether it was stale.
func (s *Server) moveStory(q mapRequest) (int, any, error) {
	var in struct {
		StepID    uuid.UUID
		ReleaseID optional[uuid.UUID]
		Version   *int
		positionIn
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	if in.StepID == uuid.Nil || !in.ReleaseID.Set || in.Version == nil {
		return 0, nil, errBadRequest("stepId, releaseId (null for Unscheduled) and version are required")
	}
	p, stale, err := s.store.MoveStory(q.r.Context(), q.user, q.ws, q.mapID, q.id, store.StoryMove{
		StepID: in.StepID, ReleaseID: in.ReleaseID.Value, Position: in.position(), SeenVersion: *in.Version,
	})
	return http.StatusOK, map[string]any{"placement": p, "stale": stale}, err
}

func (s *Server) deleteStory(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteStory(q.r.Context(), q.user, q.ws, q.mapID, q.id)
}

func (s *Server) restoreStory(q mapRequest) (int, any, error) {
	out, err := s.store.RestoreStory(q.r.Context(), q.user, q.ws, q.mapID, q.id)
	return http.StatusOK, out, err
}
