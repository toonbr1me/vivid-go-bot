package payments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Оплата с баланса: деньги списываются, заказ создаётся и выдаётся.
func TestPurchaseWithBalanceSpendsMoney(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	telegramID := int64(3101)

	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if _, err := db.AddBalance(ctx, subject.ID, domain.FromRubles(500), nil); err != nil {
		t.Fatalf("пополнение: %v", err)
	}

	checkout, err := svc.PurchaseWithBalance(ctx, Purchase{TelegramID: &telegramID, PlanID: "30d"})
	if err != nil {
		t.Fatalf("покупка: %v", err)
	}
	if checkout.Amount != domain.FromRubles(100) {
		t.Fatalf("сумма покупки = %s, ждали 100", checkout.Amount)
	}

	balance, err := db.Balance(ctx, subject.ID)
	if err != nil {
		t.Fatalf("баланс: %v", err)
	}
	if balance != domain.FromRubles(400) {
		t.Fatalf("баланс после покупки = %s, ждали 400", balance)
	}

	order, err := db.OrderByID(ctx, checkout.OrderID)
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	if order.Status != domain.OrderProvisioned {
		t.Fatalf("статус = %q, ждали %q", order.Status, domain.OrderProvisioned)
	}
	if order.ExpireAt == nil {
		t.Fatal("срок подписки не записан")
	}
	if panel.expireUpdates() == 0 {
		t.Fatal("панель не получила срок подписки")
	}
}

// Нехватка денег на балансе — понятная ошибка, деньги не трогаются.
func TestPurchaseWithBalanceRejectsInsufficientFunds(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	telegramID := int64(3102)

	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if _, err := db.AddBalance(ctx, subject.ID, domain.FromRubles(10), nil); err != nil {
		t.Fatalf("пополнение: %v", err)
	}

	if _, err := svc.PurchaseWithBalance(ctx, Purchase{TelegramID: &telegramID, PlanID: "30d"}); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("ждали ErrInsufficientBalance, получили %v", err)
	}
	balance, err := db.Balance(ctx, subject.ID)
	if err != nil {
		t.Fatalf("баланс: %v", err)
	}
	if balance != domain.FromRubles(10) {
		t.Fatalf("баланс изменился при неудачной покупке: %s", balance)
	}
	orders, err := db.OrdersForTelegram(ctx, telegramID, 10)
	if err != nil {
		t.Fatalf("заказы: %v", err)
	}
	if len(orders) != 0 {
		t.Fatalf("заказ создан без денег: %+v", orders)
	}
}

