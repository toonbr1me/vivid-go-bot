package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
)

// InvoiceLinker отдаёт счёты Telegram звёздам. Пакет payments знает только
// узкий интерфейс, поэтому бот не тянет за собой всю платёжную службу.
type InvoiceLinker struct {
	api *telego.Bot
}

func NewInvoiceLinker(api *telego.Bot) *InvoiceLinker { return &InvoiceLinker{api: api} }

func (l *InvoiceLinker) CreateInvoiceLink(ctx context.Context, params *telego.CreateInvoiceLinkParams) (*string, error) {
	return l.api.CreateInvoiceLink(ctx, params)
}

// ---------------------------------------------------------------------------
// Заказ подписки
// ---------------------------------------------------------------------------

func (b *Bot) startCheckout(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, planID, token string, recurring ...bool) {
	withRecurring := len(recurring) > 0 && recurring[0]
	if !withRecurring && b.checkoutRecurring(ctx, tgID, planID) {
		b.answer(ctx, queryID, "Выбор изменился. Откройте тариф ещё раз.", true)
		return
	}
	purchase := payments.Purchase{
		TelegramID: &tgID,
		PlanID:     planID,
		Option:     token,
		PromoCode:  b.currentPromo(tgID),
		Recurring:  withRecurring,
	}
	checkout, err := b.pay.Checkout(ctx, purchase)
	if err != nil {
		b.answer(ctx, queryID, checkoutError(err), true)
		return
	}
	if checkout.URL == "" {
		b.setPromo(tgID, "")
		text, kb := b.checkoutDoneScreen(ctx, tgID, checkout)
		b.upsert(ctx, chatID, messageID, text, kb)
		b.answer(ctx, queryID, "Готово", false)
		return
	}
	b.answer(ctx, queryID, "", false)
	b.upsert(ctx, chatID, messageID, b.paymentLinkText(checkout), b.paymentLinkKeyboard(checkout))
}

func (b *Bot) startRecurringCheckout(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, plan, token string) {
	b.startCheckout(ctx, queryID, chatID, messageID, tgID, plan, token, true)
}

func (b *Bot) startBalanceCheckout(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, planID string) {
	checkout, err := b.pay.PurchaseWithBalance(ctx, payments.Purchase{
		TelegramID: &tgID,
		PlanID:     planID,
		PromoCode:  b.currentPromo(tgID),
	})
	if err != nil {
		b.answer(ctx, queryID, checkoutError(err), true)
		return
	}
	b.setPromo(tgID, "")
	if subject, err := b.subject(ctx, tgID); err == nil {
		_ = b.db.ConsumePromoIntent(ctx, subject.ID)
	}
	text, kb := b.checkoutDoneScreen(ctx, tgID, checkout)
	b.upsert(ctx, chatID, messageID, text, kb)
	b.answer(ctx, queryID, "Оплачено", false)
}

func (b *Bot) checkoutDoneScreen(ctx context.Context, tgID int64, checkout domain.Checkout) (string, *telego.InlineKeyboardMarkup) {
	if order, _ := b.pay.CheckOrder(ctx, checkout.OrderID); order.OrderID != "" {
		return b.orderScreen(ctx, tgID, order)
	}
	return "<b>Проверяем заказ</b>\n\nНе удалось получить подтверждённый статус. Откройте историю покупок и повторите проверку.", keyboard([]telego.InlineKeyboardButton{button("История покупок", "nav:history"), button("Поддержка", "nav:support")})
}

func (b *Bot) paymentLinkText(checkout domain.Checkout) string {
	lines := []string{b.texts.Payment.Created, ""}
	if checkout.Amount > 0 {
		lines = append(lines, b.text(b.texts.Payment.Amount, map[string]any{
			"Amount": priceText(checkout.Amount, ""),
		}))
	}
	if checkout.Stars > 0 {
		lines = append(lines, b.text(b.texts.Payment.Stars, map[string]any{"Stars": checkout.Stars}))
	}
	lines = append(lines, b.text(b.texts.Payment.Order, map[string]any{"Order": escapeHTML(checkout.OrderID)}))
	if checkout.ExpiresAt != nil {
		lines = append(lines, b.text(b.texts.Payment.Expires, map[string]any{"Date": dateTimeText(*checkout.ExpiresAt)}))
	}
	if checkout.Reused {
		lines = append(lines, "", b.texts.Payment.Reused)
	}
	lines = append(lines, "", b.texts.Payment.Hint)
	return strings.Join(lines, "\n")
}

func (b *Bot) paymentLinkKeyboard(checkout domain.Checkout) *telego.InlineKeyboardMarkup {
	payButton := urlButton(b.texts.Buttons.Pay, checkout.URL)
	payButton.Style = "success"
	rows := [][]telego.InlineKeyboardButton{
		{payButton},
		{button(b.texts.Buttons.CheckPayment+" · "+shortID(checkout.OrderID), "check:"+checkout.OrderID)},
	}
	rows = append(rows, backRow())
	return keyboard(rows...)
}

