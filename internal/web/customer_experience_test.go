package web

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
)

func TestCustomerExperienceTemplates(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(18 * 24 * time.Hour)
	id := int64(7)
	subject := domain.Subject{ID: 1, TelegramID: &id}
	limit := int64(100 << 30)
	access := domain.Access{Found: true, Active: true, URL: "https://example.invalid/demo-subscription", ExpireAt: &expiry, DeviceLimit: 3, TrafficLimit: &limit, TrafficUsedBytes: 1 << 30, Devices: []domain.AccessDevice{{HWID: "demo-one", Model: "iPhone", Platform: "iOS", CreatedAt: now}, {HWID: "demo-two", Model: "MacBook", Platform: "macOS", CreatedAt: now}}}
	g := domain.GuideCustomer(access, nil, nil, false, now)
	base := accountData{Access: access, DaysLeft: 18, DeviceLimit: 3, Guidance: &g, GuidanceURL: "/connect"}
	platform, _ := domain.ConnectionPlatformFor("ios")
	for _, theme := range []string{"", filepath.Join("..", "..", "web", "themes", "berloga"), filepath.Join("..", "..", "web", "themes", "vyzhivalovo")} {
		r, err := newRenderer(&config.Settings{BrandName: "Vivid", WebDir: theme})
		if err != nil {
			t.Fatal(theme, err)
		}
		for _, section := range []string{"subscription", "devices", "profile"} {
			data := base
			data.Section = section
			rec := httptest.NewRecorder()
			if err = r.render(rec, 200, "customer_account.html", pageData{Site: r.theme, Subject: &subject, Data: data, CSRF: "preview-token", Now: now, Path: "/account"}); err != nil {
				t.Fatal(err)
			}
			body := rec.Body.String()
			if strings.Contains(body, "site-footer") || !strings.Contains(body, "customer-nav") || strings.Contains(body, "ZgotmplZ") {
				t.Fatal("broken app shell", theme, section)
			}
			if section == "subscription" && (strings.Contains(body, "Пригласить друга") || strings.Contains(body, "name=\"hwid\"")) {
				t.Fatal("home exposes secondary management")
			}
			exportExperiencePreview(t, r, section, rec.Body.Bytes())
		}
		for _, step := range []string{"install", "import", "finish"} {
			rec := httptest.NewRecorder()
			data := connectData{Access: &access, LoggedIn: true, Platform: platform, Platforms: domain.ConnectionPlatforms(), Step: step, ImportURL: connectionImportURL("happ", access.URL)}
			if err = r.render(rec, 200, "customer_connect.html", pageData{Site: r.theme, Subject: &subject, Data: data, CSRF: "preview-token", Now: now, Path: "/connect"}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rec.Body.String(), "ZgotmplZ") || !strings.Contains(rec.Body.String(), "Happ") {
				t.Fatal("broken onboarding")
			}
			exportExperiencePreview(t, r, "connect-"+step, rec.Body.Bytes())
		}
	}
}

