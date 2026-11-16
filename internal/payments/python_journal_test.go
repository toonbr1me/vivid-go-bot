package payments

import (
	"context"
	"encoding/json"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"testing"
	"time"
)

func TestPythonFulfillmentTargetsRemainReadableAndAreNotAllocatedTwice(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 8344)
	if err != nil {
		t.Fatal(err)
	}
	target := time.Now().UTC().Truncate(time.Second).Add(72 * time.Hour)
	for i, fixture := range []struct {
		id     string
		result any
	}{
		{"python-issued", map[string]any{"target": target, "prior": map[string]any{"trafficLimitBytes": 0}}},
		{"go-reserved", target.Add(24 * time.Hour)},
	} {
		status := domain.OrderProvisioned
		if i == 1 {
			status = domain.OrderPaid
		}
		order, err := db.CreateOrder(ctx, domain.Order{OrderID: fixture.id, AccountID: &subject.ID, TelegramID: subject.TelegramID, Provider: "support_comp", Kind: domain.KindSubscription, PlanID: "gift", Status: status, Currency: "RUB"})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(fixture.result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Pool.Exec(ctx, `INSERT INTO order_fulfillment_steps(order_id,step,result) VALUES($1,'subscription_target',$2::jsonb)`, fixture.id, raw); err != nil {
			t.Fatal(err)
		}
		actual, err := s.reserveTarget(ctx, db.Journal(fixture.id), order, subject, "unused", 30)
		want := target
		if i == 1 {
			want = target.Add(24 * time.Hour)
		}
		if err != nil || !actual.Equal(want) {
			t.Fatalf("stored target changed: %v %v", actual, err)
		}
	}
	reserved, err := db.ReservedExpiry(ctx, subject)
	if err != nil || !reserved.Equal(target.Add(24*time.Hour)) {
		t.Fatalf("mixed Python/Go targets: %v %v", reserved, err)
	}
}
