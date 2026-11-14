package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"time"
)

const agreementColumns = `id,subject_id,provider,method,plan_id,amount_cents,currency,days,device_count,external_id,enabled,activated,next_charge_at,pending_order_id,last_error,stop_reason,stopped_at`

func scanAgreement(row pgx.Row) (a domain.RecurringAgreement, err error) {
	err = row.Scan(&a.ID, &a.SubjectID, &a.Provider, &a.Method, &a.PlanID, &a.Amount, &a.Currency, &a.Days, &a.DeviceCount, &a.ExternalID, &a.Enabled, &a.Activated, &a.NextChargeAt, &a.PendingOrderID, &a.LastError, &a.StopReason, &a.StoppedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
func (db *DB) CreateRecurring(ctx context.Context, a domain.RecurringAgreement) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO recurring_agreements(id,subject_id,provider,method,plan_id,amount_cents,currency,days,device_count,pending_order_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$1)`, a.ID, a.SubjectID, a.Provider, a.Method, a.PlanID, int64(a.Amount), a.Currency, a.Days, a.DeviceCount)
	return err
}
func (db *DB) RecurringByID(ctx context.Context, id string) (domain.RecurringAgreement, error) {
	return scanAgreement(db.Pool.QueryRow(ctx, `SELECT `+agreementColumns+` FROM recurring_agreements WHERE id=$1`, id))
}
func (db *DB) RecurringForSubject(ctx context.Context, id int64) (domain.RecurringAgreement, error) {
	return scanAgreement(db.Pool.QueryRow(ctx, `SELECT `+agreementColumns+` FROM recurring_agreements WHERE subject_id=$1 AND enabled ORDER BY consent_at DESC LIMIT 1`, id))
}
func (db *DB) RecurringByExternal(ctx context.Context, provider, id string) (domain.RecurringAgreement, error) {
	return scanAgreement(db.Pool.QueryRow(ctx, `SELECT `+agreementColumns+` FROM recurring_agreements WHERE provider=$1 AND external_id=$2`, provider, id))
}
func (db *DB) SetRecurringExternal(ctx context.Context, id, external string, activated bool) error {
	_, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET external_id=$2,activated=activated OR $3,updated_at=now() WHERE id=$1`, id, external, activated)
	return err
}
func (db *DB) SetRecurringSchedule(ctx context.Context, id string, next *time.Time, pending *string) error {
	_, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET next_charge_at=$2,pending_order_id=$3,last_error='',updated_at=now() WHERE id=$1`, id, next, pending)
	return err
}

// Provider callbacks can arrive out of order; a replay must not move the
// scheduled charge backwards or re-enable a cancelled agreement.
func (db *DB) ConfirmRecurringCharge(ctx context.Context, id, external string, next *time.Time) error {
	_, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET external_id=$2,activated=TRUE,
 next_charge_at=GREATEST(next_charge_at,$3::timestamptz),pending_order_id=NULL,last_error='',updated_at=now() WHERE id=$1`, id, external, next)
	return err
}
func (db *DB) DisableRecurring(ctx context.Context, id string) error {
	return db.StopRecurring(ctx, id, "review")
}
func (db *DB) StopRecurring(ctx context.Context, id, reason string) error {
	_, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET enabled=FALSE,stop_reason=$2,stopped_at=now(),updated_at=now() WHERE id=$1 AND enabled`, id, reason)
	return err
}
func (db *DB) LatestRecurringForSubject(ctx context.Context, id int64) (domain.RecurringAgreement, error) {
	return scanAgreement(db.Pool.QueryRow(ctx, `SELECT `+agreementColumns+` FROM recurring_agreements WHERE subject_id=$1 ORDER BY enabled DESC,consent_at DESC,id DESC LIMIT 1`, id))
}
func (db *DB) RecurringError(ctx context.Context, id, message string) error {
	_, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET last_error=$2,updated_at=now() WHERE id=$1`, id, message)
	return err
}

func (db *DB) NoteRecurringDecline(ctx context.Context, id, orderID string) error {
	_, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET last_error='declined',updated_at=now() WHERE id=$1 AND enabled
 AND EXISTS(SELECT 1 FROM orders failed WHERE failed.order_id=$2 AND failed.recurring_agreement_id=$1 AND failed.status NOT IN ('paid','provisioned')
 AND NOT EXISTS(SELECT 1 FROM orders newer WHERE newer.recurring_agreement_id=$1 AND newer.status IN ('paid','provisioned') AND newer.created_at>failed.created_at))`, id, orderID)
	return err
}

func (db *DB) TouchRecurring(ctx context.Context, id string) {
	_, _ = db.Pool.Exec(ctx, `UPDATE recurring_agreements SET updated_at=now() WHERE id=$1`, id)
}
func (db *DB) ActiveRecurring(ctx context.Context, limit int) ([]domain.RecurringAgreement, error) {
	rows, err := db.Pool.Query(ctx, `SELECT `+agreementColumns+` FROM recurring_agreements WHERE enabled AND provider='yookassa' ORDER BY updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RecurringAgreement
	for rows.Next() {
		a, e := scanAgreement(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (db *DB) CheckoutPreference(ctx context.Context, id int64) (plan string, recurring bool, err error) {
	err = db.Pool.QueryRow(ctx, `SELECT checkout_plan,checkout_recurring FROM bot_ui_preferences WHERE telegram_id=$1`, id).Scan(&plan, &recurring)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}
func (db *DB) SaveCheckoutPreference(ctx context.Context, id int64, plan string, recurring bool) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO bot_ui_preferences(telegram_id,checkout_plan,checkout_recurring) VALUES($1,$2,$3) ON CONFLICT(telegram_id) DO UPDATE SET checkout_plan=$2,checkout_recurring=$3`, id, plan, recurring)
	return err
}

func (db *DB) LockRecurring(ctx context.Context, id string) (func(), error) {
	conn, err := pgx.ConnectConfig(ctx, db.Pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, err
	}
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, "recurring:"+id); err != nil {
		_ = conn.Close(context.Background())
		return nil, err
	}
	return func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}, nil
}

func (db *DB) RecurringChargeOrder(ctx context.Context, provider, transaction string) (string, error) {
	var id string
	err := db.Pool.QueryRow(ctx, `SELECT order_id FROM recurring_charge_links WHERE provider=$1 AND transaction_id=$2`, provider, transaction).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return id, err
}
func (db *DB) RecurringOrderReserved(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recurring_charge_links WHERE order_id=$1)`, id).Scan(&exists)
	return exists, err
}
func (db *DB) LinkRecurringCharge(ctx context.Context, provider, transaction, id string) error {
	var stored string
	err := db.Pool.QueryRow(ctx, `INSERT INTO recurring_charge_links(provider,transaction_id,order_id) VALUES($1,$2,$3)
 ON CONFLICT(provider,transaction_id) DO UPDATE SET order_id=recurring_charge_links.order_id
 WHERE recurring_charge_links.order_id=EXCLUDED.order_id RETURNING order_id`, provider, transaction, id).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("транзакция уже связана с другим заказом")
	}
	return err
}
