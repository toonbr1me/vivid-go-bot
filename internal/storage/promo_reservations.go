package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

var ErrPromoRejected = errors.New("промокод не подходит")

// reservePromoOrder вызывается в транзакции создания заказа, до счёта шлюза.
// Неоплаченные резервы освобождаются по сроку ссылки, оплаченные сохраняются.
func reservePromoOrder(ctx context.Context, tx pgx.Tx, order domain.Order) error {
	if order.PromoCode == nil || *order.PromoCode == "" {
		return nil
	}
	var subjectID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id = $1
		OR ($2::bigint IS NOT NULL AND telegram_id = $2) ORDER BY id LIMIT 1`,
		order.AccountID, order.TelegramID).Scan(&subjectID); err != nil {
		return err
	}
	promo, err := scanPromo(tx.QueryRow(ctx, `SELECT `+promoColumns+` FROM promocodes WHERE code = $1 FOR UPDATE`, *order.PromoCode))
	if err != nil {
		return err
	}
	if !promo.IsActive || !promo.Live(time.Now()) {
		return fmt.Errorf("%w: код неактивен", ErrPromoRejected)
	}
	var reserved, personal int
	if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE r.subject_id = $2)
		FROM promo_reservations r JOIN orders o ON o.order_id = r.order_id
		WHERE r.code = $1 AND NOT r.consumed AND
		(o.status IN ('paid','provisioned') OR (o.status = 'pending' AND
		COALESCE(o.payment_expires_at, o.created_at + interval '30 minutes') > now()))`,
		promo.Code, subjectID).Scan(&reserved, &personal); err != nil {
		return err
	}
	var used int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM promo_activations WHERE code = $1 AND subject_id = $2`,
		promo.Code, subjectID).Scan(&used); err != nil {
		return err
	}
	personalLimit := promo.PerUserLimit
	if order.Provider == "promo_gift" && personalLimit == 0 {
		personalLimit = 1
	}
	if (promo.MaxUses > 0 && promo.UsedCount+reserved >= promo.MaxUses) ||
		(personalLimit > 0 && used+personal >= personalLimit) {
		return fmt.Errorf("%w: лимит применений исчерпан", ErrPromoRejected)
	}
	_, err = tx.Exec(ctx, `INSERT INTO promo_reservations(order_id, subject_id, code) VALUES ($1,$2,$3)`,
		order.OrderID, subjectID, promo.Code)
	return err
}

func (t Tx) ConsumePromoReservation(ctx context.Context, orderID string) error {
	_, err := t.tx.Exec(ctx, `UPDATE promo_reservations SET consumed = TRUE WHERE order_id = $1`, orderID)
	return err
}
