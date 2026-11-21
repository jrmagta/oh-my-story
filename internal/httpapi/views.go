package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/jrmagta/oh-my-story/internal/role"
	"github.com/jrmagta/oh-my-story/internal/store"
)

// viewFilter is the map filter a saved view holds (web/src/map/filter.ts).
// Ids are not checked: a filter naming a deleted label simply matches nothing.
type viewFilter struct {
	Labels    []string `json:"labels"`
	Statuses  []string `json:"statuses"`
	Assignees []string `json:"assignees"`
	Personas  []string `json:"personas"`
	Releases  []string `json:"releases"`
	Text      string   `json:"text"`
	Mode      string   `json:"mode"`
}

const maxFilterText = 200

// parseViewFilter checks the shape of a filter and returns it re-encoded, so
// the database holds only these fields.
func parseViewFilter(raw json.RawMessage) (json.RawMessage, error) {
	var f viewFilter
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, errBadRequest("filter must be {labels, statuses, assignees, personas, releases, text, mode}")
	}
	for _, list := range [][]string{f.Labels, f.Statuses, f.Assignees, f.Personas, f.Releases} {
		if len(list) > 100 {
			return nil, errBadRequest("a filter field takes at most 100 values")
		}
	}
	if len(f.Text) > maxFilterText {
		return nil, errBadRequest("filter text must be at most 200 characters")
	}
	if _, err := oneOf("filter mode", f.Mode, "highlight", "hide"); err != nil {
		return nil, err
	}
	for _, p := range []*[]string{&f.Labels, &f.Statuses, &f.Assignees, &f.Personas, &f.Releases} {
		if *p == nil {
			*p = []string{}
		}
	}
	return json.Marshal(f)
}

func (s *Server) listViews(q mapRequest) (int, any, error) {
	vs, err := s.store.SavedViews(q.r.Context(), q.ws, q.mapID, q.user)
	return http.StatusOK, vs, err
}

// createView saves a private view; shared: true needs editor.
func (s *Server) createView(q mapRequest) (int, any, error) {
	var in struct {
		Name   string
		Filter json.RawMessage
		Shared bool
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	name, err := text("name", in.Name, maxName, false)
	if err != nil {
		return 0, nil, err
	}
	filter, err := parseViewFilter(in.Filter)
	if err != nil {
		return 0, nil, err
	}
	if in.Shared && !q.role.Can(role.Edit) {
		return 0, nil, errForbidden
	}
	v, err := s.store.CreateSavedView(q.r.Context(), q.ws, q.mapID, q.user, name, filter, in.Shared)
	return http.StatusCreated, v, err
}

// patchView changes the caller's own view; sharing it needs editor.
func (s *Server) patchView(q mapRequest) (int, any, error) {
	var in struct {
		Name   *string
		Filter json.RawMessage
		Shared *bool
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var p store.SavedViewPatch
	var err error
	if p.Name, err = textPtr("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if in.Filter != nil {
		if p.Filter, err = parseViewFilter(in.Filter); err != nil {
			return 0, nil, err
		}
	}
	if in.Shared != nil && *in.Shared && !q.role.Can(role.Edit) {
		return 0, nil, errForbidden
	}
	p.Shared = in.Shared
	v, err := s.store.UpdateSavedView(q.r.Context(), q.ws, q.mapID, q.user, q.id, p)
	return http.StatusOK, v, err
}

// deleteView removes the caller's own view; admins can also remove shared ones.
func (s *Server) deleteView(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteSavedView(q.r.Context(), q.ws, q.mapID, q.user, q.id, q.role.Can(role.ManageSettings))
}
