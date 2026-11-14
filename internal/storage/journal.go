package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Journal — журнал выдачи заказа.
//
// Каждый шаг выполняется ровно один раз: его результат сохраняется рядом с
// локальными начислениями, а сама выдача идёт по сохранённым абсолютным
// параметрам. Поэтому повторный вебхук или фоновая попытка не выдают второй
// период подписки, не активируют промокод дважды и не начисляют реферальную
// награду дважды.
//
// Шаг меняет только базу или читает внешнее состояние. Внешние записи
// (панель) делаются после шага — по абсолютным значениям из его результата.
type Journal struct {
	db      *DB
	orderID string
}

// StepAction — действие шага журнала.
type StepAction func(ctx context.Context, tx Tx) (any, error)

// Journal возвращает журнал выдачи заказа.
func (db *DB) Journal(orderID string) *Journal {
	return &Journal{db: db, orderID: orderID}
}

// Completed позволяет не повторять внешний шаг после уже записанного
// решения о доставке или отложенном начислении.
func (j *Journal) Completed(ctx context.Context, step string) (bool, error) {
	var done bool
	err := j.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_fulfillment_steps
		WHERE order_id = $1 AND step = $2)`, j.orderID, step).Scan(&done)
	return done, err
}

// Once выполняет шаг один раз и возвращает его результат. Повторный вызов
// отдаёт сохранённый результат, не выполняя действие заново.
func (j *Journal) Once(ctx context.Context, step string, action StepAction) (json.RawMessage, error) {
	tx, err := j.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("журнал выдачи %s/%s: %w", j.orderID, step, err)
	}
	defer tx.Rollback(ctx)
	// Блокируем шаг до действия: ON CONFLICT после action не защищает
	// начисления от двух параллельных исполнителей.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"fulfillment:"+j.orderID+":"+step); err != nil {
		return nil, err
	}

	stored, err := readStep(ctx, tx, j.orderID, step)
	if err != nil {
		return nil, fmt.Errorf("журнал выдачи %s/%s: %w", j.orderID, step, err)
	}
	if stored != nil {
		return stored, nil
	}

	result, err := action(ctx, Tx{tx: tx})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("журнал выдачи %s/%s: %w", j.orderID, step, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO order_fulfillment_steps (order_id, step, result)
		VALUES ($1, $2, $3::jsonb)
		ON CONFLICT (order_id, step) DO NOTHING`, j.orderID, step, encoded); err != nil {
		return nil, fmt.Errorf("журнал выдачи %s/%s: %w", j.orderID, step, err)
	}
	// Шаг мог выполнить соседний воркер: берём его результат, чтобы дальше
	// все считали одно и то же.
	stored, err = readStep(ctx, tx, j.orderID, step)
	if err != nil {
		return nil, fmt.Errorf("журнал выдачи %s/%s: %w", j.orderID, step, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("журнал выдачи %s/%s: %w", j.orderID, step, err)
	}
	if stored == nil {
		return encoded, nil
	}
	return stored, nil
}

// readStep читает сохранённый результат шага. nil — шаг ещё не выполнялся.
func readStep(ctx context.Context, tx pgx.Tx, orderID, step string) (json.RawMessage, error) {
	var raw []byte
	err := tx.QueryRow(ctx,
		`SELECT result FROM order_fulfillment_steps WHERE order_id = $1 AND step = $2`,
		orderID, step).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// Querier — общее для пула и транзакции: обычные вызовы идут через пул,
// шаги журнала — через транзакцию.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Tx — локальные действия шага журнала. Они выполняются в одной транзакции
// с записью шага, поэтому начисление и отметка о нём не расходятся.
type Tx struct {
	tx pgx.Tx
}

// TakePendingBonus забирает накопленные дни пригласившего.
func (t Tx) TakePendingBonus(ctx context.Context, subjectID int64) (int, error) {
	return takePendingBonus(ctx, t.tx, subjectID)
}

// AddPendingBonus копит дни пригласившего, у которого ещё нет подписки.
func (t Tx) AddPendingBonus(ctx context.Context, subjectID int64, days int) error {
	return addPendingBonus(ctx, t.tx, subjectID, days)
}

// AddBalance пополняет внутренний баланс субъекта.
//
// Telegram-колонка здесь не заполняется: уникальный индекс по ней мешал бы
// обновлению по субъекту, а читают баланс только по субъекту.
func (t Tx) AddBalance(ctx context.Context, subjectID int64, amount domain.Money) (domain.Money, error) {
	var cents int64
	err := t.tx.QueryRow(ctx, `
		INSERT INTO user_balance (subject_id, balance)
		VALUES ($1, $2)
		ON CONFLICT (subject_id) DO UPDATE
		   SET balance = user_balance.balance + EXCLUDED.balance
		RETURNING round(balance * 100)::bigint`, subjectID, amount.String()).Scan(&cents)
	if err != nil {
		return 0, err
	}
	return domain.Money(cents), nil
}

// UsePromo увеличивает счётчик применений промокода.
func (t Tx) UsePromo(ctx context.Context, code string) error {
	return usePromo(ctx, t.tx, code)
}

// RecordPromoActivation пишет журнал применений промокода на субъекта.
func (t Tx) RecordPromoActivation(ctx context.Context, subjectID int64, code string) error {
	return recordPromoActivation(ctx, t.tx, subjectID, code)
}

// GrantDeviceSlots записывает купленные слоты.
func (t Tx) GrantDeviceSlots(
	ctx context.Context, orderID string, subjectID int64, devices int,
	packID, planID *string, expiresAt *time.Time,
) (bool, error) {
	return grantDeviceSlots(ctx, t.tx, orderID, subjectID, devices, packID, planID, expiresAt)
}

// IncrementReferralReward увеличивает счётчик наград пригласившему.
func (t Tx) IncrementReferralReward(ctx context.Context, referredSubjectID int64) error {
	return incrementReferralReward(ctx, t.tx, referredSubjectID)
}

// ReferralByReferred ищет пригласившего субъекта.
func (t Tx) ReferralByReferred(ctx context.Context, referredSubjectID int64) (*Referral, error) {
	return referralByReferred(ctx, t.tx, referredSubjectID)
}

// ReferralMode читает выбранный способ вознаграждения.
func (t Tx) ReferralMode(ctx context.Context, subjectID int64) (string, error) {
	return referralMode(ctx, t.tx, subjectID)
}

// SubjectByID читает субъекта по идентификатору.
func (t Tx) SubjectByID(ctx context.Context, id int64) (domain.Subject, error) {
	return subjectByID(ctx, t.tx, id)
}

// GetPartner читает партнёра программы.
func (t Tx) GetPartner(ctx context.Context, partnerID int64) (*domain.Partner, error) {
	return getPartner(ctx, t.tx, partnerID)
}

// PartnerRewardCount — сколько начислений партнёру по этому клиенту.
func (t Tx) PartnerRewardCount(ctx context.Context, partnerID, subjectID int64) (int, error) {
	return partnerRewardCount(ctx, t.tx, partnerID, subjectID)
}

// CreditPartnerReward начисляет партнёру вознаграждение за заказ.
func (t Tx) CreditPartnerReward(
	ctx context.Context, partnerID int64, orderID string, subjectID *int64,
	amount domain.Money, partnerSubjectID *int64,
) (*domain.PartnerReward, error) {
	return creditPartnerReward(ctx, t.tx, partnerID, orderID, subjectID, amount, partnerSubjectID)
}
