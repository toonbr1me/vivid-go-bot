package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

// Субъект — один человек независимо от того, откуда он пришёл: из Telegram
// или с сайта. Повторные обращения не должны плодить дубли.

func TestEnsureTelegramSubjectIsIdempotent(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	first, err := db.EnsureTelegramSubject(ctx, 7001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	second, err := db.EnsureTelegramSubject(ctx, 7001)
	if err != nil {
		t.Fatalf("повторный субъект: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("получились разные субъекы: %d и %d", first.ID, second.ID)
	}
	if first.RefCode == nil || *first.RefCode == "" {
		t.Fatal("у субъекта должен быть реферальный код")
	}
	if second.RefCode == nil || *second.RefCode != *first.RefCode {
		t.Fatalf("реферальный код изменился: %v → %v", first.RefCode, second.RefCode)
	}
	byCode, err := db.SubjectByRefCode(ctx, *first.RefCode)
	if err != nil || byCode.ID != first.ID {
		t.Fatalf("поиск по коду: %v, %+v", err, byCode)
	}
}

// Привязка Telegram к веб-аккаунту: код одноразовый.
func TestLinkCodesAreOneTime(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 7002)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	code, err := db.CreateLinkCode(ctx, subject.ID)
	if err != nil {
		t.Fatalf("код: %v", err)
	}
	accountID, err := db.PeekLinkCode(ctx, code)
	if err != nil || accountID == nil || *accountID != subject.ID {
		t.Fatalf("код не найден: %v, %v", err, accountID)
	}
	if _, err := db.ConsumeLinkCode(ctx, code); err != nil {
		t.Fatalf("списание кода: %v", err)
	}
	accountID, err = db.PeekLinkCode(ctx, code)
	if err != nil {
		t.Fatalf("поиск кода: %v", err)
	}
	if accountID != nil {
		t.Fatal("использованный код не должен находиться")
	}
}

// Баланс: пополнение, списание и запрет уйти в минус.
func TestBalanceAddAndDeduct(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7003)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}

	balance, err := db.Balance(ctx, subject.ID)
	if err != nil || balance != 0 {
		t.Fatalf("баланс нового субъекта = %s, ждали 0", balance)
	}

	balance, err = db.AddBalance(ctx, subject.ID, domain.FromRubles(500), nil)
	if err != nil || balance != domain.FromRubles(500) {
		t.Fatalf("после пополнения = %s, ждали 500 (err %v)", balance, err)
	}
	balance, err = db.AddBalance(ctx, subject.ID, domain.FromRubles(250), nil)
	if err != nil || balance != domain.FromRubles(750) {
		t.Fatalf("после второго пополнения = %s, ждали 750", balance)
	}

	balance, err = db.DeductBalance(ctx, subject.ID, domain.FromRubles(300))
	if err != nil || balance != domain.FromRubles(450) {
		t.Fatalf("после списания = %s, ждали 450", balance)
	}
	// Списать больше, чем есть, нельзя: баланс не уходит в минус.
	balance, err = db.DeductBalance(ctx, subject.ID, domain.FromRubles(1000))
	if err != nil {
		t.Fatalf("списание: %v", err)
	}
	if balance != 0 {
		t.Fatalf("баланс после списания = %s, ждали 0", balance)
	}
}

// Списание с пустого баланса — понятная ошибка, а не ноль молча.
func TestDeductBalanceWithoutRow(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7004)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if _, err := db.DeductBalance(ctx, subject.ID, domain.FromRubles(10)); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("ждали ErrNotFound, получили %v", err)
	}
}

// Отложенные бонусы за приглашения: начисляются и забираются один раз.
func TestPendingReferralBonus(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7005)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.AddPendingBonus(ctx, subject.ID, 5); err != nil {
		t.Fatalf("начисление: %v", err)
	}
	days, err := db.TakePendingBonus(ctx, subject.ID)
	if err != nil || days != 5 {
		t.Fatalf("забрано дней = %d, ждали 5", days)
	}
	days, err = db.TakePendingBonus(ctx, subject.ID)
	if err != nil || days != 0 {
		t.Fatalf("повторно забрано дней = %d, ждали 0", days)
	}
}

// Режим награды за приглашения: дни или деньги.
func TestReferralMode(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7006)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	mode, err := db.ReferralMode(ctx, subject.ID)
	if err != nil || mode != "" {
		t.Fatalf("режим по умолчанию = %q, ждали пусто", mode)
	}
	if err := db.SetReferralMode(ctx, subject.ID, "balance"); err != nil {
		t.Fatalf("сохранение режима: %v", err)
	}
	mode, err = db.ReferralMode(ctx, subject.ID)
	if err != nil || mode != "balance" {
		t.Fatalf("режим = %q, ждали balance", mode)
	}
}

