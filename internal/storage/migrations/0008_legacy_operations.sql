CREATE TABLE IF NOT EXISTS support_compensations (
 ticket_id bigint PRIMARY KEY REFERENCES support_tickets(id),
 actor_id bigint NOT NULL,
 subject_id bigint NOT NULL REFERENCES accounts(id),
 order_id text NOT NULL UNIQUE REFERENCES orders(order_id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS compensation_audit_window ON staff_audit(actor_id,created_at) WHERE action='client.compensate';

CREATE TABLE IF NOT EXISTS broadcasts (
 id text PRIMARY KEY,
 actor_id bigint NOT NULL,
 audience text NOT NULL CHECK(audience IN ('all','active','inactive')),
 message text NOT NULL,
 promo_code text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','queued','sending','completed','cancelled')),
 created_at timestamptz NOT NULL DEFAULT now(),
 confirmed_at timestamptz
);
CREATE TABLE IF NOT EXISTS broadcast_deliveries (
 broadcast_id text NOT NULL REFERENCES broadcasts(id) ON DELETE CASCADE,
 telegram_id bigint NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','failed','uncertain')),
 available_at timestamptz NOT NULL DEFAULT now(),
 claimed_at timestamptz,
 claim_token text NOT NULL DEFAULT '',
 PRIMARY KEY(broadcast_id,telegram_id)
);
CREATE INDEX IF NOT EXISTS broadcast_pending ON broadcast_deliveries(available_at) WHERE status='pending';

CREATE TABLE IF NOT EXISTS panel_events (
 id text PRIMARY KEY,
 event text NOT NULL,
 telegram_id bigint,
 device_label text NOT NULL DEFAULT '',
 status text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 claimed_at timestamptz,
 claim_token text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS panel_pending ON panel_events(created_at) WHERE status='pending';
ALTER TABLE panel_events ADD COLUMN IF NOT EXISTS available_at timestamptz NOT NULL DEFAULT now();
CREATE TABLE IF NOT EXISTS panel_notification_cooldowns (
 telegram_id bigint PRIMARY KEY,
 next_allowed_at timestamptz NOT NULL
);
