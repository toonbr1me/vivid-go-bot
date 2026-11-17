package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

const orderColumns = `
	id, order_id, provider, telegram_id, account_id, username, plan_id, kind,
	round(amount * 100)::bigint,
	COALESCE(round(original_amount * 100)::bigint, round(amount * 100)::bigint),
	currency, payment_method, status, payment_url, payment_link_id,
	payment_expires_at, transaction_id, promo_code, plan_days, promo_bonus_days,
	granted_days, referrer_id, partner_id, reward_type,
	round(reward_value * 100)::bigint, reward_scope,
	device_count, device_days, device_expires_at, renew_grant_id,
	round(device_renewal_amount * 100)::bigint,
	created_at, updated_at, paid_at, provisioned_at, expire_at,
	fulfillment_version, fulfillment_attempts, fulfillment_error,
	notifications_completed_at,recurring_agreement_id,plan_profile`

func moneyArg(value domain.Money) string { return value.String() }

func scanOrder(row pgx.Row) (domain.Order, error) {
	var order domain.Order
	var amount, original, rewardValue, renewalAmount int64
	err := row.Scan(
		&order.ID,
		&order.OrderID, &order.Provider, &order.TelegramID, &order.AccountID,
		&order.Username, &order.PlanID, &order.Kind, &amount, &original,
		&order.Currency, &order.PaymentMethod, &order.Status, &order.PaymentURL,
		&order.PaymentLinkID, &order.PaymentExpiresAt, &order.TransactionID,
		&order.PromoCode, &order.PlanDays, &order.PromoBonusDays, &order.GrantedDays,
		&order.ReferrerID, &order.PartnerID, &order.RewardType, &rewardValue,
		&order.RewardScope, &order.DeviceCount, &order.DeviceDays,
		&order.DeviceExpiresAt, &order.RenewGrantID, &renewalAmount,
		&order.CreatedAt, &order.UpdatedAt, &order.PaidAt, &order.ProvisionedAt,
		&order.ExpireAt, &order.FulfillmentVer, &order.FulfillmentTries,
		&order.FulfillmentError, &order.NotificationDone, &order.RecurringAgreementID, &order.PlanProfile,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, ErrNotFound
	}
	if err != nil {
		return domain.Order{}, err
	}
	order.Amount = domain.Money(amount)
	order.OriginalAmount = domain.Money(original)
	order.RewardValue = domain.Money(rewardValue)
	order.RenewalAmount = domain.Money(renewalAmount)
	return order, nil
}

// CreateOrder сохраняет новый заказ и возвращает его с присвоенным id.
func (db *DB) CreateOrder(ctx context.Context, order domain.Order) (domain.Order, error) {
	if order.Kind != domain.KindGift && (order.PromoCode == nil || *order.PromoCode == "") {
		return createOrder(ctx, db.Pool, order)
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)
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

func createOrder(ctx context.Context, q Querier, order domain.Order) (domain.Order, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO orders (
			order_id, provider, telegram_id, account_id, username, plan_id, kind,
			amount, original_amount, currency, payment_method, status,
			payment_url, payment_expires_at, promo_code, plan_days, promo_bonus_days,
			referrer_id, partner_id, reward_type, reward_value, reward_scope,
			device_count, device_days, device_expires_at, renew_grant_id,
			device_renewal_amount, fulfillment_version, recurring_agreement_id,plan_profile
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19, $20, $21, $22,
			$23, $24, $25, $26, $27, 1, $28,$29
		)
		RETURNING `+orderColumns,
		order.OrderID, order.Provider, order.TelegramID, order.AccountID,
		order.Username, order.PlanID, order.Kind,
		moneyArg(order.Amount), moneyArg(order.OriginalAmount), order.Currency,
		order.PaymentMethod, order.Status, order.PaymentURL, order.PaymentExpiresAt,
		order.PromoCode, order.PlanDays, order.PromoBonusDays, order.ReferrerID,
		order.PartnerID, order.RewardType, moneyArg(order.RewardValue), order.RewardScope,
		order.DeviceCount, order.DeviceDays, order.DeviceExpiresAt, order.RenewGrantID,
		moneyArg(order.RenewalAmount), order.RecurringAgreementID, order.PlanProfile,
	)
	created, err := scanOrder(row)
	if err != nil {
		return domain.Order{}, err
	}
	if created.Kind == domain.KindGift {
		if created.AccountID == nil || created.PlanDays == nil || created.DeviceCount == nil {
			return domain.Order{}, errors.New("у подарка нет покупателя или условий")
		}
		_, err = q.Exec(ctx, `INSERT INTO subscription_gifts(id,buyer_id,purchase_order_id,plan_id,days,devices,profile) VALUES($1,$2,$3,$4,$5,$6,$7)`, operationToken(), *created.AccountID, created.OrderID, created.PlanID, *created.PlanDays, *created.DeviceCount, created.PlanProfile)
		if err != nil {
			return domain.Order{}, err
		}
	}
	return created, nil
}

// OrderByID читает заказ по внешнему идентификатору.
func (db *DB) OrderByID(ctx context.Context, orderID string) (domain.Order, error) {
	row := db.Pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE order_id = $1`, orderID)
	return scanOrder(row)
}

