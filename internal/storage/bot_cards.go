package storage

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (db *DB) BotCard(ctx context.Context, id int64) (int, json.RawMessage, error) {
	var messageID int
	var state json.RawMessage
	err := db.Pool.QueryRow(ctx, `SELECT card_message_id, card_state FROM bot_ui_preferences WHERE telegram_id=$1`, id).Scan(&messageID, &state)
	return messageID, state, err
}

func (db *DB) SaveBotCard(ctx context.Context, id int64, messageID int, state any) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO bot_ui_preferences(telegram_id,card_message_id,card_state) VALUES($1,$2,$3)
 ON CONFLICT(telegram_id) DO UPDATE SET card_message_id=EXCLUDED.card_message_id,card_state=EXCLUDED.card_state`, id, messageID, payload)
	return err
}

// Serializes queries before checking their revision, including repeated taps.
func (db *DB) LockBotCard(ctx context.Context, id int64) (func(), error) {
	conn, err := pgx.ConnectConfig(ctx, db.Pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, err
	}
	key := "bot-card:" + strconv.FormatInt(id, 10)
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, key); err != nil {
		_ = conn.Close(context.Background())
		return nil, err
	}
	return func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(unlockCtx)
	}, nil
}