// Приглашение записывается один раз: повторный /start не должен удваивать.
func TestCreateReferralIsOneTime(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	referrer, err := db.EnsureTelegramSubject(ctx, 7007)
	if err != nil {
		t.Fatalf("пригласивший: %v", err)
	}
	referred, err := db.EnsureTelegramSubject(ctx, 7008)
	if err != nil {
		t.Fatalf("приглашённый: %v", err)
	}
	if err := db.CreateReferral(ctx, referrer.ID, referred.ID); err != nil {
		t.Fatalf("приглашение: %v", err)
	}
	// Повторная запись не должна плодить вторую строку.
	if err := db.CreateReferral(ctx, referrer.ID, referred.ID); err != nil {
		t.Fatalf("повторное приглашение: %v", err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM referrals WHERE referred_subject_id = $1`, referred.ID).Scan(&count); err != nil {
		t.Fatalf("подсчёт: %v", err)
	}
	if count != 1 {
		t.Fatalf("приглашений %d, ждали 1", count)
	}
	referral, err := db.ReferralByReferred(ctx, referred.ID)
	if err != nil || referral == nil {
		t.Fatalf("приглашение не найдено: %v", err)
	}
	stats, err := db.ReferralStatsFor(ctx, referrer.ID)
	if err != nil {
		t.Fatalf("статистика: %v", err)
	}
	if stats.Invited != 1 {
		t.Fatalf("приглашённых = %d, ждали 1", stats.Invited)
	}
}

// Пробный период выдаётся один раз на вид (бот или сайт).
func TestTrialIsIssuedOnce(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7009)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	has, err := db.HasTrial(ctx, subject.ID, "bot")
	if err != nil || has {
		t.Fatalf("пробного периода ещё не было: %v", err)
	}
	fresh, err := db.RecordTrial(ctx, subject.ID, nil, "bot")
	if err != nil || !fresh {
		t.Fatalf("пробный период не выдан: %v", err)
	}
	fresh, err = db.RecordTrial(ctx, subject.ID, nil, "bot")
	if err != nil {
		t.Fatalf("повтор: %v", err)
	}
	if fresh {
		t.Fatal("пробный период нельзя выдать дважды")
	}
	// Для сайта пробный период отдельный.
	fresh, err = db.RecordTrial(ctx, subject.ID, nil, "web")
	if err != nil || !fresh {
		t.Fatalf("пробный период сайта не выдан: %v", err)
	}
}

// Заказ: создание, чтение и переход в оплаченные ровно один раз.
func TestOrderLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(7010)
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	days := 30
	created, err := db.CreateOrder(ctx, domain.Order{
		OrderID:        "ord-1",
		Provider:       "yookassa",
		TelegramID:     &telegramID,
		AccountID:      &subject.ID,
		PlanID:         "30d",
		Kind:           domain.KindSubscription,
		Amount:         domain.FromRubles(300),
		OriginalAmount: domain.FromRubles(300),
		Currency:       "RUB",
		Status:         domain.OrderPending,
		PlanDays:       &days,
	})
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("заказу не присвоен id")
	}

	loaded, err := db.OrderByID(ctx, "ord-1")
	if err != nil {
		t.Fatalf("чтение заказа: %v", err)
	}
	if loaded.Amount != domain.FromRubles(300) {
		t.Fatalf("сумма заказа = %s, ждали 300", loaded.Amount)
	}

	paid, err := db.MarkPaid(ctx, "ord-1", "tx-1", time.Now())
	if err != nil || !paid {
		t.Fatalf("заказ не оплачен: %v", err)
	}
	// Повторный вебхук не должен выдать второй период.
	paid, err = db.MarkPaid(ctx, "ord-1", "tx-2", time.Now())
	if err != nil {
		t.Fatalf("повторная оплата: %v", err)
	}
	if paid {
		t.Fatal("повторная оплата не должна проходить")
	}

	loaded, err = db.OrderByID(ctx, "ord-1")
	if err != nil {
		t.Fatalf("чтение заказа: %v", err)
	}
	if loaded.Status != domain.OrderPaid {
		t.Fatalf("статус = %q, ждали paid", loaded.Status)
	}
	if loaded.TransactionID == nil || *loaded.TransactionID != "tx-1" {
		t.Fatalf("транзакция = %v", loaded.TransactionID)
	}
}

// Несуществующий заказ — понятная ошибка.
func TestOrderByIDMissing(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.OrderByID(context.Background(), "нет-такого"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("ждали ErrNotFound, получили %v", err)
	}
}

// Живая ссылка на оплату: просроченные заказы не подхватываются.
func TestLivePendingOrders(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(7011)
	subject, _ := db.EnsureTelegramSubject(ctx, telegramID)
	expires := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	url := "https://pay.example/link"

	for _, order := range []domain.Order{
		{OrderID: "live-1", Provider: "yookassa", TelegramID: &telegramID, AccountID: &subject.ID,
			PlanID: "30d", Kind: domain.KindSubscription, Amount: domain.FromRubles(300),
			Currency: "RUB", Status: domain.OrderPending, PaymentURL: &url, PaymentExpiresAt: &expires},
		{OrderID: "dead-1", Provider: "yookassa", TelegramID: &telegramID, AccountID: &subject.ID,
			PlanID: "30d", Kind: domain.KindSubscription, Amount: domain.FromRubles(300),
			Currency: "RUB", Status: domain.OrderPending, PaymentURL: &url, PaymentExpiresAt: &past},
	} {
		if _, err := db.CreateOrder(ctx, order); err != nil {
			t.Fatalf("заказ %s: %v", order.OrderID, err)
		}
	}

	orders, err := db.LivePendingOrders(ctx, &telegramID, nil, domain.KindSubscription)
	if err != nil {
		t.Fatalf("живые заказы: %v", err)
	}
	if len(orders) != 1 || orders[0].OrderID != "live-1" {
		t.Fatalf("живые заказы = %+v", orders)
	}
}

// Промокод: создание, счётчик применений и журнал активаций.
func TestPromoLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, _ := db.EnsureTelegramSubject(ctx, 7012)

	promo := domain.PromoCode{
		Code:            "SUMMER",
		DiscountPercent: 10,
		BonusDays:       3,
		MaxUses:         5,
		PerUserLimit:    1,
		IsActive:        true,
	}
	if err := db.CreatePromo(ctx, promo); err != nil {
		t.Fatalf("создание кода: %v", err)
	}
	loaded, err := db.PromoCode(ctx, "SUMMER")
	if err != nil {
		t.Fatalf("чтение кода: %v", err)
	}
	if loaded.DiscountPercent != 10 || loaded.BonusDays != 3 {
		t.Fatalf("код прочитан неверно: %+v", loaded)
	}
	if loaded.UsedCount != 0 {
		t.Fatalf("счётчик применений = %d, ждали 0", loaded.UsedCount)
	}

	if err := db.UsePromo(ctx, "SUMMER"); err != nil {
		t.Fatalf("счётчик: %v", err)
	}
	loaded, _ = db.PromoCode(ctx, "SUMMER")
	if loaded.UsedCount != 1 {
		t.Fatalf("счётчик применений = %d, ждали 1", loaded.UsedCount)
	}

	if err := db.RecordPromoActivation(ctx, subject.ID, "SUMMER"); err != nil {
		t.Fatalf("активация: %v", err)
	}
	count, err := db.PromoActivationsForCode(ctx, subject.ID, "SUMMER")
	if err != nil || count != 1 {
		t.Fatalf("активаций = %d, ждали 1", count)
	}
	distinct, err := db.DistinctPromoCount(ctx, subject.ID)
	if err != nil || distinct != 1 {
		t.Fatalf("разных кодов = %d, ждали 1", distinct)
	}

	if err := db.SetPromoActive(ctx, "SUMMER", false); err != nil {
		t.Fatalf("выключение кода: %v", err)
	}
	loaded, _ = db.PromoCode(ctx, "SUMMER")
	if loaded.IsActive {
		t.Fatal("код должен быть выключен")
	}
}

// Намерение по промокоду живёт между экранами: код не теряется.
func TestPromoIntent(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, _ := db.EnsureTelegramSubject(ctx, 7013)

	if err := db.RememberPromoIntent(ctx, subject.ID, "SUMMER"); err != nil {
		t.Fatalf("запомнить: %v", err)
	}
	code, ok, err := db.PromoIntent(ctx, subject.ID)
	if err != nil || !ok || code != "SUMMER" {
		t.Fatalf("намерение = %q, %v, %v", code, ok, err)
	}
	if err := db.ConsumePromoIntent(ctx, subject.ID); err != nil {
		t.Fatalf("списание: %v", err)
	}
	if _, ok, _ := db.PromoIntent(ctx, subject.ID); ok {
		t.Fatal("намерение не должно сохраняться после списания")
	}
}

// Сотрудники: права и журнал действий.
func TestStaffAndAudit(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	if err := db.AddStaff(ctx, 8001, "owner", "Владелец", 8001); err != nil {
		t.Fatalf("назначение: %v", err)
	}
	member, err := db.StaffMemberByID(ctx, 8001)
	if err != nil || member == nil {
		t.Fatalf("сотрудник не найден: %v", err)
	}
	if member.Role != "owner" {
		t.Fatalf("роль = %q, ждали owner", member.Role)
	}

	staff, err := db.Staff(ctx)
	if err != nil || len(staff) != 1 {
		t.Fatalf("список сотрудников: %v, %+v", err, staff)
	}

	if err := db.LogAction(ctx, 8001, "grant.days", "acct:1", domain.FromRubles(10), "3 дня"); err != nil {
		t.Fatalf("журнал: %v", err)
	}
	audit, err := db.RecentAudit(ctx, 10)
	if err != nil || len(audit) == 0 {
		t.Fatalf("журнал пуст: %v", err)
	}
	if audit[0].Action != "grant.days" {
		t.Fatalf("действие = %q, ждали grant.days", audit[0].Action)
	}

	removed, err := db.RemoveStaff(ctx, 8001)
	if err != nil || !removed {
		t.Fatalf("снятие: %v, %v", removed, err)
	}
	// Сотрудник не удаляется, а деактивируется: история остаётся.
	member, err = db.StaffMemberByID(ctx, 8001)
	if err != nil {
		t.Fatalf("чтение сотрудника: %v", err)
	}
	if member == nil || member.IsActive {
		t.Fatalf("сотрудник должен быть неактивен: %+v", member)
	}
	staff, err = db.Staff(ctx)
	if err != nil {
		t.Fatalf("список сотрудников: %v", err)
	}
	if len(staff) != 0 {
		t.Fatalf("неактивный сотрудник попал в состав: %+v", staff)
	}
}

// Права: владелец из ADMIN_IDS и сотрудник из таблицы staff.
func TestPermission(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	if !db.Permission(ctx, 9001, "support.answer", map[int64]bool{9001: true}) {
		t.Error("владелец из ADMIN_IDS должен иметь права")
	}
	if db.Permission(ctx, 9002, "support.answer", map[int64]bool{9001: true}) {
		t.Error("посторонний не должен иметь прав")
	}

	if err := db.AddStaff(ctx, 9003, "support", "Поддержка", 9001); err != nil {
		t.Fatalf("назначение: %v", err)
	}
	if !db.Permission(ctx, 9003, "support.answer", nil) {
		t.Error("сотрудник поддержки должен иметь права")
	}
}

// Настройки приложения: админка меняет их без рестарта.
func TestAppSettings(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	if err := db.SaveAppSetting(ctx, "BRAND_NAME", "Другой", 9001); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	settings, err := db.AppSettings(ctx)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if settings["BRAND_NAME"] != "Другой" {
		t.Fatalf("настройка = %q, ждали «Другой»", settings["BRAND_NAME"])
	}
	if err := db.DeleteAppSetting(ctx, "BRAND_NAME"); err != nil {
		t.Fatalf("удаление: %v", err)
	}
	settings, _ = db.AppSettings(ctx)
	if _, ok := settings["BRAND_NAME"]; ok {
		t.Fatal("настройка не удалена")
	}
}

// Заявка на вывод: создаётся, видна в списке и может быть решена.
func TestWithdrawalLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(9010)
	subject, _ := db.EnsureTelegramSubject(ctx, telegramID)

	if err := db.CreateWithdrawal(ctx, subject.ID, &telegramID, "user", "card", "1234", domain.FromRubles(500)); err != nil {
		t.Fatalf("заявка: %v", err)
	}
	list, err := db.Withdrawals(ctx, "pending", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("заявки: %v, %+v", err, list)
	}
	if list[0].Amount != domain.FromRubles(500) {
		t.Fatalf("сумма = %s, ждали 500", list[0].Amount)
	}
	if err := db.DecideWithdrawal(ctx, list[0].ID, "paid"); err != nil {
		t.Fatalf("решение: %v", err)
	}
	pending, _ := db.Withdrawals(ctx, "pending", 10)
	if len(pending) != 0 {
		t.Fatalf("решённая заявка осталась в ожидании: %+v", pending)
	}
	paid, _ := db.Withdrawals(ctx, "paid", 10)
	if len(paid) != 1 {
		t.Fatalf("решённых заявок = %d, ждали 1", len(paid))
	}
}
