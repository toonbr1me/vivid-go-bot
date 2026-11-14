ALTER TABLE orders ADD COLUMN IF NOT EXISTS recurring_agreement_id TEXT;
CREATE TABLE IF NOT EXISTS recurring_agreements (
 id TEXT PRIMARY KEY,
 subject_id BIGINT NOT NULL REFERENCES accounts(id),
 provider TEXT NOT NULL,
 method TEXT NOT NULL,
 plan_id TEXT NOT NULL,
 amount_cents BIGINT NOT NULL CHECK(amount_cents > 0),
 currency TEXT NOT NULL,
 days INTEGER NOT NULL CHECK(days > 0),
 device_count INTEGER,
 external_id TEXT NOT NULL DEFAULT '',
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 activated BOOLEAN NOT NULL DEFAULT FALSE,
 next_charge_at TIMESTAMPTZ,
 pending_order_id TEXT,
 last_error TEXT NOT NULL DEFAULT '',
 consent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS recurring_one_enabled ON recurring_agreements(subject_id) WHERE enabled;
CREATE UNIQUE INDEX IF NOT EXISTS recurring_external ON recurring_agreements(provider,external_id) WHERE external_id <> '';
CREATE TABLE IF NOT EXISTS recurring_charge_links (
 provider TEXT NOT NULL,
 transaction_id TEXT NOT NULL,
 order_id TEXT NOT NULL UNIQUE REFERENCES orders(order_id),
 PRIMARY KEY(provider,transaction_id)
);
ALTER TABLE bot_ui_preferences ADD COLUMN IF NOT EXISTS checkout_plan TEXT NOT NULL DEFAULT '';
ALTER TABLE bot_ui_preferences ADD COLUMN IF NOT EXISTS checkout_recurring BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE bot_ui_preferences ADD COLUMN IF NOT EXISTS native_message_id INTEGER NOT NULL DEFAULT 0;
