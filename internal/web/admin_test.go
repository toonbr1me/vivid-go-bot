package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

// testServer собирает сайт поверх тестовой базы.
func testServer(t *testing.T) (*Server, *storage.DB) {
	t.Helper()
	db := testdb.Open(t)
	cfg := &config.Settings{
		Currency:         "RUB",
		MinPaymentAmount: domain.FromRubles(1),
		BrandName:        "Vivid",
		WebDir:           t.TempDir(),
		AdminIDs:         map[int64]bool{100: true},
	}
	registry, err := payments.BuildRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("платёжные шлюзы: %v", err)
	}
	service := payments.New(db, nil, registry, cfg)
	server, err := New(db, service, cfg, nil, nil)
	if err != nil {
		t.Fatalf("сайт не собрался: %v", err)
	}
	return server, db
}

// sessionCookieFor — вход без браузера: сессия в базе и cookie в запросе.
func sessionCookieFor(t *testing.T, db *storage.DB, subjectID int64) *http.Cookie {
	t.Helper()
	ctx := context.Background()
	token := "test-token-" + time.Now().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(token))
	if err := db.CreateWebSession(ctx, hex.EncodeToString(sum[:]), subjectID, time.Hour); err != nil {
		t.Fatalf("сессия: %v", err)
	}
	return &http.Cookie{Name: sessionCookie, Value: token}
}

func TestAdminIsClosedToOrdinaryUsers(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 555)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.AddCookie(sessionCookieFor(t, db, subject.ID))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request)

	if rec.Code != http.StatusForbidden {
		t.Errorf("код = %d, ждали 403: обычному человеку админка закрыта", rec.Code)
	}
}

func TestAdminIsOpenToOwner(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 100)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.AddCookie(sessionCookieFor(t, db, subject.ID))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request)

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ждали 200: владелец должен попасть в админку", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Админка") {
		t.Errorf("в ответе нет админки: %s", rec.Body.String()[:min(len(rec.Body.String()), 300)])
	}
}

func TestAdminIsOpenToStaffMember(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 200)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.AddStaff(ctx, 200, storage.RoleSupport, "Поддержка", 100); err != nil {
		t.Fatalf("сотрудник: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/tickets", nil)
	request.AddCookie(sessionCookieFor(t, db, subject.ID))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request)

	if rec.Code != http.StatusOK {
		t.Errorf("код = %d, ждали 200: сотрудник должен видеть очередь", rec.Code)
	}
}

func TestAdminRequiresLogin(t *testing.T) {
	server, _ := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("код = %d, ждали 303: без входа админка отправляет на /login", rec.Code)
	}
	if location := rec.Header().Get("Location"); !strings.HasPrefix(location, "/login") {
		t.Errorf("редирект = %q, ждали /login", location)
	}
}

func TestAdminFormsRequireCSRF(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 100)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	form := url.Values{"name": {"Партнёр"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/partners", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(sessionCookieFor(t, db, subject.ID))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request)

	if rec.Code != http.StatusForbidden {
		t.Errorf("код = %d, ждали 403: форма без CSRF-токена не принимается", rec.Code)
	}
}

func TestReferralModeFormSavesChoice(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 777)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	// Токен формы лежит в cookie: так его выдаёт страница.
	csrf := "csrf-token"
	request := httptest.NewRequest(http.MethodPost, "/account/referral-mode",
		strings.NewReader(url.Values{"mode": {"balance"}, "csrf": {csrf}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(sessionCookieFor(t, db, subject.ID))
	request.AddCookie(&http.Cookie{Name: csrfCookie, Value: csrf})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("код = %d, ждали 303", rec.Code)
	}
	mode, err := db.ReferralMode(ctx, subject.ID)
	if err != nil {
		t.Fatalf("чтение режима: %v", err)
	}
	if mode != "balance" {
		t.Errorf("режим = %q, ждали balance", mode)
	}
}

func TestPartnerClaimBindsAccount(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()

	partner, err := db.CreatePartner(ctx, "Партнёр", "", "", "")
	if err != nil {
		t.Fatalf("партнёр: %v", err)
	}
	code, err := db.CreatePartnerInviteCode(ctx, partner.ID, time.Hour)
	if err != nil {
		t.Fatalf("приглашение: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, 888)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/partner/claim?token="+code, nil)
	sessionCookie := sessionCookieFor(t, db, subject.ID)
	request.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Подтвердить привязку") {
		t.Fatalf("confirmation status = %d: %s", rec.Code, rec.Body.String())
	}
	if linked, err := db.GetPartnerBySubject(ctx, subject.ID); err != nil || linked != nil {
		t.Fatal("GET linked partner", linked, err)
	}
	token, cookies := csrfFrom(t, server.Handler())
	cookies = append(cookies, sessionCookie)
	rec = postForm(t, server.Handler(), "/partner/claim", url.Values{"csrf": {token}, "token": {code}}, cookies)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST status=%d: %s", rec.Code, rec.Body.String())
	}
	linked, err := db.GetPartnerBySubject(ctx, subject.ID)
	if err != nil {
		t.Fatalf("чтение партнёра: %v", err)
	}
	if linked == nil || linked.ID != partner.ID {
		t.Errorf("аккаунт не привязан: %+v", linked)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
