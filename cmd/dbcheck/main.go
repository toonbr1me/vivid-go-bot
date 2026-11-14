// Command dbcheck открывает базу через storage.Open: так проверяется, что
// миграции накатываются на существующую базу и что перенос субъектов проходит.
//
//	go run ./cmd/dbcheck -url postgres://postgres@localhost:5433/vivid
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

func main() {
	url := flag.String("url", os.Getenv("DATABASE_URL"), "строка подключения")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "нужен -url или DATABASE_URL")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	start := time.Now()
	db, err := storage.Open(ctx, *url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "открытие не удалось:", err)
		os.Exit(1)
	}
	defer db.Close()
	fmt.Printf("миграции и перенос субъектов прошли за %v\n", time.Since(start).Truncate(time.Millisecond))

	checks := []struct {
		name string
		sql  string
	}{
		{"аккаунтов", `SELECT count(*) FROM accounts`},
		{"аккаунтов с telegram_id", `SELECT count(*) FROM accounts WHERE telegram_id IS NOT NULL`},
		{"заказов", `SELECT count(*) FROM orders`},
		{"заказов без account_id", `SELECT count(*) FROM orders WHERE account_id IS NULL`},
		{"триалов", `SELECT count(*) FROM trials`},
		{"триалов без subject_id", `SELECT count(*) FROM trials WHERE subject_id IS NULL`},
		{"балансов", `SELECT count(*) FROM user_balance`},
		{"балансов без subject_id", `SELECT count(*) FROM user_balance WHERE subject_id IS NULL`},
		{"активаций промокодов", `SELECT count(*) FROM promo_activations`},
		{"активаций без subject_id", `SELECT count(*) FROM promo_activations WHERE subject_id IS NULL`},
		{"рефералов", `SELECT count(*) FROM referrals`},
		{"рефералов без subject_id", `SELECT count(*) FROM referrals WHERE referred_subject_id IS NULL`},
		{"тикетов", `SELECT count(*) FROM support_tickets`},
		{"тикетов без subject_id", `SELECT count(*) FROM support_tickets WHERE subject_id IS NULL`},
		{"аккаунтов без ref_code", `SELECT count(*) FROM accounts WHERE ref_code IS NULL`},
	}
	for _, item := range checks {
		var value int
		if err := db.Pool.QueryRow(ctx, item.sql).Scan(&value); err != nil {
			fmt.Printf("  %-32s ошибка: %v\n", item.name, err)
			continue
		}
		fmt.Printf("  %-32s %d\n", item.name, value)
	}
}
