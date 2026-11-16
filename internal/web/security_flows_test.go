package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
)

func TestPaymentWebhookValidationAndDurableAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		credentials bool
		want        int
		paid        bool
	}{
		{"missing_auth", `{"status":"CONFIRMED","payload":"order-1","id":"tx-1","amount":100,"currency":"RUB"}`, false, 400, false},
		{"broken_json", `{`, true, 400, false},
		{"oversized", strings.Repeat("x", (1<<20)+1), true, 400, false},
		{"not_settled", `{"status":"PENDING","payload":"order-1"}`, true, 200, false},
		{"missing_money", `{"status":"CONFIRMED","payload":"order-1","id":"tx-1"}`, true, 400, false},
		{"wrong_transaction", `{"status":"CONFIRMED","payload":"order-1","id":"other-tx","amount":100,"currency":"RUB"}`, true, 200, false},
		{"wrong_amount", `{"status":"CONFIRMED","payload":"order-1","id":"tx-1","amount":200,"currency":"RUB"}`, true, 200, false},
		{"wrong_currency", `{"status":"CONFIRMED","payload":"order-1","id":"tx-1","amount":100,"currency":"USD"}`, true, 200, false},
		{"paid_panel_offline", `{"status":"CONFIRMED","payload":"order-1","id":"tx-1","amount":100,"currency":"RUB"}`, true, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, db := testServer(t)
			ctx := context.Background()
			cfg := &config.Settings{Currency: "RUB", PaymentProviders: []string{"platega"}, PaymentMethods: []string{"sbp"}, PlategaMerchant: "merchant", PlategaSecret: "secret"}
			registry, err := payments.BuildRegistry(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			server.pay = payments.New(db, nil, registry, cfg)
			subject, err := db.EnsureTelegramSubject(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			days := 30
			_, err = db.CreateOrder(ctx, domain.Order{OrderID: "order-1", Provider: "platega", TelegramID: subject.TelegramID, AccountID: &subject.ID, PlanID: "30d", Kind: domain.KindSubscription, Amount: 10000, Currency: "RUB", Status: domain.OrderPending, PlanDays: &days})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.SetPaymentLink(ctx, "order-1", "https://pay.example", "tx-1", nil); err != nil {
				t.Fatal(err)
			}
			request := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/webhooks/platega", strings.NewReader(tc.body))
				if tc.credentials {
					r.Header.Set("X-MerchantId", "merchant")
					r.Header.Set("X-Secret", "secret")
				}
				rec := httptest.NewRecorder()
				server.Handler().ServeHTTP(rec, r)
				return rec
			}
			rec := request()
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			order, err := db.OrderByID(ctx, "order-1")
			if err != nil {
				t.Fatal(err)
			}
			if (order.Status == domain.OrderPaid) != tc.paid {
				t.Fatalf("unexpected order state %s", order.Status)
			}
			if tc.paid {
				if rec = request(); rec.Code != 200 {
					t.Fatal("replayed callback failed")
				}
				order, _ = db.OrderByID(ctx, "order-1")
				if order.TransactionID == nil || *order.TransactionID != "tx-1" {
					t.Fatal("replay replaced original transaction")
				}
			}
		})
	}
}

func TestWebhookDoesNotAcknowledgeDatabaseFailure(t *testing.T) {
	server, db := testServer(t)
	cfg := &config.Settings{PaymentProviders: []string{"platega"}, PaymentMethods: []string{"sbp"}, PlategaMerchant: "merchant", PlategaSecret: "secret"}
	registry, _ := payments.BuildRegistry(cfg, nil)
	server.pay = payments.New(db, nil, registry, cfg)
	db.Close()
	r := httptest.NewRequest(http.MethodPost, "/webhooks/platega", strings.NewReader(`{"status":"CONFIRMED","payload":"order-1","id":"tx-1","amount":100,"currency":"RUB"}`))
	r.Header.Set("X-MerchantId", "merchant")
	r.Header.Set("X-Secret", "secret")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, r)
	if rec.Code != 500 {
		t.Fatalf("unsaved payment acknowledged: %d", rec.Code)
	}
}

func TestTicketOwnershipBlocksReadingAndWriting(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()
	owner, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureTelegramSubject(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Private subject", "private-support-message", "")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/support/tickets/%d", ticket.ID)
	cookie := sessionCookieFor(t, db, other.ID)
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, r)
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "private-support-message") {
		t.Fatal("another user's support history disclosed")
	}
	csrf, cookies := csrfFrom(t, server.Handler())
	cookies = append(cookies, cookie)
	rec = postForm(t, server.Handler(), path, url.Values{"csrf": {csrf}, "message": {"injected reply"}}, cookies)
	if rec.Code != 404 {
		t.Fatalf("foreign reply was not rejected: %d %s", rec.Code, rec.Body.String())
	}
	messages, err := db.TicketMessages(ctx, ticket.ID, 20)
	if err != nil || len(messages) != 1 {
		t.Fatal("foreign reply modified ticket")
	}
}

func TestAppImportEscapesSecretsAndNeverAllowsArbitrarySchemes(t *testing.T) {
	server, _ := testServer(t)
	for _, app := range []string{"happ", "incy", "clashmi", "flclashx"} {
		t.Run(app, func(t *testing.T) {
			secretURL := `https://sub.example/path?x="</script><script>alert(1)</script>`
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app-import/"+app+"?u="+url.QueryEscape(secretURL), nil))
			if rec.Code != 200 {
				t.Fatalf("bridge failed: %d", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") || strings.Contains(rec.Body.String(), "#ZgotmplZ") {
				t.Fatal("unsafe or non-functional deep link")
			}
			if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("subscription link may leak through cache/referrer")
			}
		})
	}
	for _, path := range []string{"/app-import/javascript?u=https://example.com", "/app-import/happ?u=javascript:alert(1)", "/app-import/happ?u=file:///etc/passwd", "/app-import/happ?u=" + strings.Repeat("a", 2100)} {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 400 {
			t.Fatalf("unsafe bridge accepted: %s", path)
		}
	}
}
