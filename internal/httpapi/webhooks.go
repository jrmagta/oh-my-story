package httpapi

import (
	"errors"
	"net/http"
	"slices"

	"github.com/jrmagta/oh-my-story/internal/auth"
	"github.com/jrmagta/oh-my-story/internal/store"
	"github.com/jrmagta/oh-my-story/internal/webhook"
)

// webhookEvents checks a list of event types: "*", or known types, no repeats, at least one.
func webhookEvents(events []string) ([]string, error) {
	if len(events) == 0 {
		return nil, errBadRequest(`events must list at least one event type, or "*" for all`)
	}
	seen := map[string]bool{}
	for _, e := range events {
		if e != "*" && !slices.Contains(store.WebhookEvents, e) {
			return nil, errBadRequest("unknown event type: " + e)
		}
		if seen[e] {
			return nil, errBadRequest("events lists " + e + " twice")
		}
		seen[e] = true
	}
	return events, nil
}

func (s *Server) checkWebhookURL(raw string) error {
	if err := webhook.CheckURL(raw, s.cfg.BlockPrivateWebhooks); err != nil {
		return errBadRequest(err.Error())
	}
	return nil
}

func (s *Server) listWebhooks(q mapRequest) (int, any, error) {
	out, err := s.store.Webhooks(q.r.Context(), q.ws)
	return http.StatusOK, out, err
}

// createWebhook adds a webhook and returns its signing secret, in this reply only.
func (s *Server) createWebhook(q mapRequest) (int, any, error) {
	var in struct {
		URL    string
		Events []string
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	if err := s.checkWebhookURL(in.URL); err != nil {
		return 0, nil, err
	}
	events, err := webhookEvents(in.Events)
	if err != nil {
		return 0, nil, err
	}
	secret, _, err := auth.NewToken()
	if err != nil {
		return 0, nil, err
	}
	secret = "whsec_" + secret
	w, err := s.store.CreateWebhook(q.r.Context(), q.user, q.ws, in.URL, secret, events)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, struct {
		store.Webhook
		Secret string `json:"secret"`
	}{w, secret}, nil
}

func (s *Server) patchWebhook(q mapRequest) (int, any, error) {
	var in struct {
		URL    *string
		Events *[]string
		Active *bool
	}
	if err := q.read(&in); err != nil {
		return 0, nil, err
	}
	if in.URL != nil {
		if err := s.checkWebhookURL(*in.URL); err != nil {
			return 0, nil, err
		}
	}
	if in.Events != nil {
		if _, err := webhookEvents(*in.Events); err != nil {
			return 0, nil, err
		}
	}
	w, err := s.store.UpdateWebhook(q.r.Context(), q.ws, q.id, store.WebhookPatch{URL: in.URL, Events: in.Events, Active: in.Active})
	return http.StatusOK, w, err
}

func (s *Server) deleteWebhook(q mapRequest) (int, any, error) {
	return 0, nil, s.store.DeleteWebhook(q.r.Context(), q.ws, q.id)
}

// listDeliveries returns the webhook's latest 50 deliveries, newest first.
func (s *Server) listDeliveries(q mapRequest) (int, any, error) {
	out, err := s.store.Deliveries(q.r.Context(), q.ws, q.id, 50)
	return http.StatusOK, out, err
}

// testWebhook queues a "ping" delivery, sent within a few seconds.
func (s *Server) testWebhook(q mapRequest) (int, any, error) {
	d, err := s.store.QueueTestDelivery(q.r.Context(), q.ws, q.id)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil, err
	}
	return http.StatusCreated, d, err
}
