// Package storage — единственное место, где живут SQL-запросы.
//
// Репозитории возвращают доменные типы, а не строки драйвера: обработчики
// бота и сайта не должны знать, как называется колонка. Суммы читаются
// приведёнными к копейкам, поэтому NUMERIC не превращается в float.
package storage

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB — пул соединений и репозитории.
type DB struct {
	Pool *pgxpool.Pool
}

// Open открывает пул и применяет схему.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("строка подключения: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("пул соединений: %w", err)
	}
	db := &DB{Pool: pool}
	if err := db.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := db.MigrateSubjects(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}

// Close закрывает пул.
func (db *DB) Close() { db.Pool.Close() }

// Migrate применяет встроенную схему. DDL идемпотентен.
func (db *DB) Migrate(ctx context.Context) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("чтение миграций: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("миграция %s: %w", entry.Name(), err)
		}
		if _, err := db.Pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("миграция %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// SeedDefaults наполняет каталоги только на пустой базе: обновившаяся
// установка обязана продавать ровно то же, что продавала вчера.
func (db *DB) SeedDefaults(ctx context.Context, plans []PlanSeed, packs []PackSeed) error {
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM plans`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		for _, plan := range plans {
			if _, err := db.Pool.Exec(ctx, `
				INSERT INTO plans (plan_id, title, days, price_rub, devices, sort_order, is_active)
				VALUES ($1, $2, $3, $4, $5, $6, TRUE)
				ON CONFLICT (plan_id) DO NOTHING`,
				plan.ID, plan.Title, plan.Days, plan.PriceRoubles, plan.Devices, plan.SortOrder,
			); err != nil {
				return err
			}
		}
	}
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM device_packs`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		for _, pack := range packs {
			if _, err := db.Pool.Exec(ctx, `
				INSERT INTO device_packs (pack_id, title, devices, price_rub, days, sort_order, is_active)
				VALUES ($1, $2, $3, $4, $5, $6, TRUE)
				ON CONFLICT (pack_id) DO NOTHING`,
				pack.ID, pack.Title, pack.Devices, pack.PriceRoubles, pack.Days, pack.SortOrder,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// PlanSeed — строка стартового каталога.
type PlanSeed struct {
	ID           string
	Title        string
	Days         int
	PriceRoubles int64
	Devices      int
	SortOrder    int
}

// PackSeed — строка стартового каталога пакетов.
type PackSeed struct {
	ID           string
	Title        string
	Devices      int
	PriceRoubles int64
	Days         int
	SortOrder    int
}
