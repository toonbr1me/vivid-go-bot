package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// PaidPlans читает каталог платных тарифов.
func (db *DB) PaidPlans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT plan_id, title, days, price_rub, devices, sort_order, is_active
		FROM plans ORDER BY sort_order, plan_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Plan
	for rows.Next() {
		var plan domain.Plan
		var priceRub int64
		if err := rows.Scan(
			&plan.ID, &plan.Title, &plan.Days, &priceRub,
			&plan.Devices, &plan.SortOrder, &plan.IsActive,
		); err != nil {
			return nil, err
		}
		plan.Price = domain.FromRubles(priceRub)
		out = append(out, plan)
	}
	return out, rows.Err()
}

// SavePlan создаёт или обновляет тариф в каталоге.
func (db *DB) SavePlan(ctx context.Context, plan domain.Plan) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO plans (plan_id, title, days, price_rub, devices, sort_order, is_active, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (plan_id) DO UPDATE
		   SET title = EXCLUDED.title, days = EXCLUDED.days, price_rub = EXCLUDED.price_rub,
		       devices = EXCLUDED.devices, sort_order = EXCLUDED.sort_order,
		       is_active = EXCLUDED.is_active, updated_at = now()`,
		plan.ID, plan.Title, plan.Days, plan.Price.Rubles(), plan.Devices, plan.SortOrder, plan.IsActive)
	return err
}

// DevicePacks читает каталог пакетов устройств.
func (db *DB) DevicePacks(ctx context.Context) ([]domain.DevicePack, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT pack_id, title, devices, price_rub, days, sort_order, is_active
		FROM device_packs ORDER BY sort_order, pack_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DevicePack
	for rows.Next() {
		var pack domain.DevicePack
		var priceRub int64
		if err := rows.Scan(
			&pack.ID, &pack.Title, &pack.Devices, &priceRub,
			&pack.Days, &pack.SortOrder, &pack.IsActive,
		); err != nil {
			return nil, err
		}
		pack.Price = domain.FromRubles(priceRub)
		out = append(out, pack)
	}
	return out, rows.Err()
}

// SaveDevicePack создаёт или обновляет пакет.
func (db *DB) SaveDevicePack(ctx context.Context, pack domain.DevicePack) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO device_packs (pack_id, title, devices, price_rub, days, sort_order, is_active, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (pack_id) DO UPDATE
		   SET title = EXCLUDED.title, devices = EXCLUDED.devices,
		       price_rub = EXCLUDED.price_rub, days = EXCLUDED.days,
		       sort_order = EXCLUDED.sort_order, is_active = EXCLUDED.is_active,
		       updated_at = now()`,
		pack.ID, pack.Title, pack.Devices, pack.Price.Rubles(), pack.Days, pack.SortOrder, pack.IsActive)
	return err
}

// DevicePack по идентификатору.
func (db *DB) DevicePack(ctx context.Context, packID string) (domain.DevicePack, error) {
	var pack domain.DevicePack
	var priceRub int64
	err := db.Pool.QueryRow(ctx, `
		SELECT pack_id, title, devices, price_rub, days, sort_order, is_active
		FROM device_packs WHERE pack_id = $1`, packID).Scan(
		&pack.ID, &pack.Title, &pack.Devices, &priceRub,
		&pack.Days, &pack.SortOrder, &pack.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return pack, ErrNotFound
	}
	pack.Price = domain.FromRubles(priceRub)
	return pack, err
}
