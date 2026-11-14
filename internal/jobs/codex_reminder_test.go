package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestExpiryReminderUsesLatestSubscription(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	provisionedOrder(t, db, 46001, time.Now().Add(time.Hour))
	orders, err := db.OrdersForTelegram(ctx, 46001, 1)
	if err != nil || len(orders) != 1 {
		t.Fatalf("заказ: %v", err)
	}
	first := orders[0]
	second := first
	second.OrderID += "-renewed"
	second.Status = "paid"
	created, err := db.CreateOrder(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(30 * 24 * time.Hour)
	if err := db.MarkProvisioned(ctx, created.OrderID, &expiry, nil); err != nil {
		t.Fatal(err)
	}
	sender := &captureSender{}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 0 {
		t.Fatal("пришло напоминание после продления")
	}
}

func TestExpiryReminderRetriesFailedDelivery(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	provisionedOrder(t, db, 46002, time.Now().Add(time.Hour))
	if err := remindExpiry(ctx, db, &captureSender{err: errors.New("offline")}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE reminder_log SET lease_until = now() - interval '1 second' WHERE telegram_id = 46002`); err != nil {
		t.Fatal(err)
	}
	sender := &captureSender{}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("не повторена доставка: %d", len(sender.sent))
	}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatal("успешная доставка повторилась")
	}
}
