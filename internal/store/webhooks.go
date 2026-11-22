package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WebhookEvents are the event types a webhook can ask for (entity.action),
// besides "*" for all of them.
var WebhookEvents = []string{
	"map.create", "map.update", "map.delete",
	"node.create", "node.update", "node.move", "node.delete", "node.restore",
	"release.create", "release.update", "release.move", "release.delete",
	"step.create", "step.update", "step.delete", "step.restore", "step.place", "step.unplace",
	"placement.move",
	"dependency.create", "dependency.delete",
	"comment.create", "comment.update", "comment.delete",
	"attachment.create", "attachment.delete", "attachment.restore",
	"label.create", "label.update", "label.delete",
	"persona.create", "persona.update", "persona.delete",
	"workflow.create", "workflow.update", "status.create", "status.update", "status.move", "status.delete",
}

// Webhook is an outgoing webhook as admins see it. The secret is shown once, when made.
type Webhook struct {
	ID        uuid.UUID `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
}

// Delivery is one attempt-tracked send of an event (or a test ping) to a webhook.
type Delivery struct {
	ID             int64      `json:"id"`
	Type           string     `json:"type"`
	Status         string     `json:"status"` // pending, sent or failed
	Attempts       int        `json:"attempts"`
	LastStatusCode *int       `json:"lastStatusCode"`
	LastError      *string    `json:"lastError"`
	NextAttemptAt  *time.Time `json:"nextAttemptAt"` // pending only
	CreatedAt      time.Time  `json:"createdAt"`
	DeliveredAt    *time.Time `json:"deliveredAt"`
}

const webhookCols = `id, url, events, active, created_at`

func scanWebhook(row pgx.Row) (Webhook, error) {
	var w Webhook
	err := row.Scan(&w.ID, &w.URL, &w.Events, &w.Active, &w.CreatedAt)
	return w, notFound(err)
}

func (s *Store) Webhooks(ctx context.Context, workspaceID uuid.UUID) ([]Webhook, error) {
	return collect(ctx, s.pool, scanWebhook,
		`SELECT `+webhookCols+` FROM webhooks WHERE workspace_id = $1 ORDER BY created_at, id`, workspaceID)
}

func (s *Store) CreateWebhook(ctx context.Context, actor, workspaceID uuid.UUID, url, secret string, events []string) (Webhook, error) {
	return scanWebhook(s.pool.QueryRow(ctx,
		`INSERT INTO webhooks (id, workspace_id, url, secret, events, created_by) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+webhookCols, newID(), workspaceID, url, secret, events, actor))
}

type WebhookPatch struct {
	URL    *string
	Events *[]string
	Active *bool
}

func (s *Store) UpdateWebhook(ctx context.Context, workspaceID, id uuid.UUID, p WebhookPatch) (Webhook, error) {
	return scanWebhook(s.pool.QueryRow(ctx,
		`UPDATE webhooks SET url = COALESCE($3, url), events = COALESCE($4, events), active = COALESCE($5, active)
		 WHERE id = $1 AND workspace_id = $2 RETURNING `+webhookCols, id, workspaceID, p.URL, p.Events, p.Active))
}

func (s *Store) DeleteWebhook(ctx context.Context, workspaceID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	return err
}

const deliveryCols = `id, event_type, status, attempts, last_status_code, last_error,
	CASE WHEN status = 'pending' THEN next_attempt_at END, created_at, delivered_at`

func scanDelivery(row pgx.Row) (Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.Type, &d.Status, &d.Attempts, &d.LastStatusCode, &d.LastError, &d.NextAttemptAt, &d.CreatedAt, &d.DeliveredAt)
	return d, notFound(err)
}

// Deliveries lists a webhook's latest deliveries, newest first.
func (s *Store) Deliveries(ctx context.Context, workspaceID, webhookID uuid.UUID, limit int) ([]Delivery, error) {
	if _, err := scanWebhook(s.pool.QueryRow(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE id = $1 AND workspace_id = $2`,
		webhookID, workspaceID)); err != nil {
		return nil, err
	}
	return collect(ctx, s.pool, scanDelivery,
		`SELECT `+deliveryCols+` FROM webhook_deliveries WHERE webhook_id = $1 ORDER BY id DESC LIMIT $2`, webhookID, limit)
}

// QueueTestDelivery queues a "ping" to the webhook, sent like any delivery.
func (s *Store) QueueTestDelivery(ctx context.Context, workspaceID, webhookID uuid.UUID) (Delivery, error) {
	return scanDelivery(s.pool.QueryRow(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, event_type)
		 SELECT id, 'ping' FROM webhooks WHERE id = $1 AND workspace_id = $2
		 RETURNING `+deliveryCols, webhookID, workspaceID))
}

// OutgoingDelivery is a claimed delivery, ready to send.
type OutgoingDelivery struct {
	ID       int64
	URL      string
	Secret   string
	Type     string
	Attempts int // sends tried before this one
	Body     []byte
}

