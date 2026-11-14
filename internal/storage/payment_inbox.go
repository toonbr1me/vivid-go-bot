package storage

import (
	"context"
	"encoding/json"
	"time"
)

type PaymentInboxItem struct {
	Key      string
	Provider string
	Payload  json.RawMessage
}

func (db *DB) StorePaymentEvent(ctx context.Context, key, provider string, payload []byte) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO payment_inbox(event_key, provider, payload)
		VALUES ($1,$2,$3::jsonb) ON CONFLICT DO NOTHING`, key, provider, payload)
	return err
}

func (db *DB) PendingPaymentEvents(ctx context.Context, limit int) ([]PaymentInboxItem, error) {
	rows, err := db.Pool.Query(ctx, `SELECT event_key, provider, payload FROM payment_inbox
		WHERE processed_at IS NULL AND retry_at <= now() ORDER BY retry_at, created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaymentInboxItem
	for rows.Next() {
		var item PaymentInboxItem
		if err := rows.Scan(&item.Key, &item.Provider, &item.Payload); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (db *DB) FinishPaymentEvent(ctx context.Context, key string, failure error) error {
	if failure == nil {
		_, err := db.Pool.Exec(ctx, `UPDATE payment_inbox SET processed_at = now(), last_error = NULL
			WHERE event_key = $1`, key)
		return err
	}
	_, err := db.Pool.Exec(ctx, `UPDATE payment_inbox SET attempts = attempts + 1,
		last_error = $2, retry_at = $3 WHERE event_key = $1 AND processed_at IS NULL`,
		key, failure.Error(), time.Now().Add(time.Minute))
	return err
}
