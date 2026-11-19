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

func TestAdminClarityBothBrands(t *testing.T) {
	for _, brand := range []string{"vivid", "berloga"} {
		dir := ""
		if brand == "berloga" {
			dir = filepath.Join("..", "..", "web", "themes", "berloga")
		}
		r, err := newRenderer(&config.Settings{WebDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		partnerID := int64(4)
		render := func(name, page string, data any) string {
			t.Helper()
			rec := httptest.NewRecorder()
			if err := r.render(rec, 200, page, pageData{Site: r.theme, Staff: adminSession{Owner: true}, Now: time.Now(), Data: data, Path: "/admin"}); err != nil {
				t.Fatal(err)
			}
			body := rec.Body.String()
			if out := os.Getenv("ADMIN_CLARITY_EXPORT"); out != "" {
				if err := os.MkdirAll(out, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, brand+"-"+name+".html"), rec.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			return body
		}
		body := render("promos", "admin_promos.html", adminPromos{Codes: []domain.PromoCode{{Code: "SUMMER20", DiscountPercent: 20, IsActive: true, PartnerID: &partnerID, RewardType: "percent", RewardValue: 1250, RewardScope: "first"}, {Code: "GIFT30", GrantDays: 30, MaxUses: 10, UsedCount: 3, IsActive: true}}, Partners: []domain.PartnerSummary{{Partner: domain.Partner{ID: 4, Name: "Алексей"}}}})
		for _, want := range []string{`value="4" selected`, `value="percent" selected`, `value="12.50"`, `value="first" selected`, "Доступ на 30", "Ограничить количество применений", `name="per_user_limit"`, "/admin/promos/SUMMER20/partner"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %s", brand, want)
			}
		}
		queue := storage.StaffQueue{Scope: "free", Pages: 1, Total: 1, Counts: map[string]int{"free": 1, "open": 1, "closed": 3}, Tickets: []storage.StaffTicket{{Ticket: storage.Ticket{ID: 42, Subject: "Не подключается телефон", Status: "open", WorkState: "new", UpdatedAt: time.Now()}, Preview: "Подскажите, как настроить приложение"}}}
		body = render("queue", "admin_tickets.html", adminTickets{Queue: queue})
		for _, want := range []string{"Новое", "Открыть переписку", `aria-current="page"`, "Не назначено"} {
			if !strings.Contains(body, want) {
				t.Fatal(brand, want)
			}
		}
		if strings.Contains(body, "page=-1") || strings.Contains(body, ">Следующая</a>") || strings.Contains(body, " · new · ") {
			t.Fatal("invalid pagination or raw state", brand)
		}
		queue.Tickets = nil
		queue.Total = 0
		body = render("empty", "admin_tickets.html", adminTickets{Queue: queue})
		if !strings.Contains(body, "Нет новых обращений без сотрудника") || !strings.Contains(body, "Все открытые обращения") {
			t.Fatal("empty state lacks a next step", brand)
		}
		queue.Scope = "closed"
		queue.Pages = 3
		queue.Page = 1
		body = render("pages", "admin_tickets.html", adminTickets{Queue: queue})
		for _, want := range []string{"scope=closed&page=0", "scope=closed&page=2", "Страница 2 из 3"} {
			if !strings.Contains(body, want) {
				t.Fatal(brand, want)
			}
		}
	}
}
