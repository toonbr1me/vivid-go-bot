// Command dbcheck открывает базу через storage.Open: так проверяется, что
// миграции накатываются на существующую базу и что перенос субъектов проходит.
//
//	go run ./cmd/dbcheck -url postgres://postgres@localhost:5433/vivid
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

func main() {
	url := flag.String("url", os.Getenv("DATABASE_URL"), "строка подключения")
	jsonOutput := flag.Bool("json", false, "машиночитаемый отчёт")
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
	elapsed := time.Since(start).Milliseconds()
	if !*jsonOutput {
		fmt.Printf("миграции и перенос субъектов прошли за %vms\n", elapsed)
	}
	counts := map[string]int{}
	issues := []string{}

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
			issues = append(issues, fmt.Sprintf("%s: %v", item.name, err))
			continue
		}
		counts[item.name] = value
		if !*jsonOutput {
			fmt.Printf("  %-32s %d\n", item.name, value)
		}
	}
	for _, item := range []struct{ name, sql string }{
		{"orders_without_owner", `SELECT count(*) FROM orders WHERE account_id IS NULL`},
		{"trials_without_owner", `SELECT count(*) FROM trials WHERE subject_id IS NULL`},
		{"balances_without_owner", `SELECT count(*) FROM user_balance WHERE subject_id IS NULL`},
		{"unmapped_order_status", `SELECT count(*) FROM orders WHERE status NOT IN ('pending','paid','provisioned','failed','cancelled','refunded')`},
		{"issued_order_status", `SELECT count(*) FROM orders WHERE status='paid' AND provisioned_at IS NOT NULL`},
		{"negative_order_amount", `SELECT count(*) FROM orders WHERE amount<0`},
	} {
		var value int
		if err := db.Pool.QueryRow(ctx, item.sql).Scan(&value); err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", item.name, err))
		} else if value != 0 {
			issues = append(issues, fmt.Sprintf("%s: %d", item.name, value))
		}
	}
	if *jsonOutput {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"migration_ms": elapsed, "counts": counts, "issues": issues, "ok": len(issues) == 0})
	} else {
		for _, issue := range issues {
			fmt.Fprintln(os.Stderr, issue)
		}
	}
	if len(issues) > 0 {
		os.Exit(1)
	}
}
