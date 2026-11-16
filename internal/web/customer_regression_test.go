package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
)

func TestActivePartnerAndFallbackAccountRender(t *testing.T) {
	renderer, err := newRenderer(&config.Settings{WebDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().AddDate(0, 0, 30)
	for _, tc := range []struct {
		page string
		data any
		want string
	}{
		{"partner.html", partnerData{Partner: &domain.Partner{ID: 7, Name: "Partner"}, Rewarded: 555, Cancelled: 111, Rewards: []domain.PartnerReward{{Status: "cancelled", Amount: 111}}}, "отменено"},
		{"account.html", accountData{Access: domain.Access{Active: true, ExpireAt: &expiry}}, "Последние известные данные"},
	} {
		rec := httptest.NewRecorder()
		if err := renderer.render(rec, 200, tc.page, pageData{Site: renderer.theme, Subject: &domain.Subject{ID: 1}, Data: tc.data}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatal("missing state", tc.want)
		}
	}
}

func TestWebCheckoutRequiresExplicitRecurringConsent(t *testing.T) {
	for _, tc := range []struct {
		name, option, consent string
		wantCalls             int32
		wantConsent           bool
	}{
		{"once", "yookassa.card", "", 1, false},
		{"recurring", "yookassa.card", "1", 1, true},
		{"unsupported", "yookassa.sbp", "1", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, db := testServer(t)
			ctx := context.Background()
			if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true}); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			var saved atomic.Bool
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				saved.Store(body["save_payment_method"] == true)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"payment-1","confirmation":{"confirmation_url":"https://pay.example/1"}}`))
			}))
			defer gateway.Close()
			server.cfg.PaymentProviders = []string{"yookassa"}
			server.cfg.PaymentMethods = []string{"card", "sbp"}
			server.cfg.YooKassaBaseURL = gateway.URL
			server.cfg.YooKassaShopID = "shop"
			server.cfg.YooKassaSecret = "secret"
			server.cfg.RecurringEnabled = true
			server.cfg.RecurringProviders = []string{"yookassa"}
			server.cfg.LinkTTLMinutes = 30
			registry, err := payments.BuildRegistry(server.cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			server.pay = payments.New(db, nil, registry, server.cfg)
			s, err := db.UpsertGoogleSubject(ctx, "buyer", "buyer@example.com", "Buyer")
			if err != nil {
				t.Fatal(err)
			}
			token, cookies := csrfFrom(t, server.Handler())
			cookies = append(cookies, sessionCookieFor(t, db, s.ID))
			request := httptest.NewRequest(http.MethodGet, "/buy", nil)
			for _, c := range cookies {
				request.AddCookie(c)
			}
			page := httptest.NewRecorder()
			server.Handler().ServeHTTP(page, request)
			if page.Code != 200 || !strings.Contains(page.Body.String(), `id="checkout-recurring"`) {
				t.Fatal("recurring choice absent", page.Code, page.Body.String())
			}
			if strings.Contains(page.Body.String(), `name="recurring" value="1" checked`) {
				t.Fatal("preselected consent")
			}
			rec := postForm(t, server.Handler(), "/buy", url.Values{"csrf": {token}, "plan": {"30d"}, "option": {tc.option}, "recurring": {tc.consent}}, cookies)
			if rec.Code != 303 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			if calls.Load() != tc.wantCalls || saved.Load() != tc.wantConsent {
				t.Fatalf("gateway calls=%d consent=%v", calls.Load(), saved.Load())
			}
			a, err := db.RecurringForSubject(ctx, s.ID)
			if tc.wantConsent && (err != nil || !a.Enabled || a.Amount <= 0 || a.Days != 30) {
				t.Fatal("agreement missing", a, err)
			}
			if !tc.wantConsent && err == nil {
				t.Fatal("unexpected agreement", a)
			}
		})
	}
}

func TestPartnerInvitationPOSTRequiresCSRF(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()
	p, err := db.CreatePartner(ctx, "Partner", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.CreatePartnerInviteCode(ctx, p.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s, err := db.EnsureTelegramSubject(ctx, 9087)
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, server.Handler(), "/partner/claim", url.Values{"token": {code}}, []*http.Cookie{sessionCookieFor(t, db, s.ID)})
	if rec.Code != http.StatusForbidden {
		t.Fatal("missing CSRF accepted", rec.Code)
	}
	if invite, err := db.PartnerInvite(ctx, code); err != nil || invite == nil {
		t.Fatal("rejected request burned invitation", invite, err)
	}
}
