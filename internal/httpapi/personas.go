package httpapi

import (
	"net/http"

	"github.com/jrmagta/oh-my-story/internal/store"
)

const maxPersonaDescription = 2000

func (s *Server) listPersonas(q mapRequest) (int, any, error) {
	ps, err := s.store.Personas(q.r.Context(), q.ws)
	return http.StatusOK, ps, err
}

// createPersona answers 409 conflict when a persona of that name exists, ignoring case.
func (s *Server) createPersona(q mapRequest) (int, any, error) {
	var in struct {
		Name, Description string
		Colour            *string
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	name, err := text("name", in.Name, maxName, false)
	if err != nil {
		return 0, nil, err
	}
	desc, err := text("description", in.Description, maxPersonaDescription, true)
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
	p, err := s.store.CreatePersona(q.r.Context(), q.user, q.ws, name, desc, *colour)
	return http.StatusCreated, p, err
}

func (s *Server) patchPersona(q mapRequest) (int, any, error) {
	var in struct{ Name, Description, Colour *string }
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	var p store.PersonaPatch
	var err error
	if p.Name, err = textPtr("name", in.Name, maxName, false); err != nil {
		return 0, nil, err
	}
	if p.Description, err = textPtr("description", in.Description, maxPersonaDescription, true); err != nil {
		return 0, nil, err
	}
	if p.Colour, err = parseColour(in.Colour); err != nil {
		return 0, nil, err
	}
	out, err := s.store.UpdatePersona(q.r.Context(), q.user, q.ws, q.id, p)
	return http.StatusOK, out, err
}

func (s *Server) deletePersona(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeletePersona(q.r.Context(), q.user, q.ws, q.id)
}
