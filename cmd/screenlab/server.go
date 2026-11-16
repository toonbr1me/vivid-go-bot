package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/web"
)

// This command is deliberately separate from cmd/vivid. It accepts only the
// named loopback lab database, never the application's DATABASE_URL or users.
func serveLab(port int, themeDir string) error {
	databaseURL := os.Getenv("VIVID_LAB_DATABASE_URL")
	target, err := url.Parse(databaseURL)
	if err != nil || target == nil || target.Path != "/vivid_ui_lab" || target.Port() != "55439" || (target.Hostname() != "localhost" && target.Hostname() != "127.0.0.1") {
		return fmt.Errorf("нужна VIVID_LAB_DATABASE_URL с локальной БД vivid_ui_lab на порту 55439")
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	id := int64(7)
	subject, err := db.EnsureTelegramSubject(ctx, id)
	if err != nil {
		return err
	}
	cfg := &config.Settings{Currency: "RUB", SiteEnabled: true, AdminIDs: map[int64]bool{id: true}, WebDir: themeDir, MinPaymentAmount: domain.FromRubles(1), DeviceLimitEnabled: true, DeviceLimitBase: 5, TrialWebDays: 1}
	cfg.DatabaseURL = databaseURL
	for i, days := range []int{30, 90, 365} {
		prices := []int64{299, 799, 2499}
		if err = db.SavePlan(ctx, domain.Plan{ID: fmt.Sprintf("demo-%d", days), Title: fmt.Sprintf("%d дней", days), Days: days, Price: domain.FromRubles(prices[i]), Devices: 3, SortOrder: i, IsActive: true}); err != nil {
			return err
		}
	}
	for i, status := range []string{domain.OrderPending, domain.OrderPaid, domain.OrderProvisioned} {
		orderID := fmt.Sprintf("DEMO-104%d", i)
		if _, e := db.OrderByID(ctx, orderID); e == nil {
			continue
		}
		days := 90
		if _, err = db.CreateOrder(ctx, domain.Order{OrderID: orderID, Provider: "demo", TelegramID: &id, AccountID: &subject.ID, PlanID: "demo-90", Kind: domain.KindSubscription, Amount: domain.FromRubles(799), OriginalAmount: domain.FromRubles(799), Currency: "RUB", Status: status, PlanDays: &days}); err != nil {
			return err
		}
	}
	if ticket, _ := db.OpenTicketForSubject(ctx, subject.ID); ticket == nil {
		if _, err = db.CreateTicket(ctx, subject.ID, &id, "Помощь с подключением", "Демонстрационное обращение: как импортировать ссылку в Happ?", storage.SupportModeHuman); err != nil {
			return err
		}
	}
	registry, err := payments.BuildRegistry(cfg, nil)
	if err != nil {
		return err
	}
	service := payments.New(db, nil, registry, cfg)
	app, err := web.New(db, service, cfg, nil, nil)
	if err != nil {
		return err
	}
	app.StartLive(ctx)
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(token))
	if err = db.CreateWebSession(ctx, hex.EncodeToString(hash[:]), subject.ID, 24*time.Hour); err != nil {
		return err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Session injection exists only in this isolated review command.
		clone := r.Clone(r.Context())
		cookies := r.Cookies()
		clone.Header.Del("Cookie")
		for _, c := range cookies {
			if c.Name != "vivid_session" {
				clone.AddCookie(c)
			}
		}
		clone.AddCookie(&http.Cookie{Name: "vivid_session", Value: token})
		app.Handler().ServeHTTP(w, clone)
	})
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("Демо-админка: http://127.0.0.1:%d/admin\n", port)
	return server.ListenAndServe()
}
