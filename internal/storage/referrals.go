package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Referral — связь «пригласил → пришёл».
type Referral struct {
	ID                int64
	ReferrerSubjectID int64
	ReferredSubjectID int64
	RewardCount       int
	BonusDays         int
}

// CreateReferral записывает связь, если её ещё нет.
func (db *DB) CreateReferral(ctx context.Context, referrerSubjectID, referredSubjectID int64) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO referrals (referrer_subject_id, referred_subject_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, referrerSubjectID, referredSubjectID)
	return err
}

// ReferralByReferred ищет пригласившего для субъекта.
func (db *DB) ReferralByReferred(ctx context.Context, referredSubjectID int64) (*Referral, error) {
	return referralByReferred(ctx, db.Pool, referredSubjectID)
}

func referralByReferred(ctx context.Context, q Querier, referredSubjectID int64) (*Referral, error) {
	var ref Referral
	err := q.QueryRow(ctx, `
		SELECT id, COALESCE(referrer_subject_id, 0), COALESCE(referred_subject_id, 0),
		       reward_count, bonus_days
		FROM referrals WHERE referred_subject_id = $1`, referredSubjectID).Scan(
		&ref.ID, &ref.ReferrerSubjectID, &ref.ReferredSubjectID,
		&ref.RewardCount, &ref.BonusDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// IncrementReferralReward увеличивает счётчик наград пригласившему.
func (db *DB) IncrementReferralReward(ctx context.Context, referredSubjectID int64) error {
	return incrementReferralReward(ctx, db.Pool, referredSubjectID)
}

func incrementReferralReward(ctx context.Context, q Querier, referredSubjectID int64) error {
	_, err := q.Exec(ctx, `
		UPDATE referrals SET reward_count = reward_count + 1, reward_given = TRUE
		WHERE referred_subject_id = $1`, referredSubjectID)
	return err
}

// ReferralStats — сводка кабинета.
type ReferralStats struct {
	Invited     int
	RewardCount int
	EarnedDays  int
	Balance     domain.Money
}

// ReferralStatsFor собирает статистику пригласившего.
func (db *DB) ReferralStatsFor(ctx context.Context, subjectID int64) (ReferralStats, error) {
	var stats ReferralStats
	var balance int64
	err := db.Pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM referrals WHERE referrer_subject_id = $1),
			(SELECT COALESCE(sum(reward_count), 0) FROM referrals WHERE referrer_subject_id = $1),
			(SELECT COALESCE(sum(bonus_days), 0) FROM referrals WHERE referrer_subject_id = $1)`,
		subjectID).Scan(&stats.Invited, &stats.RewardCount, &stats.EarnedDays)
	if err != nil {
		return stats, err
	}
	err = db.Pool.QueryRow(ctx, `
		SELECT COALESCE(round(balance * 100)::bigint, 0) FROM user_balance
		WHERE subject_id = $1`, subjectID).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	stats.Balance = domain.Money(balance)
	return stats, err
}

// AddPendingBonus копит дни, когда у пригласившего ещё нет подписки.
func (db *DB) AddPendingBonus(ctx context.Context, subjectID int64, days int) error {
	return addPendingBonus(ctx, db.Pool, subjectID, days)
}

func addPendingBonus(ctx context.Context, q Querier, subjectID int64, days int) error {
	_, err := q.Exec(ctx, `
		INSERT INTO pending_referral_bonuses (subject_id, bonus_days)
		VALUES ($1, $2)
		ON CONFLICT (subject_id) DO UPDATE
		   SET bonus_days = pending_referral_bonuses.bonus_days + EXCLUDED.bonus_days`,
		subjectID, days)
	return err
}

// TakePendingBonus забирает накопленные дни.
func (db *DB) TakePendingBonus(ctx context.Context, subjectID int64) (int, error) {
	return takePendingBonus(ctx, db.Pool, subjectID)
}

func takePendingBonus(ctx context.Context, q Querier, subjectID int64) (int, error) {
	var days int
	err := q.QueryRow(ctx, `
		DELETE FROM pending_referral_bonuses WHERE subject_id = $1
		RETURNING bonus_days`, subjectID).Scan(&days)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return days, err
}

// Balance читает баланс субъекта.
func (db *DB) Balance(ctx context.Context, subjectID int64) (domain.Money, error) {
	var cents int64
	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(round(balance * 100)::bigint, 0) FROM user_balance
		WHERE subject_id = $1`, subjectID).Scan(&cents)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return domain.Money(cents), err
}

