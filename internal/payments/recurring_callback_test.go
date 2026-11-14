package payments

import (
	"context"
	"errors"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"testing"
	"time"
)

func TestPlategaSuccessAfterFailedCallbackActivatesAgreement(t *testing.T) {
	s, db, _, purchase := recurringSafetyService(t, "platega")
	ctx := context.Background()
	checkout, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.RecurringByID(ctx, checkout.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	next := time.Now().UTC().Truncate(time.Microsecond).AddDate(0, 0, 30)
	amount := domain.Money(10000)
	e := Event{Status: StatusFailed, TransactionID: "tx-late-success", SubscriptionID: a.ExternalID, Amount: &amount, Currency: "RUB"}
	if _, err = s.applyInboxEvent(ctx, "platega", e); err != nil {
		t.Fatal(err)
	}
	a, _ = db.RecurringByID(ctx, a.ID)
	if !a.Enabled || a.LastError != "declined" {
		t.Fatal("declined debit hidden or active provider schedule reported as stopped")
	}
	e.Status = StatusPaid
	e.NextChargeAt = &next
	if _, err = s.applyInboxEvent(ctx, "platega", e); err != nil {
		t.Fatal(err)
	}
	a, err = db.RecurringByID(ctx, a.ID)
	if err != nil || !a.Activated || a.NextChargeAt == nil || !a.NextChargeAt.Equal(next) {
		t.Fatal("confirmed late payment left agreement waiting for activation")
	}
	if a.LastError != "" {
		t.Fatal("late success left stale decline warning")
	}
	later := next.AddDate(0, 0, 30)
	e.TransactionID = "next-charge"
	e.NextChargeAt = &later
	if _, err = s.applyInboxEvent(ctx, "platega", e); err != nil {
		t.Fatal(err)
	}
	e.TransactionID = "tx-late-success"
	e.NextChargeAt = &next
	if _, err = s.applyInboxEvent(ctx, "platega", e); err != nil {
		t.Fatal(err)
	}
	a, _ = db.RecurringByID(ctx, a.ID)
	if a.NextChargeAt == nil || !a.NextChargeAt.Equal(later) {
		t.Fatal("late callback moved the next charge date backwards")
	}
}

func TestPlategaTransactionReplayCannotCrossAgreements(t *testing.T) {
	s, db, _, purchase := recurringSafetyService(t, "platega")
	ctx := context.Background()
	first, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	firstAgreement, err := db.RecurringByID(ctx, first.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	amount := domain.Money(10000)
	e := Event{Status: StatusPaid, TransactionID: "unique-provider-transaction", SubscriptionID: firstAgreement.ExternalID, Amount: &amount, Currency: "RUB"}
	if _, err = s.applyInboxEvent(ctx, "platega", e); err != nil {
		t.Fatal(err)
	}
	purchase.TelegramID = ptr(int64(91002))
	second, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	secondAgreement, err := db.RecurringByID(ctx, second.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	e.SubscriptionID = secondAgreement.ExternalID
	if _, err = s.recurringEventOrder(ctx, "platega", e); !errors.Is(err, ErrVerification) {
		t.Fatalf("a transaction linked to another payer was accepted: %v", err)
	}
	secondOrder, err := db.OrderByID(ctx, second.OrderID)
	if err != nil || secondOrder.Status != domain.OrderPending {
		t.Fatal("foreign transaction modified another order")
	}
}
