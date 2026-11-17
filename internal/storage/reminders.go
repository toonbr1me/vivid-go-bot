package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// ReleaseTrial снимает заявку на пробный период, если выдача сорвалась.
//
// Заявка пишется до похода в панель: параллельные попытки конкурируют в базе,
// и второй бесплатный период не выдаётся. Но упавшая выдача не должна съедать
// попытку — поэтому при ошибке заявка снимается.
func (db *DB) ReleaseTrial(ctx context.Context, subjectID int64, kind string) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM trials WHERE subject_id = $1 AND kind = $2`, subjectID, kind)
	return err
}

// MarkReminder фиксирует отправленное напоминание. false — уже отправляли
// про этот же срок: повтор после перезапуска не нужен.
func (db *DB) MarkReminder(ctx context.Context, telegramID int64, kind string, target time.Time) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `
		INSERT INTO reminder_log (telegram_id, kind, target_at)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, telegramID, kind, target)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimReminder резервирует доставку, но не объявляет её успешной заранее.
func (db *DB) ClaimReminder(ctx context.Context, telegramID int64, kind string, target time.Time) (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	claim := hex.EncodeToString(token[:])
	tag, err := db.Pool.Exec(ctx, `INSERT INTO reminder_log(telegram_id, kind, target_at, status, lease_until, claim)
		VALUES($1,$2,$3,'sending',now() + interval '5 minutes',$4)
		ON CONFLICT(telegram_id,kind,target_at) DO UPDATE SET status = 'sending',
		lease_until = EXCLUDED.lease_until, claim = EXCLUDED.claim
		WHERE reminder_log.status <> 'sent' AND reminder_log.lease_until <= now()`, telegramID, kind, target, claim)
	if err != nil || tag.RowsAffected() == 0 {
		return "", err
	}
	return claim, nil
}

func (db *DB) FinishReminder(ctx context.Context, claim string, delivered bool) error {
	status := "pending"
	if delivered {
		status = "sent"
	}
	_, err := db.Pool.Exec(ctx, `UPDATE reminder_log SET status = $2,
		lease_until = now() + interval '1 minute' WHERE claim = $1 AND status = 'sending'`, claim, status)
	return err
}

// SubscriptionForOwner — последний выданный заказ субъекта: из него экран
// берёт срок и ссылку, когда панель недоступна.
func (db *DB) SubscriptionForOwner(ctx context.Context, telegramID, accountID *int64) (*domain.Order, error) {
	order, err := db.queryOrders(ctx, `
		SELECT `+orderColumns+` FROM orders
		WHERE kind = 'subscription' AND status = 'provisioned'
		  AND (($1::bigint IS NOT NULL AND telegram_id = $1)
		       OR ($2::bigint IS NOT NULL AND account_id = $2))
		ORDER BY COALESCE(expire_at, created_at) DESC LIMIT 1`, telegramID, accountID)
	if err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, nil
	}
	return &order[0], nil
}

// PendingOrdersForReminder — неоплаченные счета, о которых стоит напомнить.
// По одному на человека: три напоминания о трёх счетах подряд выглядят
// спамом, а не заботой.
func (db *DB) PendingOrdersForReminder(ctx context.Context, olderThan time.Time, limit int) ([]domain.Order, error) {
	return db.queryOrders(ctx, `
		SELECT DISTINCT ON (recipient_telegram_id) `+strings.Replace(orderColumns, "telegram_id", "recipient_telegram_id", 1)+` FROM (
		 SELECT o.*, a.telegram_id AS recipient_telegram_id FROM orders o
		 JOIN accounts a ON (o.account_id=a.id OR (o.account_id IS NULL AND o.telegram_id=a.telegram_id))
		 WHERE a.telegram_id IS NOT NULL
		) orders WHERE status = 'pending'
		  AND payment_url IS NOT NULL AND created_at <= $1
		  AND payment_expires_at > now() AND created_at>now()-interval '30 days'
 AND NOT EXISTS(SELECT 1 FROM reminder_log r WHERE r.telegram_id=orders.recipient_telegram_id AND r.kind='unpaid' AND r.status='sent' AND r.sent_at>now()-interval '30 days')
		  AND NOT EXISTS (SELECT 1 FROM reminder_log r WHERE r.telegram_id = orders.recipient_telegram_id
		    AND r.kind = 'unpaid' AND r.target_at = orders.created_at
		    AND (r.status = 'sent' OR r.lease_until > now()))
		  AND NOT EXISTS (SELECT 1 FROM orders newer WHERE (newer.telegram_id = orders.recipient_telegram_id OR newer.account_id = orders.account_id)
		    AND newer.kind = orders.kind AND newer.status IN ('paid','provisioned')
		    AND newer.created_at >= orders.created_at)
		ORDER BY recipient_telegram_id, created_at DESC
		LIMIT $2`, olderThan, limit)
}
