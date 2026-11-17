package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Keep the reserve theme executable as the web renderer evolves. The test
// exercises customer, support, and staff pages without making it the default.
func TestVyzhivalovoReserveThemeRendersKeyPages(t *testing.T) {
	dir := filepath.Join("..", "..", "web", "themes", "vyzhivalovo")
	r, err := newRenderer(&config.Settings{WebDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if r.theme.Brand != "Берлога" {
		t.Fatalf("brand = %q", r.theme.Brand)
	}
	date := time.Now().AddDate(0, 0, 30)
	plan := domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true}
	for _, tc := range []struct {
		page string
		data any
		want string
	}{
		{"landing.html", landingData{LoggedIn: true, StarterPrice: 10000, Plans: []planCard{{Plan: plan, Devices: 3}}}, "Своя Берлога."},
		{"buy.html", buyData{LoggedIn: true, Selected: "30d", RecurringAvailable: true, Options: []domain.PaymentOption{{Provider: "yookassa", Method: "card"}}, Quotes: []buyQuote{{Quote: domain.PlanQuote{Plan: plan, Price: 10000, Amount: 10000}, RecurringOptions: "yookassa.card"}}}, `name="recurring"`},
		{"account.html", accountData{Access: domain.Access{Found: true, Active: true, ExpireAt: &date}}, "Действует до"},
		{"support.html", supportData{LoggedIn: true}, `action="/support"`},
		{"admin.html", adminDashboard{}, "Рабочий стол"},
	} {
		t.Run(tc.page, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := r.render(rec, 200, tc.page, pageData{Site: r.theme, Page: r.theme.Pages[strings.TrimSuffix(tc.page, ".html")], Meta: pageMeta{Title: "Берлога"}, Subject: &domain.Subject{ID: 7}, CSRF: "theme-csrf", Data: tc.data, Now: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("missing %q", tc.want)
			}
			if !strings.Contains(rec.Body.String(), "/static/berloga.css") {
				t.Fatal("reserve theme stylesheet missing")
			}
		})
	}
}
