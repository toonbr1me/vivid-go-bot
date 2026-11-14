package domain

import (
	"strings"
	"testing"
)

func TestRecurringCustomerStates(t *testing.T) {
	for _, reason := range []string{"user", "declined", "terms", "review", ""} {
		a := RecurringAgreement{StopReason: reason, LastError: "secret-gateway-token"}
		text := a.StatusText()
		if text == "" || strings.Contains(text, a.LastError) {
			t.Fatal("unsafe/missing status")
		}
	}
	for _, a := range []RecurringAgreement{{Enabled: true}, {Enabled: true, Activated: true}, {Enabled: true, LastError: "secret"}, {Enabled: true, Activated: true, PendingOrderID: new("order")}} {
		if a.StatusText() == "" || strings.Contains(a.StatusText(), "secret") {
			t.Fatal("missing/unsafe state")
		}
	}
}
