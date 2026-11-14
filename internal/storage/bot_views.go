package storage

import "context"

type BotView struct {
	Mode              string
	SelectorMessageID int
}

func (db *DB) BotView(ctx context.Context, id int64) (BotView, error) {
	var view BotView
	err := db.Pool.QueryRow(ctx, `SELECT
		COALESCE((SELECT mode FROM bot_ui_preferences WHERE telegram_id=$1),'classic'),
		COALESCE((SELECT selector_message_id FROM bot_ui_preferences WHERE telegram_id=$1),0)`, id).Scan(&view.Mode, &view.SelectorMessageID)
	return view, err
}
func (db *DB) SetBotView(ctx context.Context, id int64, mode string) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO bot_ui_preferences(telegram_id,mode) VALUES($1,$2)
		ON CONFLICT(telegram_id) DO UPDATE SET mode=EXCLUDED.mode`, id, mode)
	return err
}
func (db *DB) SetBotSelector(ctx context.Context, id int64, messageID int) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO bot_ui_preferences(telegram_id,selector_message_id) VALUES($1,$2)
		ON CONFLICT(telegram_id) DO UPDATE SET selector_message_id=EXCLUDED.selector_message_id`, id, messageID)
	return err
}

func (db *DB) NativeMessageID(ctx context.Context, id int64) int {
	var message int
	_ = db.Pool.QueryRow(ctx, `SELECT native_message_id FROM bot_ui_preferences WHERE telegram_id=$1`, id).Scan(&message)
	return message
}
func (db *DB) SaveNativeMessage(ctx context.Context, id int64, message int) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO bot_ui_preferences(telegram_id,native_message_id) VALUES($1,$2) ON CONFLICT(telegram_id) DO UPDATE SET native_message_id=$2`, id, message)
	return err
}
