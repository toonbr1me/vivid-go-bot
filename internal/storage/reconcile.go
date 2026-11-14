package storage

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func (db *DB) OrdersForPaymentCheck(ctx context.Context, limit int) ([]domain.Order, error) {
	return db.queryOrders(ctx, `SELECT `+orderColumns+` FROM orders
		WHERE status IN ('pending','failed','cancelled') AND provider IN ('wata','platega','yookassa')
		AND created_at > now() - interval '3 days'
		AND (payment_checked_at IS NULL OR payment_checked_at < now() - interval '1 minute')
		ORDER BY payment_checked_at NULLS FIRST, created_at LIMIT $1`, limit)
}

func (db *DB) ClaimPaymentCheck(ctx context.Context, orderID string) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `UPDATE orders SET payment_checked_at = now() WHERE order_id = $1
		AND status IN ('pending','failed','cancelled')
		AND (payment_checked_at IS NULL OR payment_checked_at < now() - interval '1 minute')`, orderID)
	return tag.RowsAffected() == 1, err
}