// LivePendingOrders возвращает неоплаченные заказы субъекта, ссылка на оплату
// которых ещё жива. Нужны, чтобы повторное нажатие продолжало ту же оплату.
func (db *DB) LivePendingOrders(ctx context.Context, telegramID, accountID *int64, kind string) ([]domain.Order, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT `+orderColumns+` FROM orders
		WHERE status = 'pending' AND kind = $3
		  AND (payment_url IS NOT NULL)
		  AND payment_expires_at > now()
		  AND (($1::bigint IS NOT NULL AND telegram_id = $1)
		       OR ($2::bigint IS NOT NULL AND account_id = $2))
		ORDER BY created_at DESC`, telegramID, accountID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, order)
	}
	return out, rows.Err()
}

// SetPaymentLink записывает ссылку на оплату и срок её жизни.
func (db *DB) SetPaymentLink(ctx context.Context, orderID, url, linkID string, expiresAt *time.Time) error {
	var linkArg any
	if linkID != "" {
		linkArg = linkID
	}
	_, err := db.Pool.Exec(ctx, `
		UPDATE orders SET payment_url = $2, payment_link_id = $3,
		       payment_expires_at = $4, updated_at = now()
		WHERE order_id = $1`, orderID, url, linkArg, expiresAt)
	return err
}

// MarkPaid переводит подтверждённый платёж в paid. Оплата приоритетнее
// прежнего отказа/отмены; уже оплаченный заказ повторно не меняется.
func (db *DB) MarkPaid(ctx context.Context, orderID, transactionID string, paidAt time.Time) (bool, error) {
	var transaction any
	if transactionID != "" {
		transaction = transactionID
	}
	tag, err := db.Pool.Exec(ctx, `
		UPDATE orders SET status = 'paid', paid_at = $3, transaction_id = $2,
		       payment_expires_at = NULL, updated_at = now()
		WHERE order_id = $1 AND status IN ('pending','failed','cancelled')`, orderID, transaction, paidAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkProvisioned фиксирует выдачу заказа.
func (db *DB) MarkProvisioned(ctx context.Context, orderID string, expireAt *time.Time, grantedDays *int) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE orders SET status = 'provisioned', provisioned_at = now(),
		       expire_at = $2, granted_days = $3, updated_at = now()
		WHERE order_id = $1`, orderID, expireAt, grantedDays)
	return err
}

// MarkOrderStatus меняет статус заказа.
func (db *DB) MarkOrderStatus(ctx context.Context, orderID, status string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE orders SET status = $2, updated_at = now() WHERE order_id = $1 AND status = 'pending'`, orderID, status)
	return err
}

// SetOrderReward записывает параметры вознаграждения партнёра в заказ.
func (db *DB) SetOrderReward(ctx context.Context, orderID string, partnerID int64, rewardType string, rewardValue domain.Money, rewardScope string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE orders SET partner_id = $2, reward_type = $3, reward_value = $4,
		       reward_scope = $5, updated_at = now()
		WHERE order_id = $1`, orderID, partnerID, rewardType, moneyArg(rewardValue), rewardScope)
	return err
}

