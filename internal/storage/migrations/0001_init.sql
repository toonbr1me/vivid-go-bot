-- Схема приложения. Все инструкции идемпотентны: миграция выполняется при
-- каждом старте и на уже существующей базе Python-версии ничего не ломает.

CREATE TABLE IF NOT EXISTS accounts (
    id BIGSERIAL PRIMARY KEY,
    google_sub TEXT UNIQUE,
    email TEXT,
    name TEXT,
    telegram_id BIGINT UNIQUE,
    remnawave_username TEXT,
    source TEXT NOT NULL DEFAULT 'web',
    ref_code TEXT,
    merged_into BIGINT REFERENCES accounts(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS accounts_email_idx ON accounts (email);
CREATE UNIQUE INDEX IF NOT EXISTS accounts_ref_code_uidx ON accounts (ref_code) WHERE ref_code IS NOT NULL;
-- На базе, оставшейся от прошлой версии, таблица уже создана: CREATE TABLE
-- IF NOT EXISTS ничего не добавит, а код этими колонками пользуется.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'web';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS ref_code TEXT;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS merged_into BIGINT REFERENCES accounts(id);

CREATE TABLE IF NOT EXISTS orders (
    id BIGSERIAL PRIMARY KEY,
    order_id TEXT UNIQUE NOT NULL,
    provider TEXT NOT NULL,
    telegram_id BIGINT,
    account_id BIGINT REFERENCES accounts(id),
    username TEXT,
    plan_id TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'subscription',
    amount NUMERIC(12, 2) NOT NULL,
    original_amount NUMERIC(12, 2),
    currency TEXT NOT NULL,
    payment_method TEXT,
    status TEXT NOT NULL,
    payment_link_id TEXT,
    payment_url TEXT,
    payment_expires_at TIMESTAMPTZ,
    transaction_id TEXT,
    promo_code TEXT,
    plan_days INT,
    promo_bonus_days INT,
    granted_days INT,
    referrer_id BIGINT,
    partner_id BIGINT,
    reward_type TEXT,
    reward_value NUMERIC(12, 2) NOT NULL DEFAULT 0,
    reward_scope TEXT,
    device_count INT,
    device_days INT,
    device_expires_at TIMESTAMPTZ,
    renew_grant_id TEXT,
    device_renewals JSONB,
    device_renewal_amount NUMERIC(12, 2) NOT NULL DEFAULT 0,
    fulfillment_version INT NOT NULL DEFAULT 0,
    fulfillment_attempts INT NOT NULL DEFAULT 0,
    fulfillment_error TEXT,
    fulfillment_retry_at TIMESTAMPTZ,
    fulfillment_subject_ids BIGINT[],
    notification_result JSONB,
    notifications_completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at TIMESTAMPTZ,
    provisioned_at TIMESTAMPTZ,
    expire_at TIMESTAMPTZ
);
-- Колонки выше перечислены в CREATE; на базе, созданной раньше, их добавит
-- ALTER. Порядок и состав совпадают с Python-схемой, поэтому старая база
-- работает как есть.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_method TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_expires_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS referrer_id BIGINT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS promo_code TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS plan_days INT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS promo_bonus_days INT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS fulfillment_version INT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS fulfillment_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS fulfillment_error TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS fulfillment_retry_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS fulfillment_subject_ids BIGINT[];
ALTER TABLE orders ADD COLUMN IF NOT EXISTS notification_result JSONB;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS notifications_completed_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS account_id BIGINT;
ALTER TABLE orders ALTER COLUMN telegram_id DROP NOT NULL;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS original_amount NUMERIC(12,2);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS partner_id BIGINT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS reward_type TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS reward_value NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS reward_scope TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS device_days INT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS device_expires_at TIMESTAMPTZ;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS renew_grant_id TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'subscription';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS device_count INTEGER;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS device_renewals JSONB;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS device_renewal_amount NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS granted_days INTEGER;
-- Идентификаторы платежей у разных шлюзов не UUID: Platega присылает число,
-- ЮKassa — свою строку. Текстовые колонки снимают лишнее преобразование и
-- не дают вебхуку упасть на валидации типа.
ALTER TABLE orders ALTER COLUMN transaction_id TYPE TEXT USING transaction_id::text;
ALTER TABLE orders ALTER COLUMN payment_link_id TYPE TEXT USING payment_link_id::text;

CREATE INDEX IF NOT EXISTS orders_telegram_id_idx ON orders (telegram_id);
CREATE INDEX IF NOT EXISTS orders_account_idx ON orders (account_id);
CREATE INDEX IF NOT EXISTS orders_status_idx ON orders (status);
CREATE INDEX IF NOT EXISTS orders_reminder_expire_idx ON orders (expire_at)
    WHERE telegram_id IS NOT NULL AND expire_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS orders_fulfillment_pending_idx ON orders (id)
    WHERE status = 'paid' AND provisioned_at IS NULL AND fulfillment_version = 1;
CREATE INDEX IF NOT EXISTS orders_notifications_pending_idx ON orders (id)
    WHERE status = 'paid' AND provisioned_at IS NOT NULL
      AND notification_result IS NOT NULL AND notifications_completed_at IS NULL;

CREATE TABLE IF NOT EXISTS order_fulfillment_steps (
    order_id TEXT NOT NULL REFERENCES orders(order_id),
    step TEXT NOT NULL,
    result JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (order_id, step)
);

CREATE TABLE IF NOT EXISTS order_notification_log (
    order_id TEXT NOT NULL REFERENCES orders(order_id) ON DELETE CASCADE,
    recipient_id BIGINT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'sent',
    lease_until TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    attempts INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (order_id, recipient_id, kind)
);
ALTER TABLE order_notification_log ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'sent';
ALTER TABLE order_notification_log ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;
ALTER TABLE order_notification_log ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ;
ALTER TABLE order_notification_log ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS trials (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    kind TEXT NOT NULL DEFAULT 'bot',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE trials ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE trials ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'bot';
ALTER TABLE trials ALTER COLUMN telegram_id DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS trials_bot_telegram_uidx
    ON trials (telegram_id) WHERE kind = 'bot';
-- Один пробный период каждого вида на субъекта. Перед индексом убираем
-- возможные дубли из базы, созданной до этого ограничения: иначе миграция
-- упала бы на живой установке.
DELETE FROM trials a USING trials b
    WHERE a.id > b.id AND a.subject_id IS NOT NULL
      AND a.subject_id = b.subject_id AND a.kind = b.kind;
CREATE UNIQUE INDEX IF NOT EXISTS trials_subject_kind_uidx
    ON trials (subject_id, kind) WHERE subject_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS promo_activations (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    code TEXT NOT NULL,
    used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE promo_activations ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE promo_activations ALTER COLUMN telegram_id DROP NOT NULL;
CREATE INDEX IF NOT EXISTS promo_activations_tg_idx ON promo_activations (telegram_id);

CREATE TABLE IF NOT EXISTS promocodes (
    id BIGSERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    discount_percent INT NOT NULL CHECK (discount_percent >= 0 AND discount_percent <= 100),
    discount_fixed NUMERIC(12,2) NOT NULL DEFAULT 0,
    repeat_discount_percent INT NOT NULL DEFAULT 0,
    repeat_discount_fixed NUMERIC(12,2) NOT NULL DEFAULT 0,
    bonus_days INT NOT NULL DEFAULT 0,
    grant_days INT NOT NULL DEFAULT 0,
    max_uses INT NOT NULL DEFAULT 0,
    used_count INT NOT NULL DEFAULT 0,
    per_user_limit INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    partner_id BIGINT,
    reward_type TEXT,
    reward_value NUMERIC(12,2) NOT NULL DEFAULT 0,
    reward_scope TEXT NOT NULL DEFAULT 'all',
    starts_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    plan_ids TEXT[],
    created_by BIGINT,
    visit_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS bonus_days INT NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS discount_fixed NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS repeat_discount_percent INT NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS repeat_discount_fixed NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS reward_type TEXT;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS reward_value NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS reward_scope TEXT NOT NULL DEFAULT 'all';
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS per_user_limit INT NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS plan_ids TEXT[];
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS created_by BIGINT;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS visit_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS grant_days INT NOT NULL DEFAULT 0;
ALTER TABLE promocodes ADD COLUMN IF NOT EXISTS partner_id BIGINT;

CREATE TABLE IF NOT EXISTS promo_intents (
    id BIGSERIAL PRIMARY KEY,
    subject_id BIGINT NOT NULL REFERENCES accounts(id),
    code TEXT NOT NULL,
    entered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    consumed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS promo_intents_subject_idx ON promo_intents (subject_id, id DESC);

CREATE TABLE IF NOT EXISTS referrals (
    id BIGSERIAL PRIMARY KEY,
    referrer_id BIGINT,
    referred_id BIGINT UNIQUE,
    referrer_subject_id BIGINT REFERENCES accounts(id),
    referred_subject_id BIGINT REFERENCES accounts(id),
    bonus_days INT NOT NULL DEFAULT 7,
    reward_given BOOLEAN NOT NULL DEFAULT FALSE,
    reward_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE referrals ALTER COLUMN referrer_id DROP NOT NULL;
ALTER TABLE referrals ALTER COLUMN referred_id DROP NOT NULL;
ALTER TABLE referrals ADD COLUMN IF NOT EXISTS referrer_subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE referrals ADD COLUMN IF NOT EXISTS referred_subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE referrals ADD COLUMN IF NOT EXISTS reward_count INT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS pending_referral_bonuses (
    referrer_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    bonus_days INT NOT NULL DEFAULT 0
);
ALTER TABLE pending_referral_bonuses ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE pending_referral_bonuses DROP CONSTRAINT IF EXISTS pending_referral_bonuses_pkey;
ALTER TABLE pending_referral_bonuses ALTER COLUMN referrer_id DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS pending_referral_bonuses_referrer_uidx ON pending_referral_bonuses (referrer_id);
CREATE UNIQUE INDEX IF NOT EXISTS pending_referral_bonuses_subject_uidx ON pending_referral_bonuses (subject_id);

CREATE TABLE IF NOT EXISTS user_balance (
    telegram_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    balance NUMERIC(12,2) NOT NULL DEFAULT 0
);
ALTER TABLE user_balance ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE user_balance DROP CONSTRAINT IF EXISTS user_balance_pkey;
ALTER TABLE user_balance ALTER COLUMN telegram_id DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS user_balance_telegram_uidx ON user_balance (telegram_id);
CREATE UNIQUE INDEX IF NOT EXISTS user_balance_subject_uidx ON user_balance (subject_id);

CREATE TABLE IF NOT EXISTS user_referral_mode (
    telegram_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    mode TEXT NOT NULL DEFAULT ''
);
ALTER TABLE user_referral_mode ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE user_referral_mode DROP CONSTRAINT IF EXISTS user_referral_mode_pkey;
ALTER TABLE user_referral_mode ALTER COLUMN telegram_id DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS user_referral_mode_telegram_uidx ON user_referral_mode (telegram_id);
CREATE UNIQUE INDEX IF NOT EXISTS user_referral_mode_subject_uidx ON user_referral_mode (subject_id);

CREATE TABLE IF NOT EXISTS withdrawal_requests (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    username TEXT,
    amount NUMERIC(12,2) NOT NULL,
    method TEXT NOT NULL,
    recipient_details TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE withdrawal_requests ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE withdrawal_requests ALTER COLUMN telegram_id DROP NOT NULL;
CREATE INDEX IF NOT EXISTS withdrawal_requests_status_idx ON withdrawal_requests (status);

CREATE TABLE IF NOT EXISTS reminder_log (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL,
    kind TEXT NOT NULL,
    target_at TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (telegram_id, kind, target_at)
);
CREATE INDEX IF NOT EXISTS reminder_log_sent_idx ON reminder_log (sent_at DESC);

CREATE TABLE IF NOT EXISTS notification_settings (
    telegram_id BIGINT PRIMARY KEY,
    expiry_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    traffic_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    activity_notifications BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS support_tickets (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    subject TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    assigned_to BIGINT,
    assigned_at TIMESTAMPTZ,
    locked_until TIMESTAMPTZ,
    work_state TEXT NOT NULL DEFAULT 'new',
    snooze_until TIMESTAMPTZ,
    support_mode TEXT NOT NULL DEFAULT 'human',
    ai_cursor BIGINT NOT NULL DEFAULT 0,
    ai_replies INTEGER NOT NULL DEFAULT 0,
    ai_claim TEXT,
    ai_locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS assigned_to BIGINT;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS assigned_at TIMESTAMPTZ;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS work_state TEXT NOT NULL DEFAULT 'new';
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS snooze_until TIMESTAMPTZ;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS support_mode TEXT NOT NULL DEFAULT 'human';
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS ai_cursor BIGINT NOT NULL DEFAULT 0;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS ai_replies INTEGER NOT NULL DEFAULT 0;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS ai_claim TEXT;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS ai_locked_until TIMESTAMPTZ;
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE support_tickets ALTER COLUMN telegram_id DROP NOT NULL;
CREATE INDEX IF NOT EXISTS tickets_status_idx ON support_tickets (status);
CREATE INDEX IF NOT EXISTS tickets_tg_idx ON support_tickets (telegram_id);
CREATE INDEX IF NOT EXISTS tickets_subject_idx ON support_tickets (subject_id);
CREATE INDEX IF NOT EXISTS tickets_queue_idx ON support_tickets (work_state, created_at) WHERE status = 'open';

CREATE TABLE IF NOT EXISTS support_messages (
    id BIGSERIAL PRIMARY KEY,
    ticket_id BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    telegram_id BIGINT,
    subject_id BIGINT REFERENCES accounts(id),
    message TEXT NOT NULL,
    is_from_admin BOOLEAN NOT NULL DEFAULT FALSE,
    is_internal BOOLEAN NOT NULL DEFAULT FALSE,
    is_assistant BOOLEAN NOT NULL DEFAULT FALSE,
    file_id TEXT,
    file_type TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS is_internal BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS file_id TEXT;
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS file_type TEXT;
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS is_assistant BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE support_messages ADD COLUMN IF NOT EXISTS subject_id BIGINT REFERENCES accounts(id);
ALTER TABLE support_messages ALTER COLUMN telegram_id DROP NOT NULL;
CREATE INDEX IF NOT EXISTS messages_ticket_idx ON support_messages (ticket_id);

CREATE TABLE IF NOT EXISTS support_notifications (
    ticket_id BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    staff_id BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    PRIMARY KEY (ticket_id, staff_id)
);

CREATE TABLE IF NOT EXISTS web_support_requests (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    contact TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'new',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS web_support_requests_status_idx ON web_support_requests (status);

CREATE TABLE IF NOT EXISTS device_limit_sync_state (
    subject_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    panel_user_id TEXT NOT NULL,
    last_limit INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS staff (
    telegram_id BIGINT PRIMARY KEY,
    role TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    added_by BIGINT,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS staff_audit (
    id BIGSERIAL PRIMARY KEY,
    actor_id BIGINT NOT NULL,
    action TEXT NOT NULL,
    target TEXT,
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    details TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS staff_audit_created_idx ON staff_audit (created_at DESC);
CREATE INDEX IF NOT EXISTS staff_audit_actor_idx ON staff_audit (actor_id, action, created_at DESC);

CREATE TABLE IF NOT EXISTS web_sessions (
    token_hash TEXT PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS web_sessions_account_idx ON web_sessions (account_id);
CREATE INDEX IF NOT EXISTS web_sessions_expires_idx ON web_sessions (expires_at);

CREATE TABLE IF NOT EXISTS telegram_link_codes (
    code TEXT PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS telegram_link_codes_account_idx ON telegram_link_codes (account_id);

CREATE TABLE IF NOT EXISTS partners (
    id BIGSERIAL PRIMARY KEY,
    subject_id BIGINT REFERENCES accounts(id),
    name TEXT NOT NULL,
    contact TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    payout_details TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS partners_subject_uidx ON partners (subject_id) WHERE subject_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS partner_attributions (
    subject_id BIGINT PRIMARY KEY REFERENCES accounts(id),
    partner_id BIGINT NOT NULL REFERENCES partners(id),
    code TEXT,
    source TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS partner_rewards (
    id BIGSERIAL PRIMARY KEY,
    partner_id BIGINT NOT NULL REFERENCES partners(id),
    order_id TEXT UNIQUE NOT NULL,
    subject_id BIGINT REFERENCES accounts(id),
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'accrued',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS partner_rewards_partner_idx ON partner_rewards (partner_id);

CREATE TABLE IF NOT EXISTS partner_invite_codes (
    code TEXT PRIMARY KEY,
    partner_id BIGINT NOT NULL REFERENCES partners(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS plans (
    plan_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    days INTEGER NOT NULL CHECK (days > 0),
    price_rub INTEGER NOT NULL CHECK (price_rub >= 0),
    devices INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE plans ADD COLUMN IF NOT EXISTS devices INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans DROP CONSTRAINT IF EXISTS plans_devices_check;
ALTER TABLE plans ADD CONSTRAINT plans_devices_check CHECK (devices >= 0);

CREATE TABLE IF NOT EXISTS device_packs (
    pack_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    devices INTEGER NOT NULL CHECK (devices > 0),
    price_rub INTEGER NOT NULL CHECK (price_rub >= 0),
    days INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE device_packs ADD COLUMN IF NOT EXISTS days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE device_packs DROP CONSTRAINT IF EXISTS device_packs_days_check;
ALTER TABLE device_packs ADD CONSTRAINT device_packs_days_check CHECK (days >= 0);

CREATE TABLE IF NOT EXISTS device_slot_grants (
    order_id TEXT PRIMARY KEY,
    subject_id BIGINT NOT NULL REFERENCES accounts(id),
    devices INTEGER NOT NULL CHECK (devices > 0),
    pack_id TEXT,
    plan_id TEXT,
    expires_at TIMESTAMPTZ,
    expiry_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE device_slot_grants ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE device_slot_grants ADD COLUMN IF NOT EXISTS expiry_applied_at TIMESTAMPTZ;
ALTER TABLE device_slot_grants ADD COLUMN IF NOT EXISTS plan_id TEXT;
ALTER TABLE device_slot_grants DROP CONSTRAINT IF EXISTS device_slot_grants_one_source;
ALTER TABLE device_slot_grants ADD CONSTRAINT device_slot_grants_one_source
    CHECK (pack_id IS NULL OR plan_id IS NULL);
CREATE INDEX IF NOT EXISTS device_slot_grants_subject_idx ON device_slot_grants (subject_id);
CREATE INDEX IF NOT EXISTS device_slot_grants_expiry_idx ON device_slot_grants (expires_at)
    WHERE expires_at IS NOT NULL AND expiry_applied_at IS NULL;

CREATE TABLE IF NOT EXISTS device_slot_renewals (
    order_id TEXT PRIMARY KEY,
    grant_order_id TEXT NOT NULL REFERENCES device_slot_grants(order_id) ON DELETE CASCADE,
    days INTEGER NOT NULL CHECK (days > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_slot_renewals_grant_idx ON device_slot_renewals (grant_order_id);

CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by BIGINT
);

CREATE TABLE IF NOT EXISTS funnel_daily (
    day DATE NOT NULL,
    step TEXT NOT NULL,
    hits BIGINT NOT NULL,
    PRIMARY KEY (day, step)
);

CREATE TABLE IF NOT EXISTS polls (
    id BIGSERIAL PRIMARY KEY,
    question TEXT NOT NULL,
    options TEXT[] NOT NULL,
    is_anonymous BOOLEAN NOT NULL DEFAULT TRUE,
    allows_multiple_answers BOOLEAN NOT NULL DEFAULT FALSE,
    audience TEXT NOT NULL DEFAULT 'all',
    status TEXT NOT NULL DEFAULT 'draft',
    sent_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS poll_deliveries (
    tg_poll_id TEXT PRIMARY KEY,
    poll_id BIGINT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    telegram_id BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    option_counts INTEGER[],
    voter_count INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS poll_deliveries_poll_idx ON poll_deliveries (poll_id);

CREATE TABLE IF NOT EXISTS poll_answers (
    poll_id BIGINT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    telegram_id BIGINT NOT NULL,
    option_ids INTEGER[] NOT NULL,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (poll_id, telegram_id)
);

CREATE TABLE IF NOT EXISTS feature_requests (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL,
    username TEXT,
    body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'new',
    admin_comment TEXT NOT NULL DEFAULT '',
    decided_by BIGINT,
    decided_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS feature_requests_status_idx ON feature_requests (status);
CREATE INDEX IF NOT EXISTS feature_requests_author_idx ON feature_requests (telegram_id);

CREATE TABLE IF NOT EXISTS feature_request_votes (
    request_id BIGINT NOT NULL REFERENCES feature_requests(id) ON DELETE CASCADE,
    telegram_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (request_id, telegram_id)
);

-- Живые обновления очереди поддержки для веб-кабинета.
CREATE OR REPLACE FUNCTION notify_support_update() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE target_id bigint;
BEGIN
    IF TG_TABLE_NAME = 'support_tickets' THEN
        IF TG_OP = 'DELETE' THEN target_id := OLD.id;
        ELSE target_id := NEW.id; END IF;
    ELSE
        IF TG_OP = 'DELETE' THEN target_id := OLD.ticket_id;
        ELSE target_id := NEW.ticket_id; END IF;
    END IF;
    PERFORM pg_notify('support_updates', target_id::text);
    RETURN NULL;
END;
$$;
DROP TRIGGER IF EXISTS support_tickets_live ON support_tickets;
CREATE TRIGGER support_tickets_live AFTER INSERT OR UPDATE OR DELETE
    ON support_tickets FOR EACH ROW EXECUTE FUNCTION notify_support_update();
DROP TRIGGER IF EXISTS support_messages_live ON support_messages;
CREATE TRIGGER support_messages_live AFTER INSERT OR UPDATE OR DELETE
    ON support_messages FOR EACH ROW EXECUTE FUNCTION notify_support_update();
