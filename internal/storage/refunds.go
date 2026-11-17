package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

type RefundCase struct {
	ID                                 int64
	OrderID, Reason, Status, Reference string
}

func (db *DB) RefundCases(ctx context.Context) ([]RefundCase, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id,order_id,reason,status,provider_reference FROM refund_cases ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefundCase
	for rows.Next() {
		var c RefundCase
		if err = rows.Scan(&c.ID, &c.OrderID, &c.Reason, &c.Status, &c.Reference); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// A recorded completion requires the reference of an actual provider refund.
// This operation records reconciliation; it never initiates a bank transaction.
func (db *DB) UpdateRefundCase(ctx context.Context, actor int64, orderID, reason, status, reference string) error {
	if status != "requested" && status != "completed" && status != "rejected" {
		return errors.New("неизвестное состояние возврата")
	}
	if reason == "" || len([]rune(reason)) > 1000 || len(reference) > 200 {
		return errors.New("укажите причину до 1000 символов")
	}
	if status == "completed" && reference == "" {
		return errors.New("нужен номер возврата от провайдера")
	}
	order, err := db.OrderByID(ctx, orderID)
	if err != nil {
		return err
	}
	if order.AccountID == nil && order.TelegramID != nil {
		subject, e := db.SubjectByTelegramID(ctx, *order.TelegramID)
		if e != nil {
			return e
		}
		order.AccountID = &subject.ID
	}
	if order.AccountID == nil {
		return errors.New("заказ не связан с аккаунтом")
	}
	unlock, err := db.LockSubject(ctx, *order.AccountID)
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if order, err = scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE order_id=$1 FOR UPDATE`, orderID)); err != nil {
		return err
	}
	if order.Amount <= 0 || order.Status != domain.OrderPaid && order.Status != domain.OrderProvisioned && order.Status != domain.OrderRefunded {
		return errors.New("возврат возможен только по оплаченному заказу")
	}
	var oldStatus, oldRef string
	err = tx.QueryRow(ctx, `SELECT status,provider_reference FROM refund_cases WHERE order_id=$1`, orderID).Scan(&oldStatus, &oldRef)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if oldStatus == "completed" {
		if status == "completed" && reference == oldRef {
			return nil
		}
		return errors.New("возврат уже завершён")
	}
	if status == "completed" && order.Kind == domain.KindGift {
		var claimed bool
		var giftID string
		if err = tx.QueryRow(ctx, `SELECT id FROM subscription_gifts WHERE purchase_order_id=$1 FOR UPDATE`, orderID).Scan(&giftID); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM subscription_gifts WHERE purchase_order_id=$1 AND claimed_at IS NOT NULL)`, orderID).Scan(&claimed); err != nil {
			return err
		}
		if claimed {
			return errors.New("подарок уже активирован: требуется отдельное решение по выданному доступу")
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO refund_cases(order_id,reason,status,provider_reference,actor_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(order_id) DO UPDATE SET reason=excluded.reason,status=excluded.status,provider_reference=excluded.provider_reference,actor_id=excluded.actor_id,updated_at=now()`, orderID, reason, status, reference, actor); err != nil {
		return err
	}
	if status == "completed" {
		if _, err = tx.Exec(ctx, `UPDATE orders SET status='refunded',updated_at=now() WHERE order_id=$1`, orderID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO staff_audit(actor_id,action,target,amount,details) VALUES($1,'refund.manage',$2,$3,$4)`, actor, orderID, order.Amount.String(), fmt.Sprintf("%s %s", status, reference)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
