package payments

import (
	"context"
	"encoding/json"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRecurringCapabilitiesRequireMerchantOptIn(t *testing.T) {
	s := New(nil, nil, nil, &config.Settings{RecurringEnabled: true, RecurringProviders: []string{"yookassa", "platega"}})
	for _, tc := range []struct {
		p, m string
		days int
		want bool
	}{{"yookassa", "card", 30, true}, {"yookassa", "sbp", 30, false}, {"platega", "sbp", 90, true}, {"platega", "card", 30, false}, {"platega", "sbp", 731, false}, {"wata", "card", 30, false}} {
		if got := s.RecurringSupported(domain.PaymentOption{Provider: tc.p, Method: tc.m}, tc.days); got != tc.want {
			t.Errorf("%s.%s: %v", tc.p, tc.m, got)
		}
	}
	s.cfg.RecurringEnabled = false
	if s.RecurringSupported(domain.PaymentOption{Provider: "yookassa", Method: "card"}, 30) {
		t.Fatal("env gate ignored")
	}
}

func TestYooKassaSeparatesConsentFromAutomaticDebit(t *testing.T) {
	var calls []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		calls = append(calls, p)
		if r.Header.Get("Idempotence-Key") != "order-1" {
			t.Error("missing idempotence key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"payment-1","confirmation":{"confirmation_url":"https://pay.example/1"}}`))
	}))
	defer server.Close()
	p := NewYooKassaProvider(server.URL, "shop", "secret", []string{"card"}, time.Second)
	for _, req := range []CreateRequest{{OrderID: "order-1", Amount: 10000, Currency: "RUB", Method: "card", Recurring: true}, {OrderID: "order-1", Amount: 10000, Currency: "RUB", SavedMethodID: "saved-card"}} {
		if _, err := p.CreateLink(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if calls[0]["save_payment_method"] != true {
		t.Fatal("consent not sent")
	}
	if calls[1]["payment_method_id"] != "saved-card" || calls[1]["confirmation"] != nil || calls[1]["save_payment_method"] != nil {
		t.Fatal("automatic debit asks for new redirect/consent")
	}
}

func TestPlategaSubscriptionUsesDedicatedMethodAndCustomer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		details := p["paymentDetails"].(map[string]any)
		meta := p["metadata"].(map[string]any)
		if p["paymentMethod"] != float64(6) || details["interval"] != float64(1) || details["intervalCount"] != float64(90) || meta["user_email"] != "7@telegram.org" {
			t.Errorf("wrong subscription payload: %v", p)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"transactionId":"subscription-1","redirect":"https://pay.example/subscription"}`))
	}))
	defer server.Close()
	p := NewPlategaProvider(server.URL, "merchant", "secret", []string{"sbp"}, time.Second)
	_, err := p.CreateLink(context.Background(), CreateRequest{OrderID: "order-1", Amount: 79900, Currency: "RUB", Method: "sbp", Recurring: true, PlanDays: 90, CustomerEmail: "7@telegram.org"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlategaChargeReplayAndNextChargeIssueDistinctOrders(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	tg := int64(777)
	_ = db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true})
	subject, err := db.EnsureTelegramSubject(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	id := "initial-recurring"
	_, err = db.CreateOrder(ctx, domain.Order{OrderID: id, Provider: "platega", TelegramID: &tg, AccountID: &subject.ID, PlanID: "30d", Kind: domain.KindSubscription, Amount: 10000, OriginalAmount: 10000, Currency: "RUB", Status: domain.OrderPending, PlanDays: ptr(30), RecurringAgreementID: &id})
	if err != nil {
		t.Fatal(err)
	}
	err = db.CreateRecurring(ctx, domain.RecurringAgreement{ID: id, SubjectID: subject.ID, Provider: "platega", Method: "sbp", PlanID: "30d", Amount: 10000, Currency: "RUB", Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetRecurringExternal(ctx, id, "subscription-1", false)
	amount := domain.Money(10000)
	event := Event{SubscriptionID: "subscription-1", TransactionID: "tx-1", Status: StatusPaid, Amount: &amount, Currency: "RUB"}
	if _, err = svc.ReceiveEvent(ctx, "platega", event); err != nil {
		t.Fatal(err)
	}
	first := panel.lastExpiry()
	if first.IsZero() {
		t.Fatal("initial charge not provisioned")
	}
	_, _ = svc.ReceiveEvent(ctx, "platega", event)
	if !panel.lastExpiry().Equal(first) {
		t.Fatal("replay granted a second period")
	}
	event.TransactionID = "tx-2"
	_, err = svc.ReceiveEvent(ctx, "platega", event)
	if err != nil {
		t.Fatal(err)
	}
	if delta := panel.lastExpiry().Sub(first.AddDate(0, 0, 30)); delta < -time.Microsecond || delta > time.Microsecond {
		t.Fatalf("next debit expiry %s; expected %s", panel.lastExpiry(), first.AddDate(0, 0, 30))
	}
	_, _ = svc.ReceiveEvent(ctx, "platega", event)
	if delta := panel.lastExpiry().Sub(first.AddDate(0, 0, 30)); delta < -time.Microsecond || delta > time.Microsecond {
		t.Fatal("second debit replayed")
	}
	wrong := domain.Money(20000)
	event.TransactionID = "foreign-charge"
	event.Amount = &wrong
	_, _ = svc.ReceiveEvent(ctx, "platega", event)
	if delta := panel.lastExpiry().Sub(first.AddDate(0, 0, 30)); delta < -time.Microsecond || delta > time.Microsecond {
		t.Fatal("wrong amount granted access")
	}
}

func TestRecurringScheduleSurvivesRestartAndCancellation(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	tg := int64(778)
	cfg := svc.cfg
	cfg.RecurringEnabled = true
	cfg.RecurringProviders = []string{"yookassa"}
	_ = db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true})
	subject, _ := db.EnsureTelegramSubject(ctx, tg)
	id := "yoo-initial"
	due := time.Now().Add(-time.Minute)
	_, err := db.CreateOrder(ctx, domain.Order{OrderID: id, Provider: "yookassa", TelegramID: &tg, AccountID: &subject.ID, PlanID: "30d", Kind: domain.KindSubscription, Amount: 10000, OriginalAmount: 10000, Currency: "RUB", Status: domain.OrderProvisioned, PlanDays: ptr(30), RecurringAgreementID: &id, ExpireAt: &due})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CreateRecurring(ctx, domain.RecurringAgreement{ID: id, SubjectID: subject.ID, Provider: "yookassa", Method: "card", PlanID: "30d", Amount: 10000, Currency: "RUB", Days: 30}); err != nil {
		t.Fatal(err)
	}
	_ = db.SetRecurringExternal(ctx, id, "saved-card", true)
	_ = db.SetRecurringSchedule(ctx, id, &due, nil)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"auto-payment"}`))
	}))
	defer server.Close()
	registry := &Registry{providers: map[string]Provider{}}
	registry.add(NewYooKassaProvider(server.URL, "shop", "secret", []string{"card"}, time.Second))
	svc = New(db, nil, registry, cfg)
	if err = svc.ProcessRecurring(ctx, 20); err != nil {
		t.Fatal(err)
	}
	svc = New(db, nil, registry, cfg)
	_ = svc.ProcessRecurring(ctx, 20)
	if calls != 1 {
		t.Fatalf("restart duplicated debit: %d", calls)
	}
	if err = svc.CancelRecurring(ctx, subject.ID); err != nil {
		t.Fatal(err)
	}
	_ = svc.ProcessRecurring(ctx, 20)
	if calls != 1 {
		t.Fatal("cancelled agreement charged")
	}
}
