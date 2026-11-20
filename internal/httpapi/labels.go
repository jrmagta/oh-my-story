package httpapi

import (
	"net/http"

	"github.com/jrmagta/oh-my-story/internal/store"
)

const (
	maxLabelName    = 50
	maxStepLabels   = 20
	maxNodePersonas = 20
	defaultColour   = "#8A8A93"
)

func (s *Server) listLabels(q mapRequest) (int, any, error) {
	ls, err := s.store.Labels(q.r.Context(), q.ws)
	return http.StatusOK, ls, err
}

// createLabel answers 409 conflict when a label of that name exists, ignoring case.
func (s *Server) createLabel(q mapRequest) (int, any, error) {
	var in struct {
		Name   string
		Colour *string
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	name, err := text("name", in.Name, maxLabelName, false)
	if err != nil {
		return 0, nil, err
	}
	colour, err := parseColour(in.Colour)
	if err != nil {
		return 0, nil, err
	}
	if colour == nil {
		c := defaultColour
		colour = &c
	}
	l, err := s.store.CreateLabel(q.r.Context(), q.user, q.ws, name, *colour)
	return http.StatusCreated, l, err
}

func (s *Server) patchLabel(q mapRequest) (int, any, error) {
	var in struct{ Name, Colour *string }
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var p store.LabelPatch
	var err error
	if p.Name, err = textPtr("name", in.Name, maxLabelName, false); err != nil {
		return 0, nil, err
	}
	if p.Colour, err = parseColour(in.Colour); err != nil {
		return 0, nil, err
	}
	l, err := s.store.UpdateLabel(q.r.Context(), q.user, q.ws, q.id, p)
	return http.StatusOK, l, err
}

func (s *Server) deleteLabel(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteLabel(q.r.Context(), q.user, q.ws, q.id)
}
