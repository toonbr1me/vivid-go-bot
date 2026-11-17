CREATE TABLE IF NOT EXISTS campaigns (
 id text PRIMARY KEY,
 actor_id bigint NOT NULL,
 title text NOT NULL,
 audience text NOT NULL CHECK(audience IN ('trial_only','abandoned','expired','not_connected')),
 message text NOT NULL,
 promo_code text NOT NULL DEFAULT '',
 enabled boolean NOT NULL DEFAULT false,
 revision integer NOT NULL DEFAULT 1,
 daily_limit integer NOT NULL DEFAULT 100 CHECK(daily_limit BETWEEN 1 AND 10000),
 cooldown_days integer NOT NULL DEFAULT 0 CHECK(cooldown_days=0 OR cooldown_days BETWEEN 7 AND 365),
 send_from integer NOT NULL DEFAULT 9 CHECK(send_from BETWEEN 0 AND 23),
 send_to integer NOT NULL DEFAULT 21 CHECK(send_to BETWEEN 1 AND 24 AND send_to>send_from),
 starts_at timestamptz NOT NULL DEFAULT now(),
 last_checked_at timestamptz,
 last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE broadcasts ADD COLUMN IF NOT EXISTS campaign_id text REFERENCES campaigns(id);
CREATE INDEX IF NOT EXISTS campaign_broadcasts ON broadcasts(campaign_id,created_at DESC) WHERE campaign_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS campaign_enrollments (
 campaign_id text NOT NULL REFERENCES campaigns(id),
 telegram_id bigint NOT NULL,
 last_enrolled_at timestamptz NOT NULL DEFAULT now(),
 last_broadcast_id text NOT NULL REFERENCES broadcasts(id),
 PRIMARY KEY(campaign_id,telegram_id)
);
CREATE INDEX IF NOT EXISTS campaign_daily_enrollments ON campaign_enrollments(campaign_id,last_enrolled_at);
CREATE TABLE IF NOT EXISTS campaign_contacts (
 telegram_id bigint PRIMARY KEY,
 next_allowed_at timestamptz NOT NULL,
 last_attempt_at timestamptz,
 last_attempt_broadcast_id text REFERENCES broadcasts(id)
);
ALTER TABLE campaign_contacts ADD COLUMN IF NOT EXISTS last_attempt_at timestamptz;
ALTER TABLE campaign_contacts ADD COLUMN IF NOT EXISTS last_attempt_broadcast_id text REFERENCES broadcasts(id);
CREATE TABLE IF NOT EXISTS campaign_send_slots (
 broadcast_id text NOT NULL REFERENCES broadcasts(id),
 telegram_id bigint NOT NULL,
 campaign_id text NOT NULL REFERENCES campaigns(id),
 reserved_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(broadcast_id,telegram_id)
);
CREATE INDEX IF NOT EXISTS campaign_send_window ON campaign_send_slots(campaign_id,reserved_at);
ALTER TABLE broadcast_deliveries DROP CONSTRAINT IF EXISTS broadcast_deliveries_status_check;
ALTER TABLE broadcast_deliveries ADD CONSTRAINT broadcast_deliveries_status_check CHECK(status IN ('pending','sending','sent','failed','uncertain','skipped'));
