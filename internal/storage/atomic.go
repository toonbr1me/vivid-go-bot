package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

var ErrInsufficientBalance = errors.New("на балансе недостаточно средств")
var ErrTrialUsed = errors.New("пробный период уже использован")

// CreateTrialOrder резервирует триал и записывает восстановимый заказ вместе.
func (db *DB) CreateTrialOrder(ctx context.Context, subject domain.Subject, kind string, order domain.Order) (domain.Order, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO trials (subject_id, telegram_id, kind)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, subject.ID, subject.TelegramID, kind)
	if err != nil {
		return domain.Order{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Order{}, ErrTrialUsed
	}
	created, err := createOrder(ctx, tx, order)
	if err != nil {
		return domain.Order{}, err
	}
	if err := reservePromoOrder(ctx, tx, created); err != nil {
		return domain.Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return created, nil
}

// ReservedExpiry учитывает оплаченные дни, которые панель ещё не применила.
func (db *DB) ReservedExpiry(ctx context.Context, subject domain.Subject) (time.Time, error) {
	return reservedExpiry(ctx, db.Pool, subject)
}

func (t Tx) ReservedExpiry(ctx context.Context, subject domain.Subject) (time.Time, error) {
	return reservedExpiry(ctx, t.tx, subject)
}

func reservedExpiry(ctx context.Context, q Querier, subject domain.Subject) (time.Time, error) {
	var target *time.Time
	err := q.QueryRow(ctx, `SELECT max((s.result #>> '{}')::timestamptz)
		FROM order_fulfillment_steps s JOIN orders o ON o.order_id = s.order_id
		WHERE s.step = 'subscription_target' AND o.status IN ('paid', 'provisioned')
		AND (o.account_id = $1 OR ($2::bigint IS NOT NULL AND o.telegram_id = $2))`,
		subject.ID, subject.TelegramID).Scan(&target)
	if err != nil || target == nil {
		return time.Time{}, err
	}
	return *target, nil
}

// CreateBalanceOrder атомарно списывает деньги и создаёт оплаченный заказ.
func (db *DB) CreateBalanceOrder(ctx context.Context, subjectID int64, order domain.Order) (domain.Order, error) {
	if order.Amount <= 0 || order.Provider != "balance" || order.Status != domain.OrderPaid {
		return domain.Order{}, errors.New("неверные параметры покупки с баланса")
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE user_balance SET balance = balance - $2
		WHERE subject_id = $1 AND balance >= $2`, subjectID, order.Amount.String())
	if err != nil {
		return domain.Order{}, err
	}
	if tag.RowsAffected() != 1 {
		return domain.Order{}, ErrInsufficientBalance
	}
	created, err := createOrder(ctx, tx, order)
	if err != nil {
		return domain.Order{}, err
	}
	if err := reservePromoOrder(ctx, tx, created); err != nil {
		return domain.Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return created, nil
}

// LockSubject сериализует доступ между процессами. Отдельное соединение
// не занимает пул, нужный шагам журнала. Закрытие освобождает блокировку.
func (db *DB) LockSubject(ctx context.Context, subjectID int64) (func(), error) {
	conn, err := pgx.ConnectConfig(ctx, db.Pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`,
		fmt.Sprintf("access:subject:%d", subjectID)); err != nil {
		_ = conn.Close(context.Background())
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = conn.Close(closeCtx)
		})
	}, nil
}
