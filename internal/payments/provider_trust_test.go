package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestYooKassaTrustsVerifiedPaymentInsteadOfWebhookClaims(t *testing.T) {
	for _, tc := range []struct {
		name, status, id, amount, currency string
		saved                              bool
		wantErr, errorIsNoEvent            bool
	}{
		{"settled", "succeeded", "payment-1", "100.01", "RUB", true, false, false},
		{"not_saved", "succeeded", "payment-1", "100.01", "RUB", false, false, false},
		{"pending", "pending", "payment-1", "100.01", "RUB", true, true, true},
		{"capture_not_complete", "waiting_for_capture", "payment-1", "100.01", "RUB", true, true, true},
		{"wrong_identity", "succeeded", "another-payment", "100.01", "RUB", true, true, false},
		{"missing_amount", "succeeded", "payment-1", "", "RUB", true, true, false},
		{"invalid_amount", "succeeded", "payment-1", "nan", "RUB", true, true, false},
		{"missing_currency", "succeeded", "payment-1", "100.01", "", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/payments/payment-1" || r.Header.Get("Authorization") == "" {
					t.Error("payment was not rechecked through authenticated API")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"id": tc.id, "status": tc.status, "amount": map[string]string{"value": tc.amount, "currency": tc.currency}, "metadata": map[string]string{"order_id": "verified-order"}, "payment_method": map[string]any{"id": "saved-card", "saved": tc.saved}})
			}))
			defer server.Close()
			p := NewYooKassaProvider(server.URL, "shop", "secret", []string{"card"}, time.Second)
			event, err := p.Verify(context.Background(), []byte(`{"type":"notification","event":"payment.succeeded","object":{"id":"payment-1","status":"succeeded","metadata":{"order_id":"forged-order"},"amount":{"value":"99999","currency":"USD"}}}`), nil)
			if tc.wantErr {
				if err == nil {
					t.Fatal("unverified or incomplete payment accepted")
				}
				if tc.errorIsNoEvent && !errors.Is(err, ErrNoEvent) {
					t.Fatalf("pending treated as a failed payment: %v", err)
				}
				if !tc.errorIsNoEvent && !errors.Is(err, ErrVerification) {
					t.Fatalf("bad verification error: %v", err)
				}
				return
			}
			if err != nil || event.OrderID != "verified-order" || event.TransactionID != "payment-1" || event.Amount == nil || *event.Amount != 10001 || event.Currency != "RUB" {
				t.Fatalf("webhook overrode verified facts: %+v %v", event, err)
			}
			if (event.SavedMethodID != "") != tc.saved {
				t.Fatal("unsaved card was retained for automatic debit")
			}
		})
	}
}

func TestPlategaSubscriptionActivationAloneDoesNotGrantPaidAccess(t *testing.T) {
	p := NewPlategaProvider("https://unused.example", "merchant", "secret", []string{"sbp"}, time.Second)
	headers := http.Header{}
	headers.Set("X-MerchantId", "merchant")
	headers.Set("X-Secret", "secret")
	for _, status := range []string{"Active", "ACTIVE", "PENDING", "Pending", "Failed"} {
		body, _ := json.Marshal(map[string]any{"status": status, "subscriptionId": "subscription-1", "id": "some-id"})
		if _, err := p.Verify(context.Background(), body, headers); !errors.Is(err, ErrNoEvent) {
			t.Fatalf("subscription status %s was mistaken for money: %v", status, err)
		}
	}
	headers.Set("X-Secret", "wrong-secret")
	if _, err := p.Verify(context.Background(), []byte(`{"status":"CONFIRMED","subscriptionId":"subscription-1","id":"tx-1","amount":100,"currency":"RUB"}`), headers); !errors.Is(err, ErrVerification) {
		t.Fatal("forged recurring callback accepted")
	}
}

func TestPlategaPaidCallbackRequiresCompletePaymentFacts(t *testing.T) {
	p := NewPlategaProvider("https://unused.example", "merchant", "secret", []string{"card"}, time.Second)
	headers := http.Header{"X-Merchantid": {"merchant"}, "X-Secret": {"secret"}}
	for _, body := range []string{
		`{"status":"CONFIRMED","payload":"order","id":"tx"}`,
		`{"status":"CONFIRMED","payload":"order","id":"tx","amount":100}`,
		`{"status":"CONFIRMED","payload":"order","id":"tx","amount":"nan","currency":"RUB"}`,
		`{"status":"CONFIRMED","payload":"order","amount":100,"currency":"RUB"}`,
		`{"status":"CONFIRMED","payload":"order","id":"tx","amount":0,"currency":"RUB"}`,
	} {
		if _, err := p.Verify(context.Background(), []byte(body), headers); !errors.Is(err, ErrVerification) {
			t.Fatalf("incomplete payment accepted: %s %v", body, err)
		}
	}
}
