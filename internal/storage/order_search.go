package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"strings"
	"time"
)

func (db *DB) DecideWithdrawalAtomic(ctx context.Context, id, actor int64, decision string) error {
	if decision != "approved" && decision != "rejected" {
		return errors.New("неизвестное решение")
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var subject *int64
	var amount domain.Money
	var cents int64
	var status string
	if err = tx.QueryRow(ctx, `SELECT subject_id,round(amount*100)::bigint,status FROM withdrawal_requests WHERE id=$1 FOR UPDATE`, id).Scan(&subject, &cents, &status); err != nil {
		return err
	}
	amount = domain.Money(cents)
	if status != "pending" {
		if status == decision {
			return nil
		}
		return errors.New("заявка уже обработана")
	}
	if decision == "approved" {
		if subject == nil || amount <= 0 {
			return errors.New("некорректный получатель или сумма")
		}
		tag, e := tx.Exec(ctx, `UPDATE user_balance SET balance=balance-$2 WHERE subject_id=$1 AND balance>=$2`, *subject, amount.String())
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrInsufficientBalance
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE withdrawal_requests SET status=$2,updated_at=now() WHERE id=$1`, id, decision); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO staff_audit(actor_id,action,target,amount,details) VALUES($1,'withdrawal.approve',$2,$3,$4)`, actor, fmt.Sprint(id), amount.String(), decision); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type OrderFilter struct {
	Size                    int
	Query, Status, Provider string
	From, To                *time.Time
	Page                    int
	Attention               bool
	SubjectID               *int64
}
type OrderPage struct {
	Orders             []domain.Order
	Filter             OrderFilter
	Page, Pages, Total int
}

func (db *DB) SearchOrders(ctx context.Context, f OrderFilter) (out OrderPage, err error) {
	const where = ` WHERE ($1='' OR order_id ILIKE '%'||$1||'%' OR COALESCE(transaction_id,'') ILIKE '%'||$1||'%' OR COALESCE(username,'') ILIKE '%'||$1||'%' OR telegram_id::text=$1 OR account_id::text=$1 OR COALESCE(payment_link_id,'')=$1) AND ($2='' OR status=$2) AND ($3='' OR provider=$3) AND ($4::timestamptz IS NULL OR created_at>=$4) AND ($5::timestamptz IS NULL OR created_at<$5) AND (NOT $6 OR (status='paid' AND provisioned_at IS NULL) OR (status='pending' AND created_at<now()-interval '30 minutes')) AND ($7::bigint IS NULL OR account_id=$7 OR telegram_id=(SELECT telegram_id FROM accounts WHERE id=$7))`
	args := []any{f.Query, f.Status, f.Provider, f.From, f.To, f.Attention, f.SubjectID}
	out.Filter = f
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM orders`+where, args...).Scan(&out.Total); err != nil {
		return
	}
	size := f.Size
	if size < 1 || size > 100 {
		size = 50
	}
	out.Pages = max(1, (out.Total+size-1)/size)
	out.Page = max(0, min(f.Page, out.Pages-1))
	args = append(args, out.Page*size, size)
	columns := strings.Replace(orderColumns, "account_id", "COALESCE(account_id,(SELECT id FROM accounts WHERE telegram_id=orders.telegram_id AND merged_into IS NULL LIMIT 1))", 1)
	out.Orders, err = db.queryOrders(ctx, `SELECT `+columns+` FROM orders`+where+` ORDER BY created_at DESC,id DESC LIMIT $9 OFFSET $8`, args...)
	return
}

func (db *DB) SaveSettings(ctx context.Context, values map[string]string, actor int64) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for k, v := range values {
		if v == "" {
			_, err = tx.Exec(ctx, `DELETE FROM app_settings WHERE key=$1`, k)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value,updated_by) VALUES($1,$2,$3) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`, k, v, actor)
		}
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO staff_audit(actor_id,action,details) VALUES($1,'settings.edit','Изменены настройки сервиса')`, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