// deliveryBody is what a webhook receives.
type deliveryBody struct {
	ID        int64      `json:"id"`
	Type      string     `json:"type"`
	Workspace deliveryWS `json:"workspace"`
	Map       *named     `json:"map"`
	Actor     *actor     `json:"actor"`
	Event     *Event     `json:"event"`
}

type deliveryWS struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
}

type named struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type actor struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Kind string    `json:"kind"` // user or token
}

// ClaimDeliveries takes up to n due deliveries for sending. Each is leased
// for lease (pushed that far into the future), so another sender skips it;
// if this one stops before recording a result, it comes due again.
func (s *Store) ClaimDeliveries(ctx context.Context, n int, lease time.Duration) ([]OutgoingDelivery, error) {
	var out []OutgoingDelivery
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`WITH due AS (
			   SELECT id FROM webhook_deliveries WHERE status = 'pending' AND next_attempt_at <= now()
			   ORDER BY next_attempt_at, id LIMIT $1 FOR UPDATE SKIP LOCKED)
			 UPDATE webhook_deliveries d SET next_attempt_at = now() + make_interval(secs => $2) FROM due WHERE d.id = due.id
			 RETURNING d.id, d.webhook_id, d.event_id, d.event_type, d.attempts`, n, lease.Seconds())
		if err != nil {
			return err
		}
		type claim struct {
			id       int64
			webhook  uuid.UUID
			event    *int64
			typ      string
			attempts int
		}
		claims, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (claim, error) {
			var c claim
			err := r.Scan(&c.id, &c.webhook, &c.event, &c.typ, &c.attempts)
			return c, err
		})
		if err != nil {
			return err
		}
		for _, c := range claims {
			d := OutgoingDelivery{ID: c.id, Type: c.typ, Attempts: c.attempts}
			b := deliveryBody{ID: c.id, Type: c.typ}
			if err := tx.QueryRow(ctx,
				`SELECT h.url, h.secret, w.id, w.slug, w.name FROM webhooks h JOIN workspaces w ON w.id = h.workspace_id WHERE h.id = $1`,
				c.webhook).Scan(&d.URL, &d.Secret, &b.Workspace.ID, &b.Workspace.Slug, &b.Workspace.Name); err != nil {
				return err
			}
			if c.event != nil {
				e := Event{}
				var kind string
				var actorName *string
				var mapName *string
				if err := tx.QueryRow(ctx,
					`SELECT e.id, e.map_id, e.actor_id, e.entity_type, e.entity_id, e.action, e.before, e.after, e.created_at,
					        e.actor_kind, u.display_name, m.name
					 FROM events e LEFT JOIN users u ON u.id = e.actor_id LEFT JOIN maps m ON m.id = e.map_id
					 WHERE e.id = $1`, *c.event).
					Scan(&e.ID, &e.MapID, &e.ActorID, &e.Entity, &e.EntityID, &e.Action, &e.Before, &e.After, &e.At,
						&kind, &actorName, &mapName); err != nil {
					return err
				}
				b.Event = &e
				if e.MapID != nil && mapName != nil {
					b.Map = &named{ID: *e.MapID, Name: *mapName}
				}
				if e.ActorID != nil && actorName != nil {
					b.Actor = &actor{ID: *e.ActorID, Name: *actorName, Kind: kind}
				}
			}
			if d.Body, err = json.Marshal(b); err != nil {
				return err
			}
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// RecordDelivery stores how a send went. On failure the delivery comes due
// again after retry (zero: at once), or is marked failed when giveUp is set.
func (s *Store) RecordDelivery(ctx context.Context, id int64, ok bool, code *int, errText *string, giveUp bool, retry time.Duration) error {
	var err error
	switch {
	case ok:
		_, err = s.pool.Exec(ctx,
			`UPDATE webhook_deliveries SET status = 'sent', attempts = attempts + 1, last_status_code = $2, last_error = NULL,
			   delivered_at = now() WHERE id = $1`, id, code)
	case !giveUp:
		_, err = s.pool.Exec(ctx,
			`UPDATE webhook_deliveries SET attempts = attempts + 1, last_status_code = $2, last_error = $3,
			   next_attempt_at = now() + make_interval(secs => $4) WHERE id = $1`, id, code, errText, retry.Seconds())
	default:
		_, err = s.pool.Exec(ctx,
			`UPDATE webhook_deliveries SET status = 'failed', attempts = attempts + 1, last_status_code = $2, last_error = $3
			 WHERE id = $1`, id, code, errText)
	}
	if err != nil {
		return fmt.Errorf("record delivery %d: %w", id, err)
	}
	return nil
}

// PruneDeliveries drops finished deliveries older than age.
func (s *Store) PruneDeliveries(ctx context.Context, age time.Duration) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM webhook_deliveries WHERE status <> 'pending' AND created_at < now() - make_interval(secs => $1)`, age.Seconds())
	return err
}
