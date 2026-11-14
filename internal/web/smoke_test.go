package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPublicPagesRender — дымовой тест: витрина, вход и кабинет должны
// отдаваться без ошибок.
func TestPublicPagesRender(t *testing.T) {
	server, _ := testServer(t)
	handler := server.Handler()

	cases := []struct {
		path     string
		wantCode int
		contains string
	}{
		{"/", http.StatusOK, "Тарифы"},
		{"/healthz", http.StatusOK, "ok"},
		{"/login", http.StatusOK, "Войти"},
		{"/terms", http.StatusOK, "Соглашение"},
		{"/privacy", http.StatusOK, "Конфиденциальность"},
		{"/support", http.StatusOK, "Поддержка"},
		{"/account", http.StatusSeeOther, "/login"},
		{"/nonexistent-page", http.StatusNotFound, "не найдена"},
	}
	for _, item := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, item.path, nil))
		if rec.Code != item.wantCode {
			t.Errorf("%s: код = %d, ждали %d", item.path, rec.Code, item.wantCode)
			continue
		}
		if item.contains != "" && !strings.Contains(rec.Body.String(), item.contains) {
			t.Errorf("%s: в ответе нет %q", item.path, item.contains)
		}
	}
}

// TestStaticFilesAreServed — стили и скрипты отдаются.
func TestStaticFilesAreServed(t *testing.T) {
	server, _ := testServer(t)

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/style.css", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("style.css: код = %d, ждали 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "body") {
		t.Error("стили пустые")
	}

	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/ticket-live.js", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("ticket-live.js: код = %d, ждали 200", rec.Code)
	}
}
