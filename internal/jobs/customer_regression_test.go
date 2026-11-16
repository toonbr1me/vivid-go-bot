package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestExpiryReminderDescribesAutomaticChargeInsteadOfAnotherPurchase(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	tg := int64(8031)
	expiry := time.Now().UTC().Add(6 * time.Hour)
	provisionedOrder(t, db, tg, expiry)
	s, err := db.SubjectByTelegramID(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	a := domain.RecurringAgreement{ID: "agreement", SubjectID: s.ID, Provider: "yookassa", Method: "card", PlanID: "30d", Amount: 19900, Currency: "RUB", Days: 30}
	if err := db.CreateRecurring(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET activated=true,next_charge_at=$1,pending_order_id=NULL WHERE id='agreement'`, expiry); err != nil {
		t.Fatal(err)
	}
	sender := &captureSender{}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || strings.Contains(sender.sent[0], "Продлите заранее") || !strings.Contains(sender.sent[0], "Следующее списание") || !strings.Contains(sender.sent[0], "199.00") {
		t.Fatal(sender.sent)
	}
}

func TestWebInvoicesAndExpiryUseNewlyLinkedTelegram(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.UpsertGoogleSubject(ctx, "web-reminders", "web@example.com", "Web")
	if err != nil {
		t.Fatal(err)
	}
	link := "https://pay.example/original?x=1&y=2"
	if _, err := db.CreateOrder(ctx, domain.Order{OrderID: "web-invoice", Provider: "yookassa", AccountID: &s.ID, PlanID: "30d", Kind: domain.KindSubscription, Status: domain.OrderPending, Currency: "RUB", Amount: 10000, PaymentURL: &link}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE orders SET created_at=now()-interval '2 hours' WHERE order_id='web-invoice'`); err != nil {
		t.Fatal(err)
	}
	if err := db.AttachTelegram(ctx, s.ID, 8041); err != nil {
		t.Fatal(err)
	}
	sender := &captureSender{}
	if err := remindUnpaid(ctx, db, sender, &config.Settings{RemindUnpaidHours: 1}); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], "Продолжить оплату этого счёта") || !strings.Contains(sender.sent[0], "web-invoice") {
		t.Fatal(sender.sent)
	}
	if _, err := db.MarkPaid(ctx, "web-invoice", "web-paid", time.Now()); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().UTC().Add(6 * time.Hour)
	days := 30
	if err := db.MarkProvisioned(ctx, "web-invoice", &expiry, &days); err != nil {
		t.Fatal(err)
	}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 2 || !strings.Contains(sender.sent[1], "Подписка заканчивается") {
		t.Fatal(sender.sent)
	}
}
