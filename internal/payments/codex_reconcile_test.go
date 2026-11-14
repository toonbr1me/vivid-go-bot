package payments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func TestReconcileProviderConfirmsAmountAndIdentity(t *testing.T) {
	for _, name := range []string{"yookassa", "platega", "wata"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch name {
				case "yookassa":
					if r.URL.Path != "/payments/payment-1" || r.Header.Get("Authorization") == "" {
						t.Error("нет запроса с авторизацией к платежу")
					}
					_, _ = w.Write([]byte(`{"id":"payment-1","status":"succeeded","amount":{"value":"123.45","currency":"RUB"},"metadata":{"order_id":"order-1"}}`))
				case "platega":
					if r.URL.Path != "/transaction/payment-1" || r.Header.Get("X-Secret") != "secret" {
						t.Error("нет запроса с авторизацией к транзакции")
					}
					_, _ = w.Write([]byte(`{"id":"payment-1","status":"CONFIRMED","paymentDetails":{"amount":123.45,"currency":"RUB"},"payload":"order-1"}`))
				case "wata":
					if r.URL.Path != "/v2/transactions" || r.URL.Query().Get("orderId") != "order-1" || r.Header.Get("Authorization") != "Bearer method-token" {
						t.Error("поиск заказа без правильного терминала")
					}
					_, _ = w.Write([]byte(`{"items":[{"id":"declined","kind":"Payment","status":"Declined","orderId":"order-1"},{"id":"transaction-1","kind":"Payment","status":"Paid","orderId":"order-1","amount":123.45,"currency":"RUB"}]}`))
				}
			}))
			defer server.Close()
			var checker paymentChecker
			switch name {
			case "yookassa":
				checker = NewYooKassaProvider(server.URL, "shop", "secret", nil, time.Second)
			case "platega":
				checker = NewPlategaProvider(server.URL, "merchant", "secret", nil, time.Second)
			case "wata":
				checker = NewWataProvider(server.URL, "token", "", nil, map[string]string{"card": "method-token"}, time.Second)
			}
			event, err := checker.CheckPayment(context.Background(), domain.Order{OrderID: "order-1", PaymentLinkID: ptr("payment-1"), PaymentMethod: ptr("card")})
			if err != nil {
				t.Fatal(err)
			}
			if event.OrderID != "order-1" || event.Status != StatusPaid || event.Amount == nil || *event.Amount != domain.Money(12345) || event.Currency != "RUB" {
				t.Fatalf("неверное подтверждение: %+v", event)
			}
		})
	}
}