// ScheduleProvisionRetry откладывает повторную выдачу оплаченного заказа.
func (db *DB) ScheduleProvisionRetry(ctx context.Context, orderID, reason string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE orders SET fulfillment_attempts = fulfillment_attempts + 1,
		       fulfillment_error = $2, fulfillment_retry_at = now() + interval '5 minutes',
		       updated_at = now()
		WHERE order_id = $1`, orderID, reason)
	return err
}

// PendingProvisioning — оплаченные заказы, которые ещё не выданы.
func (db *DB) PendingProvisioning(ctx context.Context, limit int) ([]domain.Order, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT `+orderColumns+` FROM orders
		WHERE status = 'paid' AND provisioned_at IS NULL
		  AND (fulfillment_retry_at IS NULL OR fulfillment_retry_at <= now())
		ORDER BY paid_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, order)
	}
	return out, rows.Err()
}

// OrdersForTelegram — история заказов пользователя бота.
func (db *DB) OrdersForTelegram(ctx context.Context, telegramID int64, limit int) ([]domain.Order, error) {
	return db.queryOrders(ctx, `
		SELECT `+orderColumns+` FROM orders WHERE telegram_id = $1
		ORDER BY created_at DESC LIMIT $2`, telegramID, limit)
}

// OrdersForAccount — история заказов веб-аккаунта.
func (db *DB) OrdersForAccount(ctx context.Context, accountID int64, limit int) ([]domain.Order, error) {
	return db.queryOrders(ctx, `
		SELECT `+orderColumns+` FROM orders WHERE account_id = $1
		ORDER BY created_at DESC LIMIT $2`, accountID, limit)
}

func (db *DB) OrderHistoryForSubject(ctx context.Context, subject domain.Subject, limit int) ([]domain.Order, error) {
	return db.queryOrders(ctx, `SELECT `+orderColumns+` FROM orders WHERE account_id = $1
		OR ($2::bigint IS NOT NULL AND telegram_id = $2) ORDER BY created_at DESC LIMIT $3`,
		subject.ID, subject.TelegramID, limit)
}

// RecentOrders — последние заказы всех клиентов. Для админки.
func (db *DB) RecentOrders(ctx context.Context, limit int) ([]domain.Order, error) {
	return db.queryOrders(ctx, `
		SELECT `+orderColumns+` FROM orders
		ORDER BY created_at DESC LIMIT $1`, limit)
}

func (db *DB) queryOrders(ctx context.Context, sql string, args ...any) ([]domain.Order, error) {
	rows, err := db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Order
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, order)
	}
	return out, rows.Err()
}

// HasTrial проверяет, брал ли субъект пробный период этого вида.
func (db *DB) HasTrial(ctx context.Context, subjectID int64, kind string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM trials WHERE subject_id = $1 AND kind = $2)`,
		subjectID, kind).Scan(&exists)
	return exists, err
}

// RecordTrial фиксирует выдачу пробного периода. false — уже был.
func (db *DB) RecordTrial(ctx context.Context, subjectID int64, telegramID *int64, kind string) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `
		INSERT INTO trials (subject_id, telegram_id, kind) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, subjectID, telegramID, kind)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// HasPaidProvisioning — покупал ли субъект что-нибудь платное.
func (db *DB) HasPaidProvisioning(ctx context.Context, subjectID int64) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM orders o JOIN accounts a ON a.id = $1
			WHERE (o.account_id = a.id
			       OR (a.telegram_id IS NOT NULL AND o.telegram_id = a.telegram_id))
			  AND o.status IN ('paid', 'provisioned')
			  AND o.kind = 'subscription' AND o.amount > 0
		)`, subjectID).Scan(&exists)
	return exists, err
}

// ExpiringSubscriptions — заказы, у которых срок подходит к концу.
func (db *DB) ExpiringSubscriptions(ctx context.Context, from, to time.Time) ([]domain.Order, error) {
	return db.queryOrders(ctx, `
		SELECT `+strings.Replace(orderColumns, "telegram_id", "recipient_telegram_id", 1)+` FROM (
			SELECT DISTINCT ON (a.id) o.*, a.telegram_id AS recipient_telegram_id FROM orders o
			JOIN accounts a ON (o.account_id=a.id OR (o.account_id IS NULL AND o.telegram_id=a.telegram_id))
			WHERE a.telegram_id IS NOT NULL AND o.status = 'provisioned'
			  AND o.kind = 'subscription' AND o.expire_at IS NOT NULL
			ORDER BY a.id, o.expire_at DESC, o.created_at DESC
		) latest WHERE expire_at BETWEEN $1 AND $2`, from, to)
}
