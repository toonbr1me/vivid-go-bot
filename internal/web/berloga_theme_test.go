package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

func TestBerlogaThemeRendersRealPageContracts(t *testing.T) {
	dir := filepath.Join("..", "..", "web", "themes", "berloga")
	r, err := newRenderer(&config.Settings{WebDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	expiry := now.Add(30 * 24 * time.Hour)
	tg := int64(7)
	name := "Тестовый клиент"
	subject := domain.Subject{ID: 1, TelegramID: &tg, Name: &name}
	limit := int64(10 * 1024 * 1024 * 1024)
	plan := domain.Plan{ID: "30d", Title: "1 месяц", Days: 30, Price: 10000}
	ticket := storage.Ticket{ID: 42, Subject: "Не работает подключение", Status: "open", CreatedAt: now}
	for _, tt := range []struct {
		name, page, want string
		data             any
	}{
		{"landing", "landing.html", "Интернет с медвежьей силой", landingData{Plans: []planCard{{Plan: plan, Devices: 5}}, TrialDays: 1}},
		{"checkout", "buy.html", `name="plan"`, buyData{Quotes: []buyQuote{{Quote: domain.PlanQuote{Plan: plan, Price: plan.Price, Amount: plan.Price}}}, Selected: "30d"}},
		{"login", "login.html", `action="/auth/code"`, loginData{Next: "/admin"}},
		{"active account", "account.html", `data-copy-target="sub-url"`, accountData{Access: domain.Access{Found: true, Active: true, URL: "https://test2.example/zhiv_id/sample", ExpireAt: &expiry, TrafficLimit: &limit, TrafficUsedBytes: 1024}, DeviceLimit: 5, HasTelegram: true, DeviceEnabled: true}},
		{"empty account", "account.html", "пока свободно", accountData{}},
		{"guest support", "support.html", `name="contact"`, supportData{}},
		{"client support", "support.html", `name="device"`, supportData{LoggedIn: true, Tickets: []storage.Ticket{ticket}}},
		{"ticket", "ticket.html", `id="ticket-compose"`, ticketData{Ticket: ticket, Open: true, Messages: []storage.Message{{ID: 1, Text: "Помогите подключиться", CreatedAt: now}}}},
		{"queue", "admin_tickets.html", `href="/support/tickets/42"`, adminTickets{Tickets: []storage.Ticket{ticket}}},
		{"admin", "admin.html", "Рабочий стол", adminDashboard{}},
		{"connect", "connect.html", "Установите приложение", connectData{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := r.render(rec, 200, tt.page, pageData{Site: r.theme, Subject: &subject, Now: now, CSRF: "test-csrf", BotURL: "https://t.me/berloga1testbot", Data: tt.data}); err != nil {
				t.Fatal(err)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.want) || !strings.Contains(body, "/static/berloga/python.css") {
				t.Fatal("missing page content or original stylesheet")
			}
			if strings.Contains(body, "/static/style.css") || strings.Contains(body, "/static/admin.css") {
				t.Fatal("default UI styles mixed into Python theme")
			}
			if out := os.Getenv("BERLOGA_REVIEW_EXPORT"); out != "" {
				if err := os.MkdirAll(out, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, strings.ReplaceAll(tt.name, " ", "-")+".html"), rec.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
