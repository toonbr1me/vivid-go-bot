package payments

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type storedOrderProvider struct {
	db   *storage.DB
	seen string
}

func (*storedOrderProvider) Name() string       { return "stored" }
func (*storedOrderProvider) Methods() []string  { return []string{"card"} }
func (*storedOrderProvider) TelegramOnly() bool { return false }
func (*storedOrderProvider) Verify(context.Context, []byte, http.Header) (Event, error) {
	return Event{}, ErrNoEvent
}
func (p *storedOrderProvider) CreateLink(ctx context.Context, req CreateRequest) (Link, error) {
	order, err := p.db.OrderByID(ctx, req.OrderID)
	if err != nil {
		return Link{}, err
	}
	if order.Status != domain.OrderPending || order.Amount != req.Amount {
		return Link{}, errors.New("нет сохранённых условий покупки")
	}
	p.seen = req.OrderID
	return Link{URL: "https://pay.example/invoice", ID: "gateway-id"}, nil
}

func TestReliabilityCheckoutStoresOrderBeforeProvider(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true}); err != nil {
		t.Fatal(err)
	}
	provider := &storedOrderProvider{db: db}
	svc.registry.add(provider)
	svc.cfg.LinkTTLMinutes = 30
	checkout, err := svc.Checkout(ctx, Purchase{TelegramID: ptr(int64(45001)), PlanID: "30d"})
	if err != nil {
		t.Fatal(err)
	}
	order, err := db.OrderByID(ctx, checkout.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if provider.seen != checkout.OrderID || deref(order.PaymentLinkID) != "gateway-id" || deref(order.PaymentURL) != checkout.URL {
		t.Fatalf("счёт не привязан: %+v", order)
	}
}

func TestReliabilityInboxKeepsUnknownOrderAndReplays(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true}); err != nil {
		t.Fatal(err)
	}
	amount := domain.FromRubles(100)
	event := Event{OrderID: "early-webhook", Status: StatusPaid, TransactionID: "early-tx", Amount: &amount, Currency: "RUB"}
	if _, err := svc.ReceiveEvent(ctx, "yookassa", event); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM payment_inbox WHERE processed_at IS NULL`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("потеряно раннее событие: %d %v", pending, err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, 45002)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateOrder(ctx, domain.Order{OrderID: event.OrderID, Provider: "yookassa", TelegramID: subject.TelegramID, AccountID: &subject.ID,
		PlanID: "30d", Kind: domain.KindSubscription, Amount: amount, Currency: "RUB", Status: domain.OrderPending, PlanDays: intPtr(30)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE payment_inbox SET retry_at = $1`, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessPaymentInbox(ctx, 10); err != nil {
		t.Fatal(err)
	}
	order, err := db.OrderByID(ctx, event.OrderID)
	if err != nil || order.Status != domain.OrderProvisioned {
		t.Fatalf("событие не восстановило покупку: %s %v", order.Status, err)
	}
	before := panel.lastExpiry()
	if _, err := svc.ReceiveEvent(ctx, "yookassa", event); err != nil {
		t.Fatal(err)
	}
	if !panel.lastExpiry().Equal(before) {
		t.Fatal("повтор события продлил подписку")
	}
}
