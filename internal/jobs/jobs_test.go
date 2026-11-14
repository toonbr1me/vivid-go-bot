package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

// captureSender запоминает отправленные уведомления.
type captureSender struct {
	sent []string
	err  error
}

func (s *captureSender) SendText(_ context.Context, telegramID int64, text string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, text)
	return nil
}

// provisionedOrder — заказ с выданной подпиской, срок которой скоро кончится.
func provisionedOrder(t *testing.T, db *storage.DB, telegramID int64, expireAt time.Time) {
	t.Helper()
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	days := 30
	orderID := fmt.Sprintf("exp-%d-%d", telegramID, time.Now().UnixNano())
	_, err = db.CreateOrder(ctx, domain.Order{
		OrderID:        orderID,
		Provider:       "yookassa",
		TelegramID:     &telegramID,
		AccountID:      &subject.ID,
		PlanID:         "30d",
		Kind:           domain.KindSubscription,
		Amount:         domain.FromRubles(300),
		OriginalAmount: domain.FromRubles(300),
		Currency:       "RUB",
		Status:         domain.OrderProvisioned,
		PlanDays:       &days,
	})
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	if err := db.MarkProvisioned(ctx, orderID, &expireAt, &days); err != nil {
		t.Fatalf("выдача: %v", err)
	}
}

// Напоминание об истечении уходит один раз: повторный запуск задачи не
// должен снова писать человеку.
func TestRemindExpirySendsOnce(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(9101)
	expire := time.Now().UTC().Add(6 * time.Hour)
	provisionedOrder(t, db, telegramID, expire)

	sender := &captureSender{}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatalf("напоминание: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("уведомлений %d, ждали 1", len(sender.sent))
	}
	if !strings.Contains(sender.sent[0], "Подписка заканчивается") {
		t.Fatalf("текст уведомления: %q", sender.sent[0])
	}
	if !strings.Contains(sender.sent[0], expire.In(time.Local).Format("02.01.2006")) {
		t.Fatalf("в уведомлении нет даты: %q", sender.sent[0])
	}

	// Повторный запуск в тот же день — молчит.
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatalf("повторное напоминание: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("повторно отправлено %d уведомлений, ждали 1", len(sender.sent))
	}
}

// Подписка, которая кончится не скоро, не должна тревожить заранее.
func TestRemindExpiryIgnoresFarExpiry(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(9102)
	provisionedOrder(t, db, telegramID, time.Now().UTC().Add(20*24*time.Hour))

	sender := &captureSender{}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatalf("напоминание: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("уведомлений %d, ждали 0", len(sender.sent))
	}
}

// Если человек выключил уведомления — молчим.
func TestRemindExpiryRespectsSettings(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(9103)
	provisionedOrder(t, db, telegramID, time.Now().UTC().Add(3*time.Hour))

	if err := db.SaveNotificationSettings(ctx, telegramID, storage.NotificationSettings{Expiry: false}); err != nil {
		t.Fatalf("настройки уведомлений: %v", err)
	}

	sender := &captureSender{}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatalf("напоминание: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("уведомлений %d, ждали 0", len(sender.sent))
	}
}

// Неоплаченный счёт: напоминание приходит и не дублируется.
func TestRemindUnpaidSendsOnce(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(9104)
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	paymentURL := "https://pay.example/link"
	created := time.Now().UTC().Add(-48 * time.Hour)
	order := domain.Order{
		OrderID:        "unpaid-1",
		Provider:       "yookassa",
		TelegramID:     &telegramID,
		AccountID:      &subject.ID,
		PlanID:         "30d",
		Kind:           domain.KindSubscription,
		Amount:         domain.FromRubles(300),
		OriginalAmount: domain.FromRubles(300),
		Currency:       "RUB",
		Status:         domain.OrderPending,
		PaymentURL:     &paymentURL,
	}
	if _, err := db.CreateOrder(ctx, order); err != nil {
		t.Fatalf("заказ: %v", err)
	}
	// Заказ должен быть старше порога.
	if _, err := db.Pool.Exec(ctx,
		`UPDATE orders SET created_at = $2 WHERE order_id = $1`, order.OrderID, created); err != nil {
		t.Fatalf("возраст заказа: %v", err)
	}

	cfg := &config.Settings{RemindUnpaidHours: 24}
	sender := &captureSender{}
	if err := remindUnpaid(ctx, db, sender, cfg); err != nil {
		t.Fatalf("напоминание: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("уведомлений %d, ждали 1", len(sender.sent))
	}
	if err := remindUnpaid(ctx, db, sender, cfg); err != nil {
		t.Fatalf("повтор: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("повторно отправлено %d уведомлений, ждали 1", len(sender.sent))
	}
}

// Напоминание о счёте выключено настройкой — молчим.
func TestRemindUnpaidDisabled(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	sender := &captureSender{}
	if err := remindUnpaid(ctx, db, sender, &config.Settings{RemindUnpaidHours: 0}); err != nil {
		t.Fatalf("напоминание: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("уведомлений %d, ждали 0", len(sender.sent))
	}
}

// Сбой доставки не должен ронять задачу: ошибка логируется, цикл идёт дальше.
func TestReminderSurvivesDeliveryError(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	telegramID := int64(9105)
	provisionedOrder(t, db, telegramID, time.Now().UTC().Add(2*time.Hour))

	sender := &captureSender{err: errors.New("telegram недоступен")}
	if err := remindExpiry(ctx, db, sender); err != nil {
		t.Fatalf("задача должна пережить сбой доставки: %v", err)
	}
}
