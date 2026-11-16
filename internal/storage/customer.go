package storage

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// CustomerFocusOrder considers the whole history, so an old paid order cannot
// disappear behind newer abandoned invoices or a UI pagination limit.
func (db *DB) CustomerFocusOrder(ctx context.Context, subject domain.Subject) (*domain.Order, error) {
	orders, err := db.queryOrders(ctx, `SELECT `+orderColumns+` FROM orders
		WHERE (account_id=$1 OR ($2::bigint IS NOT NULL AND telegram_id=$2)) AND kind='subscription'
		AND (status='paid' OR (status='pending' AND payment_expires_at>now()) OR status='provisioned')
		ORDER BY CASE WHEN status='paid' AND provisioned_at IS NULL THEN 0 WHEN status='provisioned' AND expire_at>now() THEN 1 WHEN status='pending' THEN 2 ELSE 3 END,
		expire_at DESC NULLS LAST, created_at DESC LIMIT 1`, subject.ID, subject.TelegramID)
	if err != nil || len(orders) == 0 {
		return nil, err
	}
	return &orders[0], nil
}