// AddBalance пополняет баланс и возвращает новое значение.
func (db *DB) AddBalance(ctx context.Context, subjectID int64, amount domain.Money, telegramID *int64) (domain.Money, error) {
	return addBalance(ctx, db.Pool, subjectID, amount, telegramID)
}

func addBalance(
	ctx context.Context, q Querier, subjectID int64, amount domain.Money, telegramID *int64,
) (domain.Money, error) {
	var cents int64
	err := q.QueryRow(ctx, `
		INSERT INTO user_balance (telegram_id, subject_id, balance)
		VALUES ($1, $2, $3)
		ON CONFLICT (subject_id) DO UPDATE
		   SET balance = user_balance.balance + EXCLUDED.balance
		RETURNING round(balance * 100)::bigint`,
		telegramID, subjectID, amount.String()).Scan(&cents)
	if err != nil {
		// На старых базах уникального индекса по subject_id может не быть —
		// повторяем через UPDATE, который от него не зависит.
		var updated int64
		err = q.QueryRow(ctx, `
			UPDATE user_balance SET balance = balance + $2 WHERE subject_id = $1
			RETURNING round(balance * 100)::bigint`, subjectID, amount.String()).Scan(&updated)
		return domain.Money(updated), err
	}
	return domain.Money(cents), nil
}

// DeductBalance — совместимое списание с ограничением доступным остатком.
// Для покупок используется CreateBalanceOrder: частичное списание там запрещено.
func (db *DB) DeductBalance(ctx context.Context, subjectID int64, amount domain.Money) (domain.Money, error) {
	var cents int64
	err := db.Pool.QueryRow(ctx, `
		UPDATE user_balance
		SET balance = GREATEST(balance - $2, 0)
		WHERE subject_id = $1 AND $2::numeric > 0
		RETURNING round(balance * 100)::bigint`, subjectID, amount.String()).Scan(&cents)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return domain.Money(cents), err
}

// ReferralMode читает выбранный способ вознаграждения.
func (db *DB) ReferralMode(ctx context.Context, subjectID int64) (string, error) {
	return referralMode(ctx, db.Pool, subjectID)
}

func referralMode(ctx context.Context, q Querier, subjectID int64) (string, error) {
	var mode string
	err := q.QueryRow(ctx, `
		SELECT mode FROM user_referral_mode WHERE subject_id = $1`, subjectID).Scan(&mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return mode, err
}

// SetReferralMode сохраняет способ вознаграждения.
func (db *DB) SetReferralMode(ctx context.Context, subjectID int64, mode string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO user_referral_mode (subject_id, mode) VALUES ($1, $2)
		ON CONFLICT (subject_id) DO UPDATE SET mode = EXCLUDED.mode`, subjectID, mode)
	return err
}

// CreateWithdrawal создаёт заявку на вывод.
func (db *DB) CreateWithdrawal(ctx context.Context, subjectID int64, telegramID *int64, username, method, details string, amount domain.Money) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO withdrawal_requests (subject_id, telegram_id, username, amount, method, recipient_details)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		subjectID, telegramID, username, amount.String(), method, details)
	return err
}

// Withdrawal — заявка на вывод.
type Withdrawal struct {
	ID         int64
	SubjectID  *int64
	TelegramID *int64
	Username   *string
	Amount     domain.Money
	Method     string
	Details    string
	Status     string
	CreatedAt  string
}

// Withdrawals читает заявки по статусу.
func (db *DB) Withdrawals(ctx context.Context, status string, limit int) ([]Withdrawal, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, subject_id, telegram_id, username, round(amount * 100)::bigint,
		       method, recipient_details, status, created_at::text
		FROM withdrawal_requests WHERE ($1 = '' OR status = $1)
		ORDER BY created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Withdrawal
	for rows.Next() {
		var item Withdrawal
		var cents int64
		if err := rows.Scan(&item.ID, &item.SubjectID, &item.TelegramID, &item.Username,
			&cents, &item.Method, &item.Details, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Amount = domain.Money(cents)
		out = append(out, item)
	}
	return out, rows.Err()
}

// DecideWithdrawal закрывает заявку решением staff.
func (db *DB) DecideWithdrawal(ctx context.Context, id int64, status string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE withdrawal_requests SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}
