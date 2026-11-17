ALTER TABLE plans ADD COLUMN IF NOT EXISTS traffic_limit_bytes bigint;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS internal_squads text[];
ALTER TABLE plans ADD COLUMN IF NOT EXISTS external_squad_uuid text;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS gift_enabled boolean NOT NULL DEFAULT true;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS plan_profile jsonb;
CREATE OR REPLACE FUNCTION snapshot_order_plan() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.plan_profile IS NULL THEN
  SELECT jsonb_build_object('TrafficLimitBytes',traffic_limit_bytes,'InternalSquads',internal_squads,'ExternalSquadUUID',external_squad_uuid) INTO NEW.plan_profile FROM plans WHERE plan_id=NEW.plan_id;
 END IF;
 RETURN NEW;
END; $$;
CREATE OR REPLACE TRIGGER snapshot_order_plan BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION snapshot_order_plan();
CREATE TABLE IF NOT EXISTS subscription_gifts (
 id text PRIMARY KEY,
 buyer_id bigint NOT NULL REFERENCES accounts(id),
 purchase_order_id text NOT NULL UNIQUE REFERENCES orders(order_id),
 plan_id text NOT NULL,
 days integer NOT NULL CHECK(days>0),
 devices integer NOT NULL DEFAULT 0 CHECK(devices>=0),
 profile jsonb,
 recipient_id bigint REFERENCES accounts(id),
 redemption_order_id text UNIQUE REFERENCES orders(order_id),
 created_at timestamptz NOT NULL DEFAULT now(),
 claimed_at timestamptz
);
CREATE INDEX IF NOT EXISTS gifts_buyer_history ON subscription_gifts(buyer_id,created_at DESC);
CREATE TABLE IF NOT EXISTS job_health (
 name text PRIMARY KEY,
 last_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_success_at timestamptz,
 failures integer NOT NULL DEFAULT 0,
 alerted_at timestamptz
);
CREATE TABLE IF NOT EXISTS refund_cases (
 id bigserial PRIMARY KEY,
 order_id text NOT NULL UNIQUE REFERENCES orders(order_id),
 reason text NOT NULL,
 status text NOT NULL DEFAULT 'requested' CHECK(status IN ('requested','completed','rejected')),
 provider_reference text NOT NULL DEFAULT '',
 actor_id bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE broadcasts DROP CONSTRAINT IF EXISTS broadcasts_audience_check;
ALTER TABLE broadcasts ADD CONSTRAINT broadcasts_audience_check CHECK(audience IN ('all','active','inactive','trial_only','abandoned','expired','not_connected'));
CREATE INDEX IF NOT EXISTS orders_account_history ON orders(account_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS orders_provider_history ON orders(provider,created_at DESC,id DESC);

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS first_connected_at timestamptz;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS connection_checked_at timestamptz;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS connection_known boolean NOT NULL DEFAULT false;

ALTER TABLE job_health ADD COLUMN IF NOT EXISTS interval_seconds double precision NOT NULL DEFAULT 60;
