package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

var ErrCompensationLimit = errors.New("compensation limit reached")

// Reserve the quota, audit entry and durable grant together. A panel failure
// leaves the same queued order reserved; it must not free quota for another grant.
func (db *DB) ReserveCompensation(ctx context.Context, actor, ticketID int64, subject domain.Subject, cap int, unlimited bool, currency string) (domain.Order, bool, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("compensation:%d", actor)); err != nil {
		return domain.Order{}, false, err
	}
	var locked int64
	if err = tx.QueryRow(ctx, `SELECT id FROM support_tickets WHERE id=$1 AND status='open' AND (subject_id=$2 OR telegram_id=$3) FOR UPDATE`, ticketID, subject.ID, subject.TelegramID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		return domain.Order{}, false, err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT order_id FROM support_compensations WHERE ticket_id=$1`, ticketID).Scan(&existing)
	if err == nil {
		o, e := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE order_id=$1`, existing))
		return o, false, e
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, false, err
	}
	if !unlimited {
		var spent int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0)::int FROM staff_audit WHERE actor_id=$1 AND action='client.compensate' AND created_at>now()-interval '24 hours'`, actor).Scan(&spent); err != nil {
			return domain.Order{}, false, err
		}
		if cap-spent < 3 {
			return domain.Order{}, false, ErrCompensationLimit
		}
	}
	days := 3
	username := ""
	if subject.RemnawaveUser != nil {
		username = *subject.RemnawaveUser
	}
	method := "support_comp"
	o, err := createOrder(ctx, tx, domain.Order{OrderID: fmt.Sprintf("support-comp-%d", ticketID), Provider: method, TelegramID: subject.TelegramID, AccountID: &subject.ID, Username: nullStringPtr(username), PlanID: "gift", Kind: domain.KindSubscription, Currency: currency, PaymentMethod: &method, Status: domain.OrderPaid, PlanDays: &days})
	if err != nil {
		return domain.Order{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO support_compensations(ticket_id,actor_id,subject_id,order_id) VALUES($1,$2,$3,$4)`, ticketID, actor, subject.ID, o.OrderID); err != nil {
		return domain.Order{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO staff_audit(actor_id,action,target,amount,details) VALUES($1,'client.compensate',$2,3,$3)`, actor, fmt.Sprintf("ticket:%d", ticketID), o.OrderID); err != nil {
		return domain.Order{}, false, err
	}
	if _, err = addTicketMessage(ctx, tx, ticketID, subject.ID, &actor, "Поддержка оформила компенсацию: 3 дня. Срок подписки можно проверить в профиле; при временном сбое выдача продолжится автоматически.", true, "", ""); err != nil {
		return domain.Order{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Order{}, false, err
	}
	return o, true, nil
}
func nullStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func (db *DB) TicketCompensation(ctx context.Context, id int64) (domain.Order, error) {
	var orderID string
	if err := db.Pool.QueryRow(ctx, `SELECT order_id FROM support_compensations WHERE ticket_id=$1`, id).Scan(&orderID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Order{}, ErrNotFound
		}
		return domain.Order{}, err
	}
	return db.OrderByID(ctx, orderID)
}
