package payments

import (
	"context"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func TestFullyDiscountedPaidPlanAlsoReplacesTrialLimit(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	svc.cfg.TrialBotDays = 1
	limit := int64(1024)
	svc.cfg.TrialTrafficBytes = &limit
	if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Days: 30, Price: 10000, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	s, err := db.EnsureTelegramSubject(ctx, 9071)
	if err != nil {
		t.Fatal(err)
	}
	trial, err := svc.Plan(ctx, "trial_bot")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.GrantTrial(ctx, s, "tg_9071", trial); err != nil {
		t.Fatal(err)
	}
	days := 30
	o, err := db.CreateOrder(ctx, domain.Order{OrderID: "free-paid-upgrade", Provider: "promo", TelegramID: s.TelegramID, AccountID: &s.ID, PlanID: "30d", PlanDays: &days, Kind: domain.KindSubscription, Status: domain.OrderPaid, Amount: 0, OriginalAmount: 10000, Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Provision(ctx, o); err != nil {
		t.Fatal(err)
	}
	user := panel.users[9071]
	if user.TrafficLimitBytes == nil || *user.TrafficLimitBytes != 0 {
		t.Fatal("discounted purchase kept trial limit", user.TrafficLimitBytes)
	}
}
