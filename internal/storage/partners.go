package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// CreatePartner заводит партнёра программы.
func (db *DB) CreatePartner(ctx context.Context, name, contact, payoutDetails, notes string) (domain.Partner, error) {
	row := db.Pool.QueryRow(ctx, `
		INSERT INTO partners (name, contact, payout_details, notes, status)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id, subject_id, name, contact, status, payout_details, notes, created_at`,
		name, contact, payoutDetails, notes)
	return scanPartner(row)
}

// GetPartner читает партнёра; nil — его нет.
func (db *DB) GetPartner(ctx context.Context, partnerID int64) (*domain.Partner, error) {
	return getPartner(ctx, db.Pool, partnerID)
}

func getPartner(ctx context.Context, q Querier, partnerID int64) (*domain.Partner, error) {
	row := q.QueryRow(ctx, `
		SELECT id, subject_id, name, contact, status, payout_details, notes, created_at
		FROM partners WHERE id = $1`, partnerID)
	partner, err := scanPartner(row)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &partner, nil
}

// GetPartnerBySubject ищет партнёра по привязанному аккаунту.
func (db *DB) GetPartnerBySubject(ctx context.Context, subjectID int64) (*domain.Partner, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT id, subject_id, name, contact, status, payout_details, notes, created_at
		FROM partners WHERE subject_id = $1`, subjectID)
	partner, err := scanPartner(row)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &partner, nil
}

// ListPartners читает список с сводками: сколько кодов, клиентов и начислено.
func (db *DB) ListPartners(ctx context.Context) ([]domain.PartnerSummary, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT p.id, p.subject_id, p.name, p.contact, p.status, p.payout_details,
		       p.notes, p.created_at,
		       (SELECT count(*) FROM promocodes c WHERE c.partner_id = p.id),
		       (SELECT count(*) FROM partner_attributions a WHERE a.partner_id = p.id),
		       COALESCE((SELECT sum(r.amount) FROM partner_rewards r
		                 WHERE r.partner_id = p.id AND r.status <> 'cancelled'), 0),
		       COALESCE((SELECT sum(r.amount) FROM partner_rewards r
		                 WHERE r.partner_id = p.id AND r.status = 'accrued'), 0)
		FROM partners p ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PartnerSummary
	for rows.Next() {
		var item domain.PartnerSummary
		var rewarded, accrued int64
		if err := rows.Scan(&item.ID, &item.SubjectID, &item.Name, &item.Contact,
			&item.Status, &item.PayoutDetails, &item.Notes, &item.CreatedAt,
			&item.CodesCount, &item.ClientsCount, &rewarded, &accrued); err != nil {
			return nil, err
		}
		item.Rewarded = domain.Money(rewarded)
		item.Accrued = domain.Money(accrued)
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpdatePartner меняет карточку партнёра. Пустые поля не трогаем.
func (db *DB) UpdatePartner(ctx context.Context, partnerID int64, name, contact, payoutDetails, notes, status string) (*domain.Partner, error) {
	row := db.Pool.QueryRow(ctx, `
		UPDATE partners SET
			name = COALESCE(NULLIF($2, ''), name),
			contact = COALESCE(NULLIF($3, ''), contact),
			payout_details = COALESCE(NULLIF($4, ''), payout_details),
			notes = COALESCE(NULLIF($5, ''), notes),
			status = COALESCE(NULLIF($6, ''), status)
		WHERE id = $1
		RETURNING id, subject_id, name, contact, status, payout_details, notes, created_at`,
		partnerID, name, contact, payoutDetails, notes, status)
	partner, err := scanPartner(row)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &partner, nil
}

// LinkPartnerSubject привязывает аккаунт к партнёрству.
//
// Накопленное до привязки сразу уходит на баланс: партнёр не должен терять
// деньги, которые пришли, пока кабинета ещё не было.
func (db *DB) LinkPartnerSubject(ctx context.Context, partnerID, subjectID int64) (*domain.Partner, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Привязка делается один раз: UPDATE без WHERE subject_id IS NULL
	// ничего не меняет, если аккаунт уже привязан.
	tag, err := tx.Exec(ctx, `
		UPDATE partners SET subject_id = $2
		WHERE id = $1 AND subject_id IS NULL`, partnerID, subjectID)
	if err != nil {
		return nil, err
	}
	linked := tag.RowsAffected() > 0
	if !linked {
		// Уже привязан: возвращаем как есть, ничего не пересчитываем.
		row := tx.QueryRow(ctx, `
			SELECT id, subject_id, name, contact, status, payout_details, notes, created_at
			FROM partners WHERE id = $1`, partnerID)
		partner, err := scanPartner(row)
		if err != nil {
			return nil, err
		}
		return &partner, tx.Commit(ctx)
	}

	var total int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(round(sum(amount) * 100)::bigint, 0) FROM partner_rewards
		WHERE partner_id = $1 AND status = 'accrued'`, partnerID).Scan(&total); err != nil {
		return nil, err
	}
	if total > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_balance (subject_id, balance)
			VALUES ($1, $2)
			ON CONFLICT (subject_id) DO UPDATE
			   SET balance = user_balance.balance + EXCLUDED.balance`,
			subjectID, domain.Money(total).String()); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE partner_rewards SET status = 'credited'
			WHERE partner_id = $1 AND status = 'accrued'`, partnerID); err != nil {
			return nil, err
		}
	}
	row := tx.QueryRow(ctx, `
		SELECT id, subject_id, name, contact, status, payout_details, notes, created_at
		FROM partners WHERE id = $1`, partnerID)
	partner, err := scanPartner(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &partner, nil
}

