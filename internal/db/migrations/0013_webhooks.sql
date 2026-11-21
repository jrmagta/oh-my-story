-- +goose Up

-- Outgoing webhooks. A delivery is queued in the same transaction as the
-- event it reports (see appendEvents), so none is lost or doubled; senders
-- claim due ones with FOR UPDATE SKIP LOCKED.
CREATE TABLE webhooks (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    url          text NOT NULL,
    secret       text NOT NULL,
    events       text[] NOT NULL, -- entity.action names, or '*' for all
    active       boolean NOT NULL DEFAULT true,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhooks_workspace_id ON webhooks (workspace_id) WHERE active;

CREATE TABLE webhook_deliveries (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    webhook_id       uuid NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    event_id         bigint REFERENCES events (id) ON DELETE CASCADE, -- null for a test ping
    event_type       text NOT NULL,
    status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts         integer NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    last_status_code integer,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    delivered_at     timestamptz
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_webhook ON webhook_deliveries (webhook_id, id DESC);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;
