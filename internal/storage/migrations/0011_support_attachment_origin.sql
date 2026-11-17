-- Telegram file_id привязан к конкретному боту. Старые вложения были
-- получены ботом сервиса; новые записи явно отмечают источник.
ALTER TABLE support_messages
    ADD COLUMN IF NOT EXISTS file_bot TEXT NOT NULL DEFAULT 'commerce';

UPDATE support_messages
SET file_bot = 'commerce'
WHERE file_bot = '' OR file_bot IS NULL;
