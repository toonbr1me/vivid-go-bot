package storage

import (
	"context"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func (db *DB) AppSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := db.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT value FROM app_settings WHERE key=$1),'')`, key).Scan(&value)
	return value, err
}

// AppSettings читает переопределения из админки.
func (db *DB) AppSettings(ctx context.Context) (map[string]string, error) {
	rows, err := db.Pool.Query(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

// SaveAppSetting сохраняет одну настройку.
func (db *DB) SaveAppSetting(ctx context.Context, key, value string, actorID int64) error {
	if value == "" {
		_, err := db.Pool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key)
		return err
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		key, value, actorID)
	return err
}

// DeleteAppSetting убирает переопределение, возвращая значение из .env.
func (db *DB) DeleteAppSetting(ctx context.Context, key string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key)
	return err
}

// StatsSummary — сводка админки.
type StatsSummary struct {
	Users         int
	Subscriptions int
	ActiveToday   int
	PaidOrders    int
	Revenue       domain.Money
	Trials        int
	OpenTickets   int
}

// Stats собирает сводку за период в днях.
func (db *DB) Stats(ctx context.Context, sinceDays int) (StatsSummary, error) {
	var s StatsSummary
	var revenue int64
	err := db.Pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM accounts WHERE telegram_id IS NOT NULL),
			(SELECT count(*) FROM orders WHERE status = 'provisioned' AND kind = 'subscription'),
			(SELECT count(*) FROM orders WHERE status = 'provisioned' AND expire_at > now()),
			(SELECT count(*) FROM orders WHERE status IN ('paid','provisioned') AND amount > 0
			   AND created_at >= now() - make_interval(days => $1)),
			(SELECT COALESCE(round(sum(amount) * 100), 0)::bigint FROM orders
			   WHERE status IN ('paid','provisioned') AND amount > 0
			     AND created_at >= now() - make_interval(days => $1)),
			(SELECT count(*) FROM trials),
			(SELECT count(*) FROM support_tickets WHERE status = 'open')`,
		sinceDays).Scan(
		&s.Users, &s.Subscriptions, &s.ActiveToday, &s.PaidOrders,
		&revenue, &s.Trials, &s.OpenTickets)
	s.Revenue = domain.Money(revenue)
	return s, err
}

// FunnelStep увеличивает счётчик шага воронки за сегодня.
func (db *DB) FunnelStep(ctx context.Context, step string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO funnel_daily (day, step, hits) VALUES (current_date, $1, 1)
		ON CONFLICT (day, step) DO UPDATE SET hits = funnel_daily.hits + 1`, step)
	return err
}

// FunnelRow — счётчик шага.
type FunnelRow struct {
	Step string
	Hits int64
}

// FunnelSteps читает воронку за период.
func (db *DB) FunnelSteps(ctx context.Context, days int) ([]FunnelRow, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT step, sum(hits)::bigint FROM funnel_daily
		WHERE day >= current_date - $1::int
		GROUP BY step ORDER BY sum(hits) DESC`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FunnelRow
	for rows.Next() {
		var row FunnelRow
		if err := rows.Scan(&row.Step, &row.Hits); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// BroadcastRecipients — все получатели рассылки.
func (db *DB) BroadcastRecipients(ctx context.Context) ([]int64, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT telegram_id FROM accounts WHERE telegram_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