// CreatePartnerInviteCode выдаёт код привязки партнёра к кабинету.
func (db *DB) CreatePartnerInviteCode(ctx context.Context, partnerID int64, ttl time.Duration) (string, error) {
	code := randomHex(6)
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO partner_invite_codes (code, partner_id, expires_at)
		VALUES ($1, $2, $3)`, code, partnerID, time.Now().Add(ttl))
	return code, err
}

// ConsumePartnerInviteCode забирает код привязки. nil — кода нет или он истёк.
func (db *DB) ConsumePartnerInviteCode(ctx context.Context, code string) (*int64, error) {
	var partnerID int64
	err := db.Pool.QueryRow(ctx, `
		UPDATE partner_invite_codes SET used_at = now()
		WHERE code = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING partner_id`, code).Scan(&partnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &partnerID, nil
}

// CreatePartnerAttribution запоминает, от кого пришёл клиент.
//
// ON CONFLICT DO NOTHING: клиент привязан к одному партнёру, и счётчик
// партнёра не должен расти от повторных переходов по ссылке.
func (db *DB) CreatePartnerAttribution(ctx context.Context, subjectID, partnerID int64, code, source string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO partner_attributions (subject_id, partner_id, code, source)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (subject_id) DO NOTHING`, subjectID, partnerID, nullString(code), source)
	return err
}

