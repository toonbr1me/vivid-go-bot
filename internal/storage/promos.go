package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

const promoColumns = `
	code, discount_percent, round(discount_fixed * 100)::bigint,
	repeat_discount_percent, round(repeat_discount_fixed * 100)::bigint,
	bonus_days, grant_days, max_uses, used_count, per_user_limit,
	is_active, starts_at, expires_at, plan_ids, partner_id,
	reward_type, round(reward_value * 100)::bigint, reward_scope`

func scanPromo(row pgx.Row) (domain.PromoCode, error) {
	var promo domain.PromoCode
	var discountFixed, repeatFixed, rewardValue int64
	err := row.Scan(
		&promo.Code, &promo.DiscountPercent, &discountFixed,
		&promo.RepeatDiscountPct, &repeatFixed,
		&promo.BonusDays, &promo.GrantDays, &promo.MaxUses, &promo.UsedCount,
		&promo.PerUserLimit, &promo.IsActive, &promo.StartsAt, &promo.ExpiresAt,
		&promo.PlanIDs, &promo.PartnerID, &promo.RewardType, &rewardValue,
		&promo.RewardScope,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PromoCode{}, ErrNotFound
	}
	promo.DiscountFixed = domain.Money(discountFixed)
	promo.RepeatDiscountFixed = domain.Money(repeatFixed)
	promo.RewardValue = domain.Money(rewardValue)
	return promo, err
}

// PromoCode читает код по каноническому написанию.
func (db *DB) PromoCode(ctx context.Context, code string) (domain.PromoCode, error) {
	row := db.Pool.QueryRow(ctx, `SELECT `+promoColumns+` FROM promocodes WHERE code = $1`, code)
	return scanPromo(row)
}

// UsePromo увеличивает счётчик применений.
func (db *DB) UsePromo(ctx context.Context, code string) error {
	return usePromo(ctx, db.Pool, code)
}

func usePromo(ctx context.Context, q Querier, code string) error {
	_, err := q.Exec(ctx,
		`UPDATE promocodes SET used_count = used_count + 1, updated_at = now() WHERE code = $1`, code)
	return err
}

// RecordPromoActivation пишет журнал применений на субъекта.
func (db *DB) RecordPromoActivation(ctx context.Context, subjectID int64, code string) error {
	return recordPromoActivation(ctx, db.Pool, subjectID, code)
}

func recordPromoActivation(ctx context.Context, q Querier, subjectID int64, code string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO promo_activations (subject_id, code) VALUES ($1, $2)`, subjectID, code)
	return err
}

// PromoActivationsForCode — сколько раз субъект применял конкретный код.
func (db *DB) PromoActivationsForCode(ctx context.Context, subjectID int64, code string) (int, error) {
	var count int
	err := db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM promo_activations WHERE subject_id = $1 AND code = $2`,
		subjectID, code).Scan(&count)
	return count, err
}

// DistinctPromoCount — сколько разных кодов субъект уже применял.
func (db *DB) DistinctPromoCount(ctx context.Context, subjectID int64) (int, error) {
	var count int
	err := db.Pool.QueryRow(ctx, `
		SELECT count(DISTINCT code) FROM promo_activations WHERE subject_id = $1`,
		subjectID).Scan(&count)
	return count, err
}

// ListPromos — каталог кодов для админки.
func (db *DB) ListPromos(ctx context.Context, limit int) ([]domain.PromoCode, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT `+promoColumns+` FROM promocodes ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PromoCode
	for rows.Next() {
		promo, err := scanPromo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, promo)
	}
	return out, rows.Err()
}

// CreatePromo добавляет код.
func (db *DB) CreatePromo(ctx context.Context, promo domain.PromoCode) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO promocodes (
			code, discount_percent, discount_fixed, bonus_days, grant_days,
			max_uses, per_user_limit, is_active, starts_at, expires_at, plan_ids,
			partner_id, reward_type, reward_value, reward_scope, created_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		promo.Code, promo.DiscountPercent, moneyArg(promo.DiscountFixed),
		promo.BonusDays, promo.GrantDays, promo.MaxUses, promo.PerUserLimit,
		promo.IsActive, promo.StartsAt, promo.ExpiresAt, promo.PlanIDs,
		promo.PartnerID, promo.RewardType, moneyArg(promo.RewardValue),
		promo.RewardScope, nil)
	return err
}

// UpdatePromoPartner перевешивает код на партнёра и задаёт его награду.
func (db *DB) UpdatePromoPartner(ctx context.Context, code string, partnerID *int64, rewardType string, rewardValue domain.Money, rewardScope string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE promocodes SET partner_id = $2, reward_type = $3, reward_value = $4,
		       reward_scope = $5, updated_at = now()
		WHERE code = $1`, code, partnerID, rewardType, moneyArg(rewardValue), rewardScope)
	return err
}

// SetPromoActive включает и выключает код.
func (db *DB) SetPromoActive(ctx context.Context, code string, active bool) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE promocodes SET is_active = $2, updated_at = now() WHERE code = $1`, code, active)
	return err
}

// GiftPromoByCode — подарочный код для выдачи дней без оплаты.
func (db *DB) GiftPromoByCode(ctx context.Context, code string) (domain.PromoCode, error) {
	promo, err := db.PromoCode(ctx, code)
	if err != nil {
		return promo, err
	}
	if !promo.IsGiftPromo() {
		return promo, ErrNotFound
	}
	return promo, nil
}

// RememberPromoIntent запоминает введённый, но ещё не потраченный код.
func (db *DB) RememberPromoIntent(ctx context.Context, subjectID int64, code string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE promo_intents SET consumed_at = now()
		WHERE subject_id = $1 AND consumed_at IS NULL;
		`, subjectID)
	if err != nil {
		return err
	}
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO promo_intents (subject_id, code) VALUES ($1, $2)`, subjectID, code)
	return err
}

// PromoIntent — действующий введённый код.
func (db *DB) PromoIntent(ctx context.Context, subjectID int64) (string, bool, error) {
	var code string
	err := db.Pool.QueryRow(ctx, `
		SELECT code FROM promo_intents
		WHERE subject_id = $1 AND consumed_at IS NULL
		ORDER BY id DESC LIMIT 1`, subjectID).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return code, err == nil, err
}

// ConsumePromoIntent закрывает введённый код после покупки.
func (db *DB) ConsumePromoIntent(ctx context.Context, subjectID int64) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE promo_intents SET consumed_at = now()
		WHERE subject_id = $1 AND consumed_at IS NULL`, subjectID)
	return err
}

// RecordPromoVisit считает переход по партнёрской ссылке.
func (db *DB) RecordPromoVisit(ctx context.Context, code string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE promocodes SET visit_count = visit_count + 1 WHERE code = $1`, code)
	return err
}
