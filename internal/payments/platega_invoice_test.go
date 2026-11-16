package payments

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"testing"
)

func TestPlategaCallbackCannotSettleAnotherInvoice(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7791)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateOrder(ctx, domain.Order{OrderID: "invoice-bound", Provider: "platega", AccountID: &subject.ID, TelegramID: subject.TelegramID, Kind: domain.KindSubscription, Status: domain.OrderPending, Amount: 10000, Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetPaymentLink(ctx, "invoice-bound", "https://pay.example", "expected-tx", nil); err != nil {
		t.Fatal(err)
	}
	amount := domain.Money(10000)
	for _, event := range []Event{
		{OrderID: "invoice-bound", Status: StatusPaid, TransactionID: "other-tx", Amount: &amount, Currency: "RUB"},
		{OrderID: "invoice-bound", Status: StatusPaid, TransactionID: "expected-tx", Currency: "RUB"},
	} {
		if _, err = svc.HandleEvent(ctx, "platega", event); err == nil {
			t.Fatal("unverified invoice accepted")
		}
		order, e := db.OrderByID(ctx, "invoice-bound")
		if e != nil || order.Status != domain.OrderPending {
			t.Fatalf("payment state mutated: %+v %v", order, e)
		}
	}
}
