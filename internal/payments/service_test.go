package payments

import (
	"context"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

// testService собирает службу без панели и без шлюзов: тестам нужно
// оформление заказа, а не платёжный шлюз.
func testService(t *testing.T) (*Service, *storage.DB) {
	t.Helper()
	db := testdb.Open(t)
	cfg := &config.Settings{
		Currency:         "RUB",
		MinPaymentAmount: domain.FromRubles(1),
		ReferralMode:     "days",
		DeviceLimitBase:  2,
	}
	return New(db, nil, &Registry{providers: map[string]Provider{}}, cfg), db
}

func TestCheckoutWithoutProvidersFails(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	_, err := svc.Checkout(ctx, Purchase{TelegramID: ptr(int64(1001)), PlanID: "30d"})
	if err == nil {
		t.Fatal("ожидалась ошибка: шлюзов нет")
	}
}

func TestQuotePlansAppliesPromo(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.CreatePromo(ctx, domain.PromoCode{
		Code:            "SAVE10",
		DiscountPercent: 10,
		IsActive:        true,
		RewardScope:     domain.RewardScopeAll,
	}); err != nil {
		t.Fatalf("промокод: %v", err)
	}
	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}

	quotes, err := svc.QuotePlans(ctx, subject.ID, "SAVE10")
	if err != nil {
		t.Fatalf("смета: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("в смете %d тарифов, ждали 1", len(quotes))
	}
	if quotes[0].Rejection != "" {
		t.Fatalf("промокод отклонён: %s", quotes[0].Rejection)
	}
	want := domain.FromRubles(90)
	if quotes[0].Amount != want {
		t.Errorf("цена = %s, ждали %s", quotes[0].Amount, want)
	}
}

func TestQuotePlansRejectsWrongPromo(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}

	quotes, err := svc.QuotePlans(ctx, subject.ID, "НЕТТАКОГО")
	if err != nil {
		t.Fatalf("смета: %v", err)
	}
	if len(quotes) != 1 || quotes[0].Rejection == "" {
		t.Fatalf("неизвестный промокод должен быть отклонён: %+v", quotes)
	}
	if quotes[0].Amount != domain.FromRubles(100) {
		t.Errorf("цена без скидки = %s, ждали 100", quotes[0].Amount)
	}
}

func TestRecordTrialIsIdempotent(t *testing.T) {
	_, db := testService(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	claimed, err := db.RecordTrial(ctx, subject.ID, ptr(int64(1001)), "bot")
	if err != nil {
		t.Fatalf("заявка на триал: %v", err)
	}
	if !claimed {
		t.Fatal("первая заявка на триал должна быть засчитана")
	}
	claimed, err = db.RecordTrial(ctx, subject.ID, ptr(int64(1001)), "bot")
	if err != nil {
		t.Fatalf("повторная заявка: %v", err)
	}
	if claimed {
		t.Error("второй триал выдаваться не должен")
	}
	used, err := db.HasTrial(ctx, subject.ID, "bot")
	if err != nil {
		t.Fatalf("проверка триала: %v", err)
	}
	if !used {
		t.Error("база должна помнить, что триал уже был")
	}
}

func TestBalancePurchaseMarksOrderPaid(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	if _, err := db.AddBalance(ctx, subject.ID, domain.FromRubles(500), nil); err != nil {
		t.Fatalf("баланс: %v", err)
	}

	checkout, err := svc.PurchaseWithBalance(ctx, Purchase{
		TelegramID: ptr(int64(1001)),
		PlanID:     "30d",
	})
	if err != nil {
		t.Fatalf("покупка с баланса: %v", err)
	}
	order, err := db.OrderByID(ctx, checkout.OrderID)
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	if order.Status != domain.OrderPaid {
		t.Errorf("статус заказа = %s, ждали %s", order.Status, domain.OrderPaid)
	}
	if order.Amount != domain.FromRubles(100) {
		t.Errorf("сумма заказа = %s, ждали 100", order.Amount)
	}
	balance, err := db.Balance(ctx, subject.ID)
	if err != nil {
		t.Fatalf("баланс после покупки: %v", err)
	}
	if balance != domain.FromRubles(400) {
		t.Errorf("баланс после покупки = %s, ждали 400", balance)
	}
}

func TestPurchaseWithBalanceFailsWhenNotEnough(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	if _, err := db.AddBalance(ctx, subject.ID, domain.FromRubles(10), nil); err != nil {
		t.Fatalf("баланс: %v", err)
	}
	_, err = svc.PurchaseWithBalance(ctx, Purchase{TelegramID: ptr(int64(1001)), PlanID: "30d"})
	if err == nil {
		t.Fatal("ожидалась ошибка: денег на балансе мало")
	}
}

func TestLivePendingOrderIsReused(t *testing.T) {
	_, db := testService(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	// Заказ с живой ссылкой на оплату: повторное нажатие должно вернуть его же.
	expires := time.Now().Add(time.Hour)
	link := "https://pay.example/abc"
	_, err = db.CreateOrder(ctx, domain.Order{
		OrderID:          "tg-1001-abcdef",
		Provider:         "wata",
		TelegramID:       ptr(int64(1001)),
		AccountID:        &subject.ID,
		PlanID:           "30d",
		Kind:             domain.KindSubscription,
		Amount:           domain.FromRubles(100),
		Currency:         "RUB",
		Status:           domain.OrderPending,
		PaymentURL:       &link,
		PaymentExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	orders, err := db.LivePendingOrders(ctx, ptr(int64(1001)), nil, domain.KindSubscription)
	if err != nil {
		t.Fatalf("живые заказы: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("живых заказов %d, ждали 1", len(orders))
	}
	if *orders[0].PaymentURL != link {
		t.Errorf("ссылка = %s, ждали %s", *orders[0].PaymentURL, link)
	}
}

func ptr[T any](value T) *T { return &value }
