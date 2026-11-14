CREATE TABLE IF NOT EXISTS bot_ui_preferences (
    telegram_id BIGINT PRIMARY KEY,
    mode TEXT NOT NULL DEFAULT 'classic' CHECK (mode IN ('classic','cards')),
    selector_message_id INTEGER NOT NULL DEFAULT 0
);
