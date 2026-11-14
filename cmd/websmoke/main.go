// Command websmoke поднимает сайт против указанной базы и обходит основные
// страницы. Так проверяется, что приложение поднимается на существующих
// данных: миграции, темы, шаблоны и запросы.
//
//	go run ./cmd/websmoke -url postgres://postgres@localhost:5433/vivid
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/web"
)

func main() {
	url := flag.String("url", os.Getenv("DATABASE_URL"), "строка подключения")
	port := flag.Int("port", 8099, "порт для проверки")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "нужен -url или DATABASE_URL")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	db, err := storage.Open(ctx, *url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "база не открылась:", err)
		os.Exit(1)
	}
	defer db.Close()

	cfg := &config.Settings{
		Currency:         "RUB",
		MinPaymentAmount: 1000,
		BrandName:        "Vivid",
		SiteEnabled:      true,
		AdminIDs:         map[int64]bool{},
		WebDir:           "web",
	}
	registry, err := payments.BuildRegistry(cfg, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "платёжные шлюзы:", err)
		os.Exit(1)
	}
	service := payments.New(db, nil, registry, cfg)
	server, err := web.New(db, service, cfg, nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "сайт:", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", *port),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "слушатель:", err)
		}
	}()
	defer httpServer.Close()
	time.Sleep(500 * time.Millisecond)

	client := &http.Client{Timeout: 20 * time.Second}
	pages := []string{
		"/", "/buy", "/login", "/support", "/connect", "/terms", "/privacy",
		"/healthz", "/account", "/admin", "/partner", "/partner/claim?token=нет",
	}
	failed := 0
	for _, page := range pages {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", *port, page))
		if err != nil {
			fmt.Printf("  %-28s ошибка: %v\n", page, err)
			failed++
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		mark := "ок"
		if resp.StatusCode >= 500 {
			mark = "ОШИБКА"
			failed++
		}
		fmt.Printf("  %-28s %d %s (%d байт)\n", page, resp.StatusCode, mark, len(body))
	}

	// Страница со статистикой: она читает реальные данные.
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", *port))
	if err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Println("healthz:", strings.TrimSpace(string(body)))
	}
	if failed > 0 {
		fmt.Printf("проблемных страниц: %d\n", failed)
		os.Exit(1)
	}
	fmt.Println("все страницы ответили")
}
