CREATE TABLE IF NOT EXISTS promo_reservations (
    order_id TEXT PRIMARY KEY REFERENCES orders(order_id) ON DELETE CASCADE,
    subject_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    code TEXT NOT NULL REFERENCES promocodes(code),
    consumed BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS promo_reservations_code_idx ON promo_reservations(code);

CREATE TABLE IF NOT EXISTS payment_inbox (
    event_key TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT
);
CREATE INDEX IF NOT EXISTS payment_inbox_pending_idx ON payment_inbox(retry_at)
    WHERE processed_at IS NULL;

ALTER TABLE reminder_log ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'sent';
ALTER TABLE reminder_log ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;
ALTER TABLE reminder_log ADD COLUMN IF NOT EXISTS claim TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_checked_at TIMESTAMPTZ;
