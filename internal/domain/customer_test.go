package domain

import (
	"testing"
	"time"
)

func TestCustomerGuidancePrioritizesPaidIssuanceOverAnotherPurchase(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Hour)
	paid := Order{OrderID: "paid", Status: OrderPaid}
	for _, access := range []Access{{}, {Active: true, Found: true, ExpireAt: &expiry}} {
		g := GuideCustomer(access, &paid, nil, true, now)
		if g.Action != "order" || g.OrderID != "paid" {
			t.Fatal(g)
		}
	}
}

func TestCustomerGuidanceStates(t *testing.T) {
	now := time.Now()
	future := now.Add(12 * time.Hour)
	past := now.Add(-time.Hour)
	for _, tc := range []struct {
		name      string
		access    Access
		order     *Order
		recurring *RecurringAgreement
		trial     bool
		action    string
	}{
		{"new", Access{}, nil, nil, true, "trial"},
		{"expired", Access{ExpireAt: &past}, nil, nil, true, "buy"},
		{"expiring", Access{Active: true, ExpireAt: &future}, nil, nil, false, "buy"},
		{"trial", Access{Active: true, ExpireAt: &future}, &Order{Provider: "trial", Status: OrderProvisioned}, nil, false, "connect"},
		{"automatic", Access{Active: true, ExpireAt: &future}, nil, &RecurringAgreement{Enabled: true}, false, "recurring"},
		{"limited", Access{Found: true, ExpireAt: &future}, nil, nil, false, "support"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if g := GuideCustomer(tc.access, tc.order, tc.recurring, tc.trial, now); g.Action != tc.action {
				t.Fatal(g)
			}
		})
	}
}
