package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestPythonOrdersMigrateWithoutReissuingOrCountingTrialsAsPurchases(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7799)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		id, status string
		amount     domain.Money
		issued     bool
	}{
		{"python-paid", "paid", 30000, true}, {"python-manual", "manual_granted", 0, true},
		{"python-trial", "trial_granted", 0, true}, {"python-declined", "declined", 30000, false},
		{"python-waiting", "paid", 30000, false},
	} {
		_, err = db.CreateOrder(ctx, domain.Order{OrderID: fixture.id, Provider: "test", AccountID: &subject.ID, TelegramID: subject.TelegramID, Kind: domain.KindSubscription, Amount: fixture.amount, Currency: "RUB", Status: fixture.status})
		if err != nil {
			t.Fatal(err)
		}
		if fixture.issued {
			_, err = db.Pool.Exec(ctx, `UPDATE orders SET provisioned_at=now(), paid_at=now(),expire_at=now()+interval '30 days' WHERE order_id=$1`, fixture.id)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string]string{"python-paid": "provisioned", "python-manual": "provisioned", "python-trial": "provisioned", "python-declined": "failed", "python-waiting": "paid"} {
		order, e := db.OrderByID(ctx, id)
		if e != nil || order.Status != want {
			t.Fatalf("%s: %+v %v", id, order, e)
		}
	}
	var original string
	if err = db.Pool.QueryRow(ctx, `SELECT legacy_status FROM orders WHERE order_id='python-paid'`).Scan(&original); err != nil || original != "paid" {
		t.Fatalf("lost original status: %q %v", original, err)
	}
	stats, err := db.Stats(ctx, 1)
	if err != nil || stats.PaidOrders != 2 || stats.Revenue != 60000 || stats.Subscriptions != 3 {
		t.Fatalf("wrong accounting: %+v %v", stats, err)
	}
	pending, err := db.PendingProvisioning(ctx, 100)
	if err != nil || len(pending) != 1 || pending[0].OrderID != "python-waiting" {
		t.Fatalf("reissue: %+v %v", pending, err)
	}
	ops, err := db.Operations(ctx)
	if err != nil || ops.AwaitingAccess != 1 {
		t.Fatalf("queue: %+v %v", ops, err)
	}
	sub, err := db.SubscriptionForOwner(ctx, subject.TelegramID, &subject.ID)
	if err != nil || sub == nil || !sub.ExpireAt.After(time.Now()) {
		t.Fatalf("historical subscription unavailable: %+v %v", sub, err)
	}
}
