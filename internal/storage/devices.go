package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// GrantDeviceSlots записывает купленные слоты. false — заказ уже начислен
// (повторный вебхук); второй раз устройства не выдаются.
func (db *DB) GrantDeviceSlots(
	ctx context.Context,
	orderID string, subjectID int64, devices int,
	packID, planID *string, expiresAt *time.Time,
) (bool, error) {
	return grantDeviceSlots(ctx, db.Pool, orderID, subjectID, devices, packID, planID, expiresAt)
}

func grantDeviceSlots(
	ctx context.Context, q Querier, orderID string, subjectID int64, devices int,
	packID, planID *string, expiresAt *time.Time,
) (bool, error) {
	tag, err := q.Exec(ctx, `
		INSERT INTO device_slot_grants (order_id, subject_id, devices, pack_id, plan_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (order_id) DO NOTHING`,
		orderID, subjectID, devices, packID, planID, expiresAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RenounceDeviceSlots снимает начисление вместе с заказом.
func (db *DB) RenounceDeviceSlots(ctx context.Context, orderID string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM device_slot_grants WHERE order_id = $1`, orderID)
	return err
}

// ExtraDeviceSlots — сколько устройств субъект докупил сверх базового лимита.
//
// Тарифные начисления не складываются, а берутся по максимуму: «Семейный»
// дважды подряд — это те же семь устройств на два месяца, а не четырнадцать
// на первый. Пакеты поверх тарифа складываются как обычно.
func (db *DB) ExtraDeviceSlots(ctx context.Context, subjectID int64) (int, error) {
	var total int
	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(devices) FILTER (WHERE plan_id IS NOT NULL), 0)
		     + COALESCE(SUM(devices) FILTER (WHERE pack_id IS NOT NULL), 0)
		  FROM device_slot_grants
		 WHERE subject_id = $1 AND (expires_at IS NULL OR expires_at > now())`,
		subjectID).Scan(&total)
	return total, err
}

// ActivePlanGrant — слоты, пришедшие вместе с действующим тарифом.
func (db *DB) ActivePlanGrant(ctx context.Context, subjectID int64) (*DeviceGrant, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT order_id, subject_id, devices, plan_id, expires_at
		FROM device_slot_grants
		WHERE subject_id = $1 AND plan_id IS NOT NULL
		  AND expires_at > now()
		ORDER BY devices DESC, expires_at DESC LIMIT 1`, subjectID)
	var grant DeviceGrant
	err := row.Scan(&grant.OrderID, &grant.SubjectID, &grant.Devices, &grant.PlanID, &grant.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &grant, err
}

// DeviceGrant — начисление слотов.
type DeviceGrant struct {
	SubjectID int64
	OrderID   string
	Devices   int
	PackID    *string
	PlanID    *string
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// DeviceGrantByOrder читает начисление по заказу.
func (db *DB) DeviceGrantByOrder(ctx context.Context, orderID string) (*DeviceGrant, error) {
	var grant DeviceGrant
	err := db.Pool.QueryRow(ctx, `
		SELECT order_id, subject_id, devices, pack_id, plan_id, expires_at, created_at
		FROM device_slot_grants WHERE order_id = $1`, orderID).Scan(
		&grant.OrderID, &grant.SubjectID, &grant.Devices, &grant.PackID, &grant.PlanID,
		&grant.ExpiresAt, &grant.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &grant, err
}

// ListDeviceGrants — все начисления субъекта, новые сверху.
func (db *DB) ListDeviceGrants(ctx context.Context, subjectID int64) ([]DeviceGrant, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT order_id, subject_id, devices, pack_id, plan_id, expires_at, created_at
		FROM device_slot_grants WHERE subject_id = $1
		ORDER BY created_at DESC`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceGrant
	for rows.Next() {
		var grant DeviceGrant
		if err := rows.Scan(&grant.OrderID, &grant.SubjectID, &grant.Devices, &grant.PackID,
			&grant.PlanID, &grant.ExpiresAt, &grant.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

// RenewDeviceUntil сдвигает срок начисления. false — продление уже записано.
func (db *DB) RenewDeviceUntil(
	ctx context.Context, orderID, grantOrderID string, expiresAt time.Time, days int,
) (bool, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		INSERT INTO device_slot_renewals (order_id, grant_order_id, days)
		VALUES ($1, $2, $3) ON CONFLICT (order_id) DO NOTHING`,
		orderID, grantOrderID, days)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE device_slot_grants SET expires_at = $2 WHERE order_id = $1`,
		grantOrderID, expiresAt); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ExpiredGrants — истёкшие начисления, по которым лимит ещё не опущен.
func (db *DB) ExpiredGrants(ctx context.Context, limit int) ([]DeviceGrant, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT order_id, subject_id, devices, pack_id, plan_id, expires_at, created_at
		FROM device_slot_grants
		WHERE expires_at IS NOT NULL AND expires_at <= now() AND expiry_applied_at IS NULL
		ORDER BY expires_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceGrant
	for rows.Next() {
		var grant DeviceGrant
		if err := rows.Scan(&grant.OrderID, &grant.SubjectID, &grant.Devices, &grant.PackID,
			&grant.PlanID, &grant.ExpiresAt, &grant.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

// MarkGrantExpiryApplied отмечает, что лимит в панели уже пересчитан.
func (db *DB) MarkGrantExpiryApplied(ctx context.Context, orderIDs []string) error {
	if len(orderIDs) == 0 {
		return nil
	}
	_, err := db.Pool.Exec(ctx, `
		UPDATE device_slot_grants SET expiry_applied_at = now()
		WHERE order_id = ANY($1)`, orderIDs)
	return err
}

// DeviceLimitSyncState — последний выставленный лимит.
func (db *DB) DeviceLimitSyncState(ctx context.Context, subjectID int64) (string, int, bool, error) {
	var panelUserID string
	var limit int
	err := db.Pool.QueryRow(ctx, `
		SELECT panel_user_id, last_limit FROM device_limit_sync_state WHERE subject_id = $1`,
		subjectID).Scan(&panelUserID, &limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, false, nil
	}
	return panelUserID, limit, err == nil, err
}

// SaveDeviceLimitSyncState запоминает выставленный лимит.
func (db *DB) SaveDeviceLimitSyncState(ctx context.Context, subjectID int64, panelUserID string, limit int) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO device_limit_sync_state (subject_id, panel_user_id, last_limit, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (subject_id) DO UPDATE
		   SET panel_user_id = EXCLUDED.panel_user_id,
		       last_limit = EXCLUDED.last_limit, updated_at = now()`,
		subjectID, panelUserID, limit)
	return err
}

// SubjectDeviceLimit собирает итоговый лимит из базы и докупленных пакетов.
func (db *DB) SubjectDeviceLimit(ctx context.Context, subjectID int64, base int) (int, error) {
	var soldBase *int
	if err := db.Pool.QueryRow(ctx, `SELECT max(COALESCE((plan_profile->>'BaseDevices')::int,$2)) FROM orders WHERE (account_id=$1 OR telegram_id=(SELECT telegram_id FROM accounts WHERE id=$1)) AND kind='subscription' AND provider<>'trial' AND status IN ('paid','provisioned') AND (status='paid' OR expire_at>now())`, subjectID, base).Scan(&soldBase); err != nil {
		return 0, err
	}
	if soldBase != nil {
		base = *soldBase
	}

	extra, err := db.ExtraDeviceSlots(ctx, subjectID)
	if err != nil {
		return base, err
	}
	return base + extra, nil
}
