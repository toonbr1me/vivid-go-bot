package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func TestThemeStarterRendersLandingAppAndCustomPage(t *testing.T) {
	r, err := newRenderer(&config.Settings{WebDir: filepath.Join("..", "..", "web", "themes", "starter")})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name string
		data any
		want string
	}{
		{"landing.html", landingData{}, "Начните со своего продукта"},
		{"account.html", accountData{}, `class="app-layout"`},
		{"about.html", nil, "Расскажите о своём проекте"},
	} {
		rec := httptest.NewRecorder()
		if err := r.render(rec, 200, item.name, pageData{Site: r.theme, Subject: &domain.Subject{}, Data: item.data}); err != nil {
			t.Fatalf("%s: %v", item.name, err)
		}
		if !strings.Contains(rec.Body.String(), item.want) {
			t.Fatalf("%s: нет %q", item.name, item.want)
		}
	}
}

func TestThemeCustomPageRequiresLogin(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(`{"pages":{"private":{"path":"/private","template":"private.html","require_login":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "private.html"), []byte(`{{define "content"}}private-content{{end}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(nil, nil, &config.Settings{WebDir: dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/private", nil))
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatalf("нет защиты входом: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "private-content") {
		t.Fatal("раскрыт приватный контент")
	}
}

func TestThemeRejectsTemplateTraversalAndReservedRoutes(t *testing.T) {
	for _, body := range []string{
		`{"pages":{"landing":{"template":"../secret.html"}}}`,
		`{"pages":{"custom":{"path":"/admin/new","template":"notice.html"}}}`,
		`{"blocks":[{"template":"missing/component"}]}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := newRenderer(&config.Settings{WebDir: dir}); err == nil {
			t.Fatalf("принята неверная тема: %s", body)
		}
	}
}