// Подарочный код выдаёт дни и не срабатывает повторно.
func TestRedeemGiftPromoGrantsDaysOnce(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	telegramID := int64(3103)

	if err := db.CreatePromo(ctx, domain.PromoCode{
		Code: "GIFT7", GrantDays: 7, IsActive: true,
	}); err != nil {
		t.Fatalf("промокод: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}

	days, err := svc.RedeemGiftPromo(ctx, subject, "GIFT7")
	if err != nil {
		t.Fatalf("активация кода: %v", err)
	}
	if days != 7 {
		t.Fatalf("выдано дней = %d, ждали 7", days)
	}
	expiry := panel.lastExpiry()
	if expiry.IsZero() {
		t.Fatal("панель не получила срок подписки")
	}

	// Повторная активация того же кода — отказ: подарок одноразовый.
	if _, err := svc.RedeemGiftPromo(ctx, subject, "GIFT7"); !errors.Is(err, ErrPromoRejected) {
		t.Fatalf("повторная активация должна быть отклонена, получили %v", err)
	}
	if last := panel.lastExpiry(); !last.Equal(expiry) {
		t.Fatalf("после повтора срок = %v, было %v — подарок выдан дважды", last, expiry)
	}
}

// Несуществующий и неподходящий коды дают ошибку, а не «нулевые дни».
func TestRedeemGiftPromoRejectsBadCodes(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	telegramID := int64(3104)

	if err := db.CreatePromo(ctx, domain.PromoCode{
		Code: "SALE10", DiscountPercent: 10, IsActive: true,
	}); err != nil {
		t.Fatalf("промокод: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if _, err := svc.RedeemGiftPromo(ctx, subject, "НЕТТАКОГО"); !errors.Is(err, ErrPromoRejected) {
		t.Fatalf("несуществующий код должен отклоняться, получили %v", err)
	}
	if _, err := svc.RedeemGiftPromo(ctx, subject, "SALE10"); !errors.Is(err, ErrPromoRejected) {
		t.Fatalf("код со скидкой не должен выдавать дни, получили %v", err)
	}
}

// Выключенный подарок не выдаётся, даже если код правильный.
func TestRedeemGiftPromoRejectsInactiveCode(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	telegramID := int64(3107)

	if err := db.CreatePromo(ctx, domain.PromoCode{
		Code: "OLDGIFT", GrantDays: 7, IsActive: false,
	}); err != nil {
		t.Fatalf("промокод: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if _, err := svc.RedeemGiftPromo(ctx, subject, "OLDGIFT"); !errors.Is(err, ErrPromoRejected) {
		t.Fatalf("выключенный код должен отклоняться, получили %v", err)
	}
}

// Вебхук оплаты: заказ переходит в оплаченные и выдаётся. Повторное
// событие не должен выдать второй период.
func TestHandleEventProvisionsOnce(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	telegramID := int64(3105)

	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	created, err := db.CreateOrder(ctx, domain.Order{
		OrderID:    "wh-1",
		Provider:   "wata",
		TelegramID: &telegramID,
		AccountID:  &subject.ID,
		PlanID:     "30d",
		Kind:       domain.KindSubscription,
		Amount:     domain.FromRubles(100),
		Currency:   "RUB",
		Status:     domain.OrderPending,
		PlanDays:   ptr(30),
	})
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	_ = created

	event := Event{
		OrderID:       "wh-1",
		Amount:        ptr(domain.FromRubles(100)),
		Status:        "paid",
		TransactionID: "tx-1",
	}
	handled, err := svc.HandleEvent(ctx, "wata", event)
	if err != nil {
		t.Fatalf("событие: %v", err)
	}
	if !handled {
		t.Fatal("событие об оплате должно обрабатываться")
	}
	order, err := db.OrderByID(ctx, "wh-1")
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	if order.Status != domain.OrderProvisioned {
		t.Fatalf("статус = %q, ждали %q", order.Status, domain.OrderProvisioned)
	}
	expiry := panel.lastExpiry()
	if expiry.IsZero() {
		t.Fatal("панель не получила срок подписки")
	}

	// Повторное событие: срок не должен увеличиться вдвое.
	if _, err := svc.HandleEvent(ctx, "wata", event); err != nil {
		t.Fatalf("повторное событие: %v", err)
	}
	if last := panel.lastExpiry(); !last.Equal(expiry) {
		t.Fatalf("после повтора срок = %v, было %v", last, expiry)
	}
}

// Вебхук о неизвестном заказе: событие сохраняется, но заказ не меняется и
// шлюз не получает ошибку — иначе он будет повторять его бесконечно.
func TestHandleEventIgnoresUnknownOrder(t *testing.T) {
	svc, _, _ := testProvisionService(t)
	changed, err := svc.ReceiveEvent(context.Background(), "wata", Event{
		OrderID: "нет-такого",
		Status:  "paid",
	})
	if err != nil {
		t.Fatalf("событие: %v", err)
	}
	if changed {
		t.Fatal("неизвестный заказ не должен считаться обработанным")
	}
}

// Смета: промокод не применяется к чужому тарифу.
func TestQuotePlansKeepsPriceForOtherPlan(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 3106)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.CreatePromo(ctx, domain.PromoCode{
		Code: "ONLY90", DiscountPercent: 50, PlanIDs: []string{"90d"}, IsActive: true,
	}); err != nil {
		t.Fatalf("промокод: %v", err)
	}
	for _, plan := range []domain.Plan{
		{ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true},
		{ID: "90d", Title: "Квартал", Days: 90, Price: domain.FromRubles(250), IsActive: true},
	} {
		if err := db.SavePlan(ctx, plan); err != nil {
			t.Fatalf("тариф %s: %v", plan.ID, err)
		}
	}

	quotes, err := svc.QuotePlans(ctx, subject.ID, "ONLY90")
	if err != nil {
		t.Fatalf("смета: %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("в смете %d тарифов, ждали 2", len(quotes))
	}
	for _, quote := range quotes {
		switch quote.Plan.ID {
		case "30d":
			if quote.Amount != domain.FromRubles(100) {
				t.Fatalf("цена месяца = %s, ждали 100", quote.Amount)
			}
		case "90d":
			if quote.Amount != domain.FromRubles(125) {
				t.Fatalf("цена квартала = %s, ждали 125", quote.Amount)
			}
		}
	}
}

// Сумма в звёздах: без провайдера звёзд их ноль, с ценой — считает вверх.
func TestStarsAmount(t *testing.T) {
	svc, _ := testService(t)
	if got := svc.StarsAmount(domain.FromRubles(100)); got != 0 {
		t.Fatalf("звёзд без провайдера = %d, ждали 0", got)
	}

	registry := &Registry{providers: map[string]Provider{}}
	registry.add(NewStarsProvider(nil, domain.FromRubles(2)))
	stars := &Service{registry: registry}
	// 100 ₽ при цене 2 ₽ за звезду — 50 звёзд (округление вверх).
	if got := stars.StarsAmount(domain.FromRubles(100)); got != 50 {
		t.Fatalf("звёзд = %d, ждали 50", got)
	}
	// 3 ₽ при цене 2 ₽ — две звезды, а не одна.
	if got := stars.StarsAmount(domain.FromRubles(3)); got != 2 {
		t.Fatalf("звёзд = %d, ждали 2", got)
	}
}

// Цена пакета устройств считается от остатка срока.
func TestDeviceQuotesFollowRemainingDays(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	svc.cfg.DeviceLimitEnabled = true
	ctx := context.Background()
	if err := db.SaveDevicePack(ctx, domain.DevicePack{
		ID: "dev1", Title: "+1 устройство", Devices: 1,
		Price: domain.FromRubles(100), Days: 30, IsActive: true,
	}); err != nil {
		t.Fatalf("пакет: %v", err)
	}
	now := time.Now().UTC()
	expires := now.Add(15 * 24 * time.Hour)

	quotes, err := svc.DeviceQuotes(ctx, &expires, now)
	if err != nil {
		t.Fatalf("смета пакетов: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("пакетов в смете %d, ждали 1", len(quotes))
	}
	if quotes[0].Amount != domain.FromRubles(50) {
		t.Fatalf("цена за половину срока = %s, ждали 50", quotes[0].Amount)
	}
	if quotes[0].BilledDays != 15 {
		t.Fatalf("оплаченных дней = %d, ждали 15", quotes[0].BilledDays)
	}
}