// checkOrder — кнопка «Я оплатил». Мы не ждём вебхук, а спрашиваем базу:
// платёж мог прийти раньше, чем человек вернулся в бот.
func (b *Bot) checkOrder(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, orderID string) {
	order, err := b.pay.Order(ctx, orderID)
	if err != nil {
		b.answer(ctx, queryID, b.texts.Payment.NotYet, true)
		return
	}
	if order.TelegramID == nil || *order.TelegramID != tgID {
		b.answer(ctx, queryID, b.texts.Common.Empty, true)
		return
	}
	updated, checkErr := b.pay.CheckOrder(ctx, orderID)
	if updated.OrderID != "" {
		order = updated
	}
	if checkErr != nil {
		// A failed refresh cannot erase a confirmed payment.
		if saved, e := b.pay.Order(ctx, orderID); e == nil {
			order = saved
		}
	}
	if order.Status == domain.OrderProvisioned {
		b.setPromo(tgID, "")
		if subject, e := b.subject(ctx, tgID); e == nil {
			_ = b.db.ConsumePromoIntent(ctx, subject.ID)
		}
	}
	text, kb := b.orderScreen(ctx, tgID, order)
	if checkErr != nil {
		text += "\n\nНе удалось обновить статус. Показаны последние подтверждённые данные."
	}
	b.upsert(ctx, chatID, messageID, text, kb)
	b.answer(ctx, queryID, order.Progress(time.Now()).Label, false)
}

func (b *Bot) orderScreen(ctx context.Context, tgID int64, order domain.Order) (string, *telego.InlineKeyboardMarkup) {
	if order.Status == domain.OrderProvisioned {
		return b.provisionedScreen(ctx, tgID, order)
	}
	p := order.Progress(time.Now())
	text := "<b>" + escapeHTML(p.Label) + "</b>\n\n" + escapeHTML(p.Detail) + "\n\nЗаказ: <code>" + escapeHTML(order.OrderID) + "</code>"
	rows := [][]telego.InlineKeyboardButton{}
	if p.CanPay {
		rows = append(rows, []telego.InlineKeyboardButton{urlButton("Оплатить", *order.PaymentURL)})
	}
	rows = append(rows, []telego.InlineKeyboardButton{button("Проверить статус", "check:"+order.OrderID)}, []telego.InlineKeyboardButton{button("Поддержка по заказу", "support:order:"+order.OrderID)}, []telego.InlineKeyboardButton{button("История покупок", "nav:history"), button("Меню", "nav:main")})
	return text, keyboard(rows...)
}

func (b *Bot) provisionedScreen(ctx context.Context, tgID int64, order domain.Order) (string, *telego.InlineKeyboardMarkup) {
	if order.Kind == domain.KindBalance {
		return "<b>Баланс пополнен</b>\n\nЗаказ: <code>" + escapeHTML(order.OrderID) + "</code>\nСумма: " + priceText(order.Amount, order.Currency), keyboard([]telego.InlineKeyboardButton{button("Аккаунт", "nav:profile"), button("Меню", "nav:main")})
	}
	lines := []string{"✅ <b>Оплата получена — доступ выдан</b>", ""}
	lines = append(lines, "Заказ: <code>"+escapeHTML(order.OrderID)+"</code>")
	if order.Kind == domain.KindDevices {
		if order.DeviceCount != nil {
			lines = append(lines, fmt.Sprintf("Устройства: <b>+%d</b>", *order.DeviceCount))
		}
		if order.DeviceExpiresAt != nil {
			lines = append(lines, "Слоты действуют до <b>"+dateText(*order.DeviceExpiresAt)+"</b>.")
		}
		return strings.Join(lines, "\n"), keyboard(
			[]telego.InlineKeyboardButton{
				button("📱 Устройства", "nav:devices"),
				button("🏠 Меню", "nav:main"),
			})
	}
	if order.ExpireAt != nil {
		lines = append(lines, b.text(b.texts.Subscription.Until, map[string]any{
			"Date": dateText(*order.ExpireAt),
		}))
	}
	if order.GrantedDays != nil {
		lines = append(lines, fmt.Sprintf("Начислено: <b>%d %s</b>", *order.GrantedDays, daysWord(*order.GrantedDays)))
	}
	lines = append(lines, "", "Ссылка на подключение — в «Моей подписке».")
	return strings.Join(lines, "\n"), keyboard(
		[]telego.InlineKeyboardButton{
			button("Подключиться", "nav:connect"),
			button("🏠 Меню", "nav:main"),
		})
}