// PartnerAttribution читает привязку клиента.
func (db *DB) PartnerAttribution(ctx context.Context, subjectID int64) (*domain.PartnerAttribution, error) {
	var item domain.PartnerAttribution
	err := db.Pool.QueryRow(ctx, `
		SELECT subject_id, partner_id, code, source, created_at
		FROM partner_attributions WHERE subject_id = $1`, subjectID).Scan(
		&item.SubjectID, &item.PartnerID, &item.Code, &item.Source, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// PartnerRewardCount — сколько начислений партнёру по этому клиенту.
func (db *DB) PartnerRewardCount(ctx context.Context, partnerID, subjectID int64) (int, error) {
	return partnerRewardCount(ctx, db.Pool, partnerID, subjectID)
}

func partnerRewardCount(ctx context.Context, q Querier, partnerID, subjectID int64) (int, error) {
	var count int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM partner_rewards
		WHERE partner_id = $1 AND subject_id = $2 AND status <> 'cancelled'`,
		partnerID, subjectID).Scan(&count)
	return count, err
}

// CreditPartnerReward начисляет партнёру вознаграждение за заказ.
//
// Начисление по заказу может быть только одно: UNIQUE(order_id) и
// ON CONFLICT DO NOTHING не дают задвоить при повторном вебхуке. Если у
// партнёра есть аккаунт, сумма сразу уходит на баланс.
func (db *DB) CreditPartnerReward(
	ctx context.Context, partnerID int64, orderID string, subjectID *int64,
	amount domain.Money, partnerSubjectID *int64,
) (*domain.PartnerReward, error) {
	return creditPartnerReward(ctx, db.Pool, partnerID, orderID, subjectID, amount, partnerSubjectID)
}

func creditPartnerReward(
	ctx context.Context, q Querier, partnerID int64, orderID string, subjectID *int64,
	amount domain.Money, partnerSubjectID *int64,
) (*domain.PartnerReward, error) {
	var reward domain.PartnerReward
	err := q.QueryRow(ctx, `
		INSERT INTO partner_rewards (partner_id, order_id, subject_id, amount, status)
		VALUES ($1, $2, $3, $4, 'accrued')
		ON CONFLICT (order_id) DO NOTHING
		RETURNING id, partner_id, order_id, subject_id, round(amount * 100)::bigint, status, created_at`,
		partnerID, orderID, subjectID, amount.String()).Scan(
		&reward.ID, &reward.PartnerID, &reward.OrderID, &reward.SubjectID,
		&reward.Amount, &reward.Status, &reward.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Заказ уже оплачен партнёру: повторное начисление не нужно.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if partnerSubjectID != nil {
		if _, err := q.Exec(ctx, `
			INSERT INTO user_balance (subject_id, balance)
			VALUES ($1, $2)
			ON CONFLICT (subject_id) DO UPDATE
			   SET balance = user_balance.balance + EXCLUDED.balance`,
			*partnerSubjectID, amount.String()); err != nil {
			return nil, err
		}
		if err := q.QueryRow(ctx, `
			UPDATE partner_rewards SET status = 'credited' WHERE id = $1
			RETURNING id, partner_id, order_id, subject_id, round(amount * 100)::bigint, status, created_at`,
			reward.ID).Scan(&reward.ID, &reward.PartnerID, &reward.OrderID, &reward.SubjectID,
			&reward.Amount, &reward.Status, &reward.CreatedAt); err != nil {
			return nil, err
		}
	}
	return &reward, nil
}

// PartnerRewards — начисления партнёра, новые сверху.
func (db *DB) PartnerRewards(ctx context.Context, partnerID int64, limit int) ([]domain.PartnerReward, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, partner_id, order_id, subject_id, round(amount * 100)::bigint, status, created_at
		FROM partner_rewards WHERE partner_id = $1
		ORDER BY created_at DESC LIMIT $2`, partnerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PartnerReward
	for rows.Next() {
		var item domain.PartnerReward
		if err := rows.Scan(&item.ID, &item.PartnerID, &item.OrderID, &item.SubjectID,
			&item.Amount, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// PartnerCodes — промокоды партнёра.
func (db *DB) PartnerCodes(ctx context.Context, partnerID int64) ([]domain.PromoCode, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT `+promoColumns+` FROM promocodes WHERE partner_id = $1 ORDER BY created_at DESC`, partnerID)
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

// DeactivatePartnerCodes выключает промокоды партнёра — на паузе партнёр не
// должен приводить людей.
func (db *DB) DeactivatePartnerCodes(ctx context.Context, partnerID int64) (int, error) {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE promocodes SET is_active = FALSE, updated_at = now()
		WHERE partner_id = $1 AND is_active`, partnerID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func scanPartner(row pgx.Row) (domain.Partner, error) {
	var partner domain.Partner
	err := row.Scan(&partner.ID, &partner.SubjectID, &partner.Name, &partner.Contact,
		&partner.Status, &partner.PayoutDetails, &partner.Notes, &partner.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Partner{}, ErrNotFound
	}
	return partner, err
}
