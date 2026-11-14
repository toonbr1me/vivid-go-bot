package domain

import (
	"testing"
	"time"
)

func TestOrderProgress(t *testing.T) {
	now := time.Now()
	old, future := now.Add(-3*time.Minute), now.Add(time.Minute)
	failure := "secret internal error"
	url := "https://pay.example"
	for _, tt := range []struct {
		name                  string
		order                 Order
		ready, pay, attention bool
	}{
		{"pending", Order{Status: OrderPending, PaymentURL: &url, PaymentExpiresAt: &future}, false, true, false},
		{"expired", Order{Status: OrderPending, PaymentURL: &url, PaymentExpiresAt: &old}, false, false, false},
		{"paid", Order{Status: OrderPaid, PaidAt: &now}, false, false, false},
		{"delayed", Order{Status: OrderPaid, PaidAt: &old}, false, false, true},
		{"failed issuance", Order{Status: OrderPaid, FulfillmentError: &failure}, false, false, true},
		{"ready", Order{Status: OrderProvisioned}, true, false, false},
		{"failed", Order{Status: OrderFailed}, false, false, false},
		{"cancelled", Order{Status: OrderCancelled}, false, false, false},
		{"refunded", Order{Status: OrderRefunded}, false, false, false},
		{"unknown", Order{}, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.order.Progress(now)
			if p.Ready != tt.ready || p.CanPay != tt.pay || p.NeedsAttention != tt.attention || p.Label == "" || p.Detail == failure {
				t.Fatalf("unexpected progress: %+v", p)
			}
		})
	}
}
