package payments

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"sync"
	"testing"
)

func TestGiftPurchaseClaimReplayAndPlanSnapshot(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	svc.cfg.DeviceLimitEnabled = true
	limit := int64(2 << 30)
	plan := domain.Plan{ID: "giftable", Title: "Gift", Days: 30, Price: 10000, Devices: 3, IsActive: true, GiftEnabled: true, TrafficLimitBytes: &limit}
	if err := db.SavePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	buyer, err := db.EnsureTelegramSubject(ctx, 7001)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddBalance(ctx, buyer.ID, 50000, nil); err != nil {
		t.Fatal(err)
	}
	checkout, err := svc.PurchaseWithBalance(ctx, Purchase{TelegramID: buyer.TelegramID, PlanID: plan.ID, Gift: true})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := db.OrderByID(ctx, checkout.OrderID)
	if err != nil || purchase.Kind != domain.KindGift || purchase.Status != domain.OrderProvisioned {
		t.Fatalf("purchase: %+v %v", purchase, err)
	}
	if panel.created != 0 {
		t.Fatal("gift issued to buyer")
	}
	gifts, err := db.Gifts(ctx, buyer.ID)
	if err != nil || len(gifts) != 1 {
		t.Fatalf("gift history: %+v %v", gifts, err)
	}
	if _, err = db.ClaimGift(ctx, gifts[0].ID, buyer); err == nil {
		t.Fatal("buyer claimed own gift")
	}
	plan.Days = 2
	plan.Price = 99000
	newLimit := int64(100)
	plan.TrafficLimitBytes = &newLimit
	if err = db.SavePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	var mu sync.Mutex
	var winners []domain.Order
	for _, id := range []int64{7002, 7003} {
		s, err := db.EnsureTelegramSubject(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		group.Go(func() {
			o, e := db.ClaimGift(ctx, gifts[0].ID, s)
			if e == nil {
				mu.Lock()
				winners = append(winners, o)
				mu.Unlock()
			}
		})
	}
	group.Wait()
	if len(winners) != 1 {
		t.Fatalf("claimed %d times", len(winners))
	}
	order := winners[0]
	if order.PlanDays == nil || *order.PlanDays != 30 || order.PlanProfile == nil || order.PlanProfile.TrafficLimitBytes == nil || *order.PlanProfile.TrafficLimitBytes != limit {
		t.Fatalf("sold terms changed: %+v", order)
	}
	if _, err = svc.Provision(ctx, order); err != nil {
		t.Fatal(err)
	}
	first := panel.expireUpdates()
	winner, err := db.SubjectByID(ctx, *order.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.ClaimGift(ctx, winner, gifts[0].ID)
	if err != nil || replayed.OrderID != order.OrderID {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
	if panel.expireUpdates() != first {
		t.Fatal("gift extended twice")
	}
	balance, err := db.Balance(ctx, buyer.ID)
	if err != nil || balance != 40000 {
		t.Fatalf("buyer balance: %v %v", balance, err)
	}
}

func TestGiftBalanceRetryDoesNotChargeTwice(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	buyer, err := db.EnsureTelegramSubject(ctx, 7100)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SavePlan(ctx, domain.Plan{ID: "gift", Title: "Gift", Days: 10, Price: 10000, IsActive: true, GiftEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddBalance(ctx, buyer.ID, 50000, nil); err != nil {
		t.Fatal(err)
	}
	purchase := Purchase{TelegramID: buyer.TelegramID, PlanID: "gift", Gift: true, RequestID: "same-purchase-request-123"}
	var wg sync.WaitGroup
	var ids sync.Map
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			c, e := svc.PurchaseWithBalance(ctx, purchase)
			if e != nil {
				t.Error(e)
				return
			}
			ids.Store(c.OrderID, true)
		})
	}
	wg.Wait()
	count := 0
	ids.Range(func(_, _ any) bool { count++; return true })
	if count != 1 {
		t.Fatalf("created %d purchases", count)
	}
	balance, err := db.Balance(ctx, buyer.ID)
	if err != nil || balance != 40000 {
		t.Fatalf("balance %v %v", balance, err)
	}
	gifts, err := db.Gifts(ctx, buyer.ID)
	if err != nil || len(gifts) != 1 {
		t.Fatalf("history: %+v %v", gifts, err)
	}
}

func TestGiftInvoiceExistsBeforeProviderAndIsNotClaimableBeforePayment(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	provider := &storedOrderProvider{db: db}
	svc.registry.add(provider)
	svc.cfg.LinkTTLMinutes = 30
	if err := db.SavePlan(ctx, domain.Plan{ID: "giftable", Title: "Gift", Days: 10, Price: 10000, IsActive: true, GiftEnabled: true}); err != nil {
		t.Fatal(err)
	}
	buyer, err := db.EnsureTelegramSubject(ctx, 7200)
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := db.EnsureTelegramSubject(ctx, 7201)
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := svc.Checkout(ctx, Purchase{TelegramID: buyer.TelegramID, PlanID: "giftable", Gift: true})
	if err != nil {
		t.Fatal(err)
	}
	gifts, err := db.Gifts(ctx, buyer.ID)
	if err != nil || len(gifts) != 1 {
		t.Fatalf("pending gift: %+v %v", gifts, err)
	}
	if _, err = db.ClaimGift(ctx, gifts[0].ID, recipient); err == nil {
		t.Fatal("unpaid gift claimed")
	}
	amount := domain.Money(10000)
	event := Event{OrderID: checkout.OrderID, Status: StatusPaid, TransactionID: "provider-payment", Amount: &amount, Currency: "RUB"}
	if _, err = svc.HandleEvent(ctx, provider.Name(), event); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ClaimGift(ctx, recipient, gifts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.HandleEvent(ctx, provider.Name(), event); err != nil {
		t.Fatal(err)
	}
	gifts, err = db.Gifts(ctx, buyer.ID)
	if err != nil || len(gifts) != 1 || gifts[0].ClaimedAt == nil {
		t.Fatal("gift history was lost")
	}
}