func exportExperiencePreview(t *testing.T, r *renderer, name string, body []byte) {
	t.Helper()
	if os.Getenv("VIVID_EXPORT_PREVIEW") != "true" {
		return
	}
	if r.webDir != "" {
		return
	}
	dir := filepath.Join("..", "..", ".codex-attachments", "customer-experience")
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".html"), body, 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"style.css", "customer.css", "customer.js", "miniapp.js", "favicon.svg"} {
		b, err := assets.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "static", name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCustomerStatusOwnershipAndConfirmedState(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	_, err := db.CreateOrder(ctx, domain.Order{OrderID: "owned-pending", AccountID: &owner.ID, TelegramID: owner.TelegramID, Provider: "missing", Kind: domain.KindSubscription, Status: domain.OrderPending, Amount: 10000, Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		s    *domain.Subject
		code int
	}{{nil, 401}, {&other, 404}, {&owner, 200}} {
		req := httptest.NewRequest("GET", "/account/orders/owned-pending/status?success=true", nil)
		if tc.s != nil {
			req.AddCookie(sessionCookieFor(t, db, tc.s.ID))
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if tc.code == 200 {
			var state struct {
				Status string
				Ready  bool
			}
			if json.Unmarshal(rec.Body.Bytes(), &state) != nil || state.Status != domain.OrderPending || state.Ready {
				t.Fatal("redirect asserted payment", rec.Body.String())
			}
		}
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE orders SET status='provisioned',provisioned_at=now() WHERE order_id='owned-pending'`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/account/orders/owned-pending/status", nil)
	req.AddCookie(sessionCookieFor(t, db, owner.ID))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Fatal("confirmed completion missing", rec.Body.String())
	}
}

func TestCustomerJourneyCannotAssertConnection(t *testing.T) {
	s, db := testServer(t)
	subject, _ := db.EnsureTelegramSubject(context.Background(), 7)
	token, cookies := csrfFrom(t, s.Handler())
	cookies = append(cookies, sessionCookieFor(t, db, subject.ID))
	for _, tc := range []struct {
		step string
		code int
	}{{"first_connected", 400}, {"import_completed", 400}, {"import_requested", 204}, {"import_requested", 204}} {
		rec := postForm(t, s.Handler(), "/account/journey", url.Values{"csrf": {token}, "step": {tc.step}, "platform": {"ios"}}, cookies)
		if rec.Code != tc.code {
			t.Fatal(tc.step, rec.Code, rec.Body.String())
		}
	}
	h, err := db.CustomerExperience(context.Background(), 30)
	if err != nil || h.Import != 1 || h.Connected != 0 {
		t.Fatal(h, err)
	}
	if rec := postForm(t, s.Handler(), "/account/journey", url.Values{"step": {"setup_started"}}, cookies); rec.Code != 403 {
		t.Fatal("csrf bypass", rec.Code)
	}
	for _, path := range []string{"/account/devices", "/account/profile"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(cookies[len(cookies)-1])
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(path, rec.Code)
		}
	}
}

func TestCustomerSupportCarriesTopicWithoutSubscriptionSecret(t *testing.T) {
	s, db := testServer(t)
	subject, _ := db.EnsureTelegramSubject(context.Background(), 7)
	token, cookies := csrfFrom(t, s.Handler())
	cookies = append(cookies, sessionCookieFor(t, db, subject.ID))
	rec := postForm(t, s.Handler(), "/support", url.Values{"csrf": {token}, "topic": {"connection"}, "device": {"iPhone / iOS"}, "app": {"Happ"}, "message": {"После добавления нет подключения"}}, cookies)
	if rec.Code != 303 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	tickets, err := db.TicketsForSubject(context.Background(), subject.ID, 10)
	if err != nil || len(tickets) != 1 {
		t.Fatal(err, tickets)
	}
	for _, want := range []string{"Приложение не подключается", "iPhone / iOS", "Happ", "не проверялось"} {
		if !strings.Contains(tickets[0].Context, want) {
			t.Fatal("missing context", want, tickets[0].Context)
		}
	}
}

func TestCustomerQRUsesOnlyAuthenticatedSubscription(t *testing.T) {
	s, db := testServer(t)
	owner, _ := db.EnsureTelegramSubject(context.Background(), 7)
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/users/stream" {
			if r.URL.Query().Get("telegramId") != "7" {
				t.Error("wrong panel identity", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"response":{"users":[{"id":1,"username":"own","telegramId":7,"status":"ACTIVE","expireAt":"2030-01-01T00:00:00Z","subscriptionUrl":"https://example.invalid/private-own-link","hwidDeviceLimit":3}]}}`))
		} else {
			_, _ = w.Write([]byte(`{"response":{"devices":[]}}`))
		}
	}))
	defer panel.Close()
	s.pay = payments.New(db, remnawave.New(panel.URL, "test-only", time.Second), s.pay.Registry(), s.cfg)
	for _, logged := range []bool{false, true} {
		req := httptest.NewRequest("GET", "/connect/qr?u=https://untrusted.invalid/foreign", nil)
		if logged {
			req.AddCookie(sessionCookieFor(t, db, owner.ID))
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if !logged {
			if rec.Code != 401 {
				t.Fatal("anonymous QR exposed", rec.Code)
			}
			continue
		}
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatal(rec.Code, rec.Header(), rec.Body.String())
		}
		if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
			t.Fatal("invalid QR image", err)
		}
	}
}

func TestCustomerNotificationsUpdateOnlySignedInAccount(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	token, cookies := csrfFrom(t, s.Handler())
	cookies = append(cookies, sessionCookieFor(t, db, owner.ID))
	before, err := db.NotificationSettingsFor(ctx, *other.TelegramID)
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, s.Handler(), "/account/notifications", url.Values{"csrf": {token}, "telegram_id": {"8"}, "expiry": {"1"}}, cookies)
	if rec.Code != 303 || rec.Header().Get("Location") != "/account/profile" {
		t.Fatal(rec.Code, rec.Header())
	}
	got, _ := db.NotificationSettingsFor(ctx, *owner.TelegramID)
	after, _ := db.NotificationSettingsFor(ctx, *other.TelegramID)
	if !got.Expiry || got.Activity || before != after {
		t.Fatal("settings crossed accounts", got, before, after)
	}
}
