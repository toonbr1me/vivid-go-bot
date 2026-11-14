package web

import (
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Optional local export uses synthetic fixtures and never opens a database.
func TestCustomerLifecycleTemplatesRender(t *testing.T) {
	renderer, err := newRenderer(&config.Settings{BrandName: "Vivid"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-5 * time.Minute)
	next := now.Add(30 * 24 * time.Hour)
	id := int64(7)
	file := "private-file-id"
	subject := domain.Subject{ID: 1, TelegramID: &id}
	paid := domain.Order{OrderID: "example-order", Status: domain.OrderPaid, Provider: "yookassa", Kind: domain.KindSubscription, PlanID: "30d", Amount: 19900, Currency: "RUB", PaidAt: &old, CreatedAt: old}
	pending := paid
	pending.Status = domain.OrderPending
	a := domain.RecurringAgreement{ID: "example-agreement", Provider: "yookassa", Amount: 19900, Currency: "RUB", Days: 30, Enabled: true, Activated: true, NextChargeAt: &next}
	ticket := storage.Ticket{ID: 42, Subject: "Не удаётся подключиться", Status: "open", CreatedAt: old, Context: "Заказ: example-order · iPhone / iOS · Happ"}
	for _, tt := range []struct {
		name, page, want string
		data             any
	}{
		{"pending", "pay_result.html", "Ожидаем оплату", payResultData{Order: &pending, OrderOwned: true}},
		{"delayed", "pay_result.html", "Выдача задерживается", payResultData{Order: &paid, OrderOwned: true}},
		{"account", "account.html", "Следующее списание", accountData{Recurring: &a, Orders: []domain.Order{paid}, Tickets: []storage.Ticket{ticket}}},
		{"support", "support.html", "Приложение", supportData{LoggedIn: true, Order: paid.OrderID, Tickets: []storage.Ticket{ticket}}},
		{"ticket", "ticket.html", "Проблема решена", ticketData{Ticket: ticket, Open: true, Messages: []storage.Message{{ID: 1, Text: "Ошибка подключения", FileID: &file, CreatedAt: old}, {ID: 2, Text: "Обновите подписку в приложении и повторите подключение.", FromAdmin: true, CreatedAt: now}}}},
		{"admin", "admin.html", "Вся очередь заказов", adminDashboard{Owner: true, AwaitingAccess: 3, AwaitingPayment: 12, RecurringErrors: 1, Orders: []domain.Order{paid}}},
		{"orders", "admin_orders.html", "Повторить выдачу", adminOrdersData{Orders: []domain.Order{paid}, Attention: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := renderer.render(rec, 200, tt.page, pageData{Site: renderer.theme, Meta: pageMeta{Title: "Vivid · " + tt.name}, Subject: &subject, CSRF: "preview-token", Now: now, Year: now.Year(), Data: tt.data})
			if err != nil || rec.Code != 200 || !strings.Contains(rec.Body.String(), tt.want) {
				t.Fatalf("render %s: %v", tt.name, err)
			}
			if strings.Contains(rec.Body.String(), file) {
				t.Fatal("Telegram file identifier exposed")
			}
			if os.Getenv("VIVID_EXPORT_PREVIEW") == "true" {
				dir := filepath.Join("..", "..", ".codex-attachments")
				if err = os.MkdirAll(filepath.Join(dir, "static"), 0755); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(dir, "customer-"+tt.name+".html"), rec.Body.Bytes(), 0644); err != nil {
					t.Fatal(err)
				}
				for _, asset := range []string{"style.css", "admin.css", "admin.js", "ticket-live.js", "favicon.svg"} {
					data, e := assets.ReadFile("static/" + asset)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(filepath.Join(dir, "static", asset), data, 0644); e != nil {
						t.Fatal(e)
					}
				}
			}
		})
	}
}
