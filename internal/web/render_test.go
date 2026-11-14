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
)

// Набор страниц — это контракт между renderer и шаблонами. Опечатка в
// имени файла иначе всплыла бы только при первом заходе на страницу.
func TestEmbeddedTemplatesParse(t *testing.T) {
	r, err := newRenderer(&config.Settings{WebDir: t.TempDir()})
	if err != nil {
		t.Fatalf("встроенные шаблоны не собрались: %v", err)
	}
	for _, page := range pageFiles {
		if _, ok := r.pages[page]; !ok {
			t.Errorf("нет собранного шаблона %q", page)
		}
	}
}

func TestThemeOverrideMergesColorsAndTexts(t *testing.T) {
	dir := t.TempDir()
	body := `{"brand": "Своё имя", "colors": {"accent": "#123456"}}`
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := newRenderer(&config.Settings{WebDir: dir})
	if err != nil {
		t.Fatalf("тема не загрузилась: %v", err)
	}
	if r.theme.Brand != "Своё имя" {
		t.Errorf("бренд = %q, ожидалось переопределение", r.theme.Brand)
	}
	if got := r.theme.Colors["accent"]; got != "#123456" {
		t.Errorf("accent = %q, ожидалось значение из theme.json", got)
	}
	// Незаданные цвета обязаны остаться встроенными.
	if got := r.theme.Colors["bg"]; got != defaultTheme().Colors["bg"] {
		t.Errorf("bg = %q, ожидалось встроенное значение", got)
	}
	if css := string(r.theme.CSS); !strings.Contains(css, "--accent:#123456;") {
		t.Errorf("в CSS нет переопределённого accent: %s", css)
	}
}

func TestBrokenThemeFailsStart(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newRenderer(&config.Settings{WebDir: dir}); err == nil {
		t.Fatal("ожидалась ошибка разбора theme.json")
	}
}

func TestRenderLanding(t *testing.T) {
	r, err := newRenderer(&config.Settings{WebDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	data := pageData{
		Site: r.theme,
		Meta: pageMeta{Title: "Витрина", Description: "Проверка вёрстки"},
		CSRF: "csrf",
		Year: 2026,
		Now:  time.Now(),
		Data: landingData{
			Plans: []planCard{{
				Plan:    domain.Plan{ID: "30d", Title: "1 месяц", Days: 30, Price: 100_00},
				Monthly: 100_00,
				Devices: 5,
			}},
			TrialDays:      1,
			TrialAvailable: true,
			StarterPrice:   100_00,
		},
	}
	rec := httptest.NewRecorder()
	if err := r.render(rec, 200, "landing.html", data); err != nil {
		t.Fatalf("страница не отрисовалась: %v", err)
	}
	html := rec.Body.String()
	if !strings.Contains(html, "1 месяц") || !strings.Contains(html, "Витрина") {
		t.Errorf("в выводе нет данных страницы: %s", html)
	}
}

func TestRenderAllPagesWithEmptyData(t *testing.T) {
	r, err := newRenderer(&config.Settings{WebDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	pages := []struct {
		name    string
		data    any
		subject *domain.Subject
	}{
		{"landing.html", landingData{}, nil},
		{"buy.html", buyData{}, nil},
		{"login.html", loginData{}, nil},
		{"account.html", accountData{}, &domain.Subject{ID: 1}},
		{"connect.html", connectData{}, nil},
		{"support.html", supportData{}, nil},
		{"ticket.html", ticketData{}, nil},
		{"documents.html", documentData{}, nil},
		{"pay_result.html", payResultData{}, nil},
		{"notice.html", noticeData{}, nil},
		{"admin.html", adminDashboard{}, &domain.Subject{ID: 1}},
		{"admin_promos.html", adminPromos{}, &domain.Subject{ID: 1}},
		{"admin_partners.html", adminPartners{}, &domain.Subject{ID: 1}},
		{"admin_partner.html", adminPartnerCard{}, &domain.Subject{ID: 1}},
		{"admin_tickets.html", adminTickets{}, &domain.Subject{ID: 1}},
		{"admin_audit.html", adminAudit{}, &domain.Subject{ID: 1}},
		{"partner.html", partnerData{}, &domain.Subject{ID: 1}},
	}
	for _, page := range pages {
		pageData := pageData{
			Site:    r.theme,
			Meta:    pageMeta{Title: page.name},
			Year:    2026,
			Now:     time.Now(),
			Subject: page.subject,
			Data:    page.data,
		}
		rec := httptest.NewRecorder()
		if err := r.render(rec, 200, page.name, pageData); err != nil {
			t.Errorf("%s: %v", page.name, err)
		}
	}
}

func TestMoneyText(t *testing.T) {
	cases := map[int64]string{
		0:       "0\u00a0₽",
		100_00:  "100\u00a0₽",
		1234_56: "1\u2009234,56\u00a0₽",
	}
	for cents, want := range cases {
		if got := moneyText(domain.Money(cents)); got != want {
			t.Errorf("moneyText(%d) = %q, ожидалось %q", cents, got, want)
		}
	}
}