func checkoutError(err error) string {
	switch {
	case errors.Is(err, payments.ErrNoProviders):
		return "Платежи временно недоступны — попробуйте позже."
	case errors.Is(err, payments.ErrPromoRejected):
		return err.Error()
	case errors.Is(err, payments.ErrPlanUnavailable):
		return "Этот тариф снят с продажи."
	case errors.Is(err, payments.ErrInsufficientBalance):
		return "На балансе недостаточно средств."
	case errors.Is(err, payments.ErrTrialUsed):
		return "Пробный период уже использован."
	case errors.Is(err, payments.ErrTrialWhileActive):
		return "Подписка ещё действует — пробный период не положен."
	default:
		return "Не удалось создать счёт: " + err.Error()
	}
}

// ---------------------------------------------------------------------------
// Устройства
// ---------------------------------------------------------------------------

func (b *Bot) startDeviceCheckout(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, packID, grantOrderID, token string) {
	purchase := payments.DevicePurchase{
		TelegramID:   &tgID,
		PackID:       packID,
		GrantOrderID: grantOrderID,
		Option:       token,
	}
	checkout, err := b.pay.CheckoutDevices(ctx, purchase)
	if err != nil {
		b.answer(ctx, queryID, deviceError(err), true)
		return
	}
	b.answer(ctx, queryID, "", false)
	b.upsert(ctx, chatID, messageID, b.paymentLinkText(checkout), b.paymentLinkKeyboard(checkout))
}

func deviceError(err error) string {
	switch {
	case errors.Is(err, payments.ErrDevicesDisabled):
		return "Докупка устройств выключена."
	case errors.Is(err, payments.ErrDeviceTermTooShort):
		return "Докупить устройства можно при подписке сроком больше суток."
	case errors.Is(err, payments.ErrNoSubscription):
		return "Сначала нужна действующая подписка."
	case errors.Is(err, payments.ErrUnknownDevicePack), errors.Is(err, payments.ErrPackUnavailable):
		return "Этот пакет больше не продаётся."
	case errors.Is(err, payments.ErrGrantNotRenewable), errors.Is(err, payments.ErrGrantNotOwned):
		return "Этот слот нельзя продлить."
	case errors.Is(err, payments.ErrNoProviders):
		return "Платежи временно недоступны."
	default:
		return "Не удалось создать счёт: " + err.Error()
	}
}

// ---------------------------------------------------------------------------
// Telegram Stars
// ---------------------------------------------------------------------------

func (b *Bot) handlePreCheckout(ctx *telegohandler.Context, update telego.Update) error {
	query := update.PreCheckoutQuery
	if query == nil {
		return nil
	}
	params := &telego.AnswerPreCheckoutQueryParams{PreCheckoutQueryID: query.ID, Ok: true}
	if _, _, ok := payments.ParseStarsPayload(query.InvoicePayload); !ok {
		params.Ok = false
		params.ErrorMessage = "Счёт устарел — создайте новый в меню бота."
	}
	if err := b.api.AnswerPreCheckoutQuery(ctx, params); err != nil {
		return fmt.Errorf("ответ на pre_checkout: %w", err)
	}
	return nil
}

func (b *Bot) handleSuccessfulPayment(ctx *telegohandler.Context, message telego.Message) error {
	payment := message.SuccessfulPayment
	if payment == nil {
		return nil
	}
	orderID, stars, ok := payments.ParseStarsPayload(payment.InvoicePayload)
	if !ok {
		return nil
	}
	// Счёт мог быть оплачен по другой цене — тогда это не наш заказ.
	if order, err := b.pay.Order(ctx, orderID); err == nil {
		if expected := b.pay.StarsAmount(order.Amount); expected != stars {
			slog.Warn("звёзды: сумма не совпала с заказом",
				"order", orderID, "stars", stars, "expected", expected)
			return nil
		}
	}
	paidAt := time.Unix(message.Date, 0)
	paid, err := b.pay.ReceiveEvent(ctx, "stars", payments.Event{
		OrderID:       orderID,
		Status:        payments.StatusPaid,
		TransactionID: payment.TelegramPaymentChargeID,
		PaidAt:        &paidAt,
	})
	if err != nil {
		return fmt.Errorf("оплата звёздами %s: %w", orderID, err)
	}
	if !paid {
		return nil
	}
	tgID := message.From.ID
	b.setPromo(tgID, "")
	if subject, err := b.subject(ctx, tgID); err == nil {
		_ = b.db.ConsumePromoIntent(ctx, subject.ID)
	}
	order, err := b.pay.CheckOrder(ctx, orderID)
	if err != nil {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID},
			b.texts.Payment.Pending,
			keyboard([]telego.InlineKeyboardButton{button("📱 Моя подписка", "nav:subscription")}))
		return nil
	}
	// Платёж мог прийти от другого человека: доступ получит владелец
	// заказа, а отчёт о сроке — плательщик.
	if order.TelegramID == nil || *order.TelegramID != tgID {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID},
			"⭐ Оплата получена. Доступ выдаётся владельцу заказа.",
			keyboard([]telego.InlineKeyboardButton{button("🏠 Меню", "nav:main")}))
		return nil
	}
	text, kb := b.provisionedScreen(ctx, tgID, order)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}
