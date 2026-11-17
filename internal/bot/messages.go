package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// ---------------------------------------------------------------------------
// Команды
// ---------------------------------------------------------------------------

func (b *Bot) handleMenuCommand(ctx *telegohandler.Context, message telego.Message) error {
	if message.From != nil {
		if b.isSupportBot() {
			b.showSupportHome(ctx, telego.ChatID{ID: message.Chat.ID}, message.From.ID)
			return nil
		}
		b.showCommandScreen(ctx, telego.ChatID{ID: message.Chat.ID}, "main", message.From.ID)
	}
	return nil
}

func (b *Bot) handleStart(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	if b.isSupportBot() {
		b.showSupportHome(ctx, telego.ChatID{ID: message.Chat.ID}, tgID)
		return nil
	}
	if code, ok := deepLinkArg(message.Text, "gift_"); ok && message.Chat.ID == tgID {
		b.claimGift(ctx, telego.ChatID{ID: tgID}, tgID, code)
		return nil
	}
	b.claimReferral(ctx, tgID, message.Text)
	chatID := telego.ChatID{ID: message.Chat.ID}
	if chatID.ID == tgID && b.cfg.Current().BotStyle == "" {
		if err := b.showViewSelector(ctx, tgID); err != nil {
			slog.Warn("выбор вида бота", "err", err)
		}
	}
	if code, ok := deepLinkArg(message.Text, "link_"); ok {
		b.attachWebAccount(ctx, chatID, tgID, code)
		return nil
	}
	b.showCommandScreen(ctx, chatID, "main", tgID)
	return nil
}

// deepLinkArg вынимает хвост команды /start с известным префиксом.
func deepLinkArg(text, prefix string) (string, bool) {
	fields := strings.Fields(text)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], prefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(fields[1], prefix)), true
}

// handleWebLoginCommand выдаёт код для входа на сайт. Это тот же профиль:
// подписка, история и поддержка остаются общими с ботом.
func (b *Bot) handleWebLoginCommand(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	chatID := telego.ChatID{ID: message.Chat.ID}
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		b.send(ctx, chatID, b.texts.Common.ProfileError, keyboard(b.backRow()))
		return nil
	}
	code, err := b.db.CreateLinkCode(ctx, subject.ID)
	if err != nil {
		b.send(ctx, chatID, b.texts.Session.CodeFailed, keyboard(b.backRow()))
		return nil
	}
	text := b.texts.Session.CodeTitle + "\n\n" +
		"<code>" + code + "</code>\n\n" +
		b.texts.Session.CodeBody
	var kb *telego.InlineKeyboardMarkup
	if site := b.siteURL(); site != "" {
		kb = keyboard([]telego.InlineKeyboardButton{
			urlButton(b.texts.Session.OpenSite, site+"/login?code="+code),
		})
	}
	b.send(ctx, chatID, text, kb)
	return nil
}

// attachWebAccount привязывает Telegram к веб-аккаунту по коду с сайта.
// Слияние двух уже существующих профилей здесь не делается: это потеря
// истории заказов, а не удобство.
func (b *Bot) attachWebAccount(ctx context.Context, chatID telego.ChatID, tgID int64, code string) {
	if code == "" {
		b.send(ctx, chatID, b.texts.Session.EmptyCode, keyboard(b.backRow()))
		return
	}
	accountID, err := b.db.PeekLinkCode(ctx, code)
	if err != nil {
		b.send(ctx, chatID, b.texts.Session.CodeCheckFailed, keyboard(b.backRow()))
		return
	}
	if accountID == nil {
		b.send(ctx, chatID, b.texts.Session.CodeNotFound, keyboard(b.backRow()))
		return
	}
	subject, err := b.db.SubjectByID(ctx, *accountID)
	if err != nil {
		b.send(ctx, chatID, b.texts.Session.AccountNotFound, keyboard(b.backRow()))
		return
	}
	if subject.TelegramID != nil {
		if *subject.TelegramID == tgID {
			b.send(ctx, chatID, b.texts.Session.AlreadyLinked, keyboard(b.backRow()))
		} else {
			b.send(ctx, chatID, b.texts.Session.OtherTelegram, keyboard(b.backRow()))
		}
		return
	}
	current, err := b.db.SubjectByTelegramID(ctx, tgID)
	switch {
	case err == nil && current.ID != subject.ID:
		b.send(ctx, chatID, b.texts.Session.AnotherProfile+"\n\nПоддержка поможет проверить оба профиля и сохранить оплаченные подписки и историю.", keyboard([]telego.InlineKeyboardButton{button("Помощь с объединением профилей", "support:new")}, b.backRow()))
		return
	case err != nil && !errors.Is(err, storage.ErrNotFound):
		b.send(ctx, chatID, b.texts.Session.TelegramFailed, keyboard(b.backRow()))
		return
	}
	if err := b.db.AttachTelegramByCode(ctx, code, subject.ID, tgID); err != nil {
		b.send(ctx, chatID, b.texts.Session.AttachFailed, keyboard(b.backRow()))
		return
	}

	site := b.siteURL()
	if site == "" {
		site = b.cfg.Current().PublicBaseURL
	}
	var kb *telego.InlineKeyboardMarkup
	if site != "" {
		kb = keyboard([]telego.InlineKeyboardButton{
			urlButton(b.texts.Session.OpenAccount, site+"/account"),
		})
	}
	b.send(ctx, chatID, b.texts.Session.LinkedTitle+"\n\n"+b.texts.Session.LinkedBody, kb)
}

// claimReferral записывает пригласившего по deep link /start ref_код.
// Повторный переход и приглашение самого себя ничего не меняют.
func (b *Bot) claimReferral(ctx context.Context, tgID int64, command string) {
	fields := strings.Fields(command)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "ref_") {
		return
	}
	code := strings.TrimPrefix(fields[1], "ref_")
	if code == "" {
		return
	}
	referrer, err := b.db.SubjectByRefCode(ctx, code)
	if err != nil || referrer.ID == 0 {
		return
	}
	subject, err := b.subject(ctx, tgID)
	if err != nil || subject.ID == referrer.ID {
		return
	}
	if existing, err := b.db.ReferralByReferred(ctx, subject.ID); err != nil || existing != nil {
		return
	}
	_ = b.db.CreateReferral(ctx, referrer.ID, subject.ID)
}

func (b *Bot) handleBuyCommand(ctx *telegohandler.Context, message telego.Message) error {
	b.showCommandScreen(ctx, telego.ChatID{ID: message.Chat.ID}, "buy", message.From.ID)
	return nil
}

func (b *Bot) handleTrialCommand(ctx *telegohandler.Context, message telego.Message) error {
	text, kb := b.trialAction(ctx, message.From.ID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

func (b *Bot) handleSubscriptionCommand(ctx *telegohandler.Context, message telego.Message) error {
	b.showCommandScreen(ctx, telego.ChatID{ID: message.Chat.ID}, "subscription", message.From.ID)
	return nil
}

func (b *Bot) handleDevicesCommand(ctx *telegohandler.Context, message telego.Message) error {
	b.showCommandScreen(ctx, telego.ChatID{ID: message.Chat.ID}, "devices", message.From.ID)
	return nil
}

func (b *Bot) handlePromoCommand(ctx *telegohandler.Context, message telego.Message) error {
	text, kb := b.promoScreen(ctx, message.From.ID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

func (b *Bot) handleReferralCommand(ctx *telegohandler.Context, message telego.Message) error {
	text, kb := b.referralScreen(ctx, message.From.ID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

func (b *Bot) handleSupportCommand(ctx *telegohandler.Context, message telego.Message) error {
	if !b.isSupportBot() && b.cfg.Current().SupportBotEnabled && b.cfg.Current().SupportBotUsername != "" {
		b.clearInput(message.From.ID)
		b.send(ctx, telego.ChatID{ID: message.Chat.ID},
			"<b>Поддержка</b>\n\nДля обращений используйте отдельный бот: там сохраняется история, фотографии и ответы сотрудников.",
			keyboard([]telego.InlineKeyboardButton{urlButton("Открыть поддержку", "https://t.me/"+b.cfg.Current().SupportBotUsername)}))
		return nil
	}
	b.clearInput(message.From.ID)
	text, kb := b.supportScreen(ctx, message.From.ID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

func (b *Bot) handleInfoCommand(ctx *telegohandler.Context, message telego.Message) error {
	text, kb := b.infoScreen(ctx, message.From.ID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

func (b *Bot) handleHelpCommand(ctx *telegohandler.Context, message telego.Message) error {
	text, kb := b.helpScreen(ctx, message.From.ID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

func (b *Bot) handlePaySupportCommand(ctx *telegohandler.Context, message telego.Message) error {
	text := "💳 <b>Оплата и возврат</b>\n\n" +
		"Оплата проходит на стороне платёжного шлюза, бот не хранит данные карты.\n\n" +
		"Если доступ не выдался после оплаты — откройте «Историю покупок», нажмите «Проверить оплату», " +
		"а если не помогло, напишите в поддержку: приложите номер заказа и время платежа."
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, keyboard(
		[]telego.InlineKeyboardButton{
			button("📋 История покупок", "nav:history"),
			button("✉️ Поддержка", "nav:support"),
		}))
	return nil
}

func (b *Bot) handleCancelCommand(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	b.clearInput(tgID)
	if b.isSupportBot() {
		b.showSupportHome(ctx, telego.ChatID{ID: message.Chat.ID}, tgID)
		return nil
	}
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, "Отменено.",
		keyboard([]telego.InlineKeyboardButton{button("🏠 Меню", "nav:main")}))
	return nil
}

// ---------------------------------------------------------------------------
// Свободный текст
// ---------------------------------------------------------------------------

func (b *Bot) handleText(ctx *telegohandler.Context, message telego.Message) error {
	if message.From == nil {
		return nil
	}
	tgID := message.From.ID
	chatID := telego.ChatID{ID: message.Chat.ID}
	ctx = ctx.WithContext(context.WithValue(ctx, incomingMessageKey{}, message))
	kind, ref := b.input(tgID)
	text := strings.TrimSpace(message.Text)

	switch kind {
	case "staff_note":
		if b.db.Permission(ctx, tgID, "tickets.note", b.cfg.Current().AdminIDs) {
			id, _ := strconv.ParseInt(ref, 10, 64)
			if err := b.db.StaffTicketAction(ctx, id, tgID, "note", text, 0); err != nil {
				b.send(ctx, chatID, escapeHTML(err.Error()), keyboard(b.backRow()))
				return nil
			}
			b.clearInput(tgID)
			b.showSavedTicket(ctx, tgID, id)
		}
	case inputBroadcast:
		b.handleBroadcastInput(ctx, chatID, tgID, ref, broadcastMarkdown(message.Text, message.Entities))
	case inputPromo:
		b.clearInput(tgID)
		ctx = ctx.WithContext(context.WithValue(ctx, promoPlanKey{}, ref))
		b.handlePromoInput(ctx, chatID, tgID, text)
	case inputTicket:
		ctx = ctx.WithContext(context.WithValue(ctx, ticketRefKey{}, ref))
		b.handleTicketInput(ctx, chatID, tgID, text)
	case inputTicketReply:
		b.handleTicketReply(ctx, chatID, tgID, ref, text)
	case inputPollQuestion, inputPollOptions:
		b.handlePollInput(ctx, chatID, tgID, kind, ref, text)
	default:
		if b.isSupportBot() {
			b.showSupportHome(ctx, chatID, tgID)
			return nil
		}
		// Свободный текст вне диалога — не ошибка: показываем меню и
		// подсказываем, где искать нужное.
		kb := keyboard(
			[]telego.InlineKeyboardButton{
				button("🏠 Меню", "nav:main"),
				button("✉️ Поддержка", "nav:support"),
			})
		b.send(ctx, chatID, b.texts.Common.NotUnderstood, kb)
	}
	return nil
}

func (b *Bot) showSupportHome(ctx context.Context, chatID telego.ChatID, tgID int64) {
	b.clearInput(tgID)
	text, kb := b.supportScreen(ctx, tgID)
	b.send(ctx, chatID, text, kb)
}

func (b *Bot) handlePromoInput(ctx context.Context, chatID telego.ChatID, tgID int64, raw string) {
	code, ok := domain.NormalizePromoCode(raw)
	if !ok {
		if plan, _ := ctx.Value(promoPlanKey{}).(string); plan != "" {
			b.setInput(tgID, inputPromo, plan)
		}
		b.send(ctx, chatID, b.texts.Promo.BadFormat,
			keyboard([]telego.InlineKeyboardButton{button(b.texts.Buttons.TryPromo, "promo:enter")}))
		return
	}
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		b.send(ctx, chatID, b.texts.Common.ProfileError, keyboard(b.backRow()))
		return
	}

	// Подарочный код сразу добавляет дни и не ждёт оплаты.
	if _, err := b.db.GiftPromoByCode(ctx, code); err == nil {
		days, err := b.pay.RedeemGiftPromo(ctx, subject, code)
		if err != nil {
			b.send(ctx, chatID, b.text(b.texts.Promo.Rejected, map[string]any{"Reason": escapeHTML(promoError(err))}),
				keyboard([]telego.InlineKeyboardButton{button(b.texts.Buttons.TryPromo, "promo:enter")}))
			return
		}
		text, kb := b.subscriptionScreen(ctx, tgID)
		b.send(ctx, chatID, b.text(b.texts.Promo.Gift, map[string]any{
			"Days": days, "DaysWord": daysWord(days),
		})+"\n\n"+text, kb)
		return
	}

	quotes, err := b.pay.QuotePlans(ctx, subject.ID, code)
	if err != nil {
		b.send(ctx, chatID, b.texts.Common.TryLater, keyboard(b.backRow()))
		return
	}
	accepted := false
	rejection := ""
	for _, quote := range quotes {
		if quote.Rejection == "" {
			accepted = true
			break
		}
		if rejection == "" {
			rejection = quote.Rejection
		}
	}
	if !accepted {
		if rejection == "" {
			rejection = b.texts.Promo.NoFit
		}
		b.send(ctx, chatID, b.text(b.texts.Promo.Rejected, map[string]any{"Reason": escapeHTML(rejection)}),
			keyboard([]telego.InlineKeyboardButton{button(b.texts.Buttons.TryPromo, "promo:enter")}))
		return
	}

	b.cleanIncoming(ctx)
	b.setPromo(tgID, code)
	if err := b.db.RememberPromoIntent(ctx, subject.ID, code); err != nil {
		// Не страшно: код уже в памяти сеанса, оплата его применит.
		_ = err
	}
	if plan, _ := ctx.Value(promoPlanKey{}).(string); plan != "" {
		b.showCheckout(ctx, tgID, chatID.ID, b.currentCardID(ctx, tgID), plan)
		return
	}
	text, kb := b.buyScreen(ctx, tgID)
	b.send(ctx, chatID, b.text(b.texts.Promo.Entered, map[string]any{"Promo": escapeHTML(code)})+"\n\n"+text, kb)
}

func promoError(err error) string {
	switch {
	case errors.Is(err, payments.ErrPromoRejected):
		return strings.TrimPrefix(err.Error(), payments.ErrPromoRejected.Error()+": ")
	default:
		return err.Error()
	}
}

func (b *Bot) handleTicketInput(ctx context.Context, chatID telego.ChatID, tgID int64, text string) {
	fileID, fileType := supportAttachment(ctx)
	if text == "" && fileID == "" {
		b.send(ctx, chatID, b.texts.Ticket.Empty,
			keyboard([]telego.InlineKeyboardButton{button(b.texts.Buttons.Support, "nav:support")}))
		return
	}
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		b.send(ctx, chatID, b.texts.Common.ProfileError, keyboard(b.backRow()))
		return
	}
	ref, _ := ctx.Value(ticketRefKey{}).(string)
	var open *storage.Ticket
	contextText := ""
	if strings.HasPrefix(ref, "order:") {
		order, e := b.pay.Order(ctx, strings.TrimPrefix(ref, "order:"))
		if e != nil || order.TelegramID == nil || *order.TelegramID != tgID {
			b.send(ctx, chatID, "Заказ не найден.", keyboard(b.backRow()))
			return
		}
		contextText = "Заказ: " + order.OrderID
	} else if strings.HasPrefix(ref, "setup:") {
		parts := strings.Split(strings.TrimPrefix(ref, "setup:"), ",")
		if supportApps[parts[0]] == "" {
			b.send(ctx, chatID, "Выберите приложение заново.", keyboard(b.backRow()))
			return
		}
		contextText = "Приложение: " + supportApps[parts[0]]
		if len(parts) > 1 && supportDevices[parts[1]] != "" {
			contextText += "\nУстройство: " + supportDevices[parts[1]]
		}
	} else if ref != "" {
		ticketID, e := strconv.ParseInt(ref, 10, 64)
		if e != nil {
			b.send(ctx, chatID, "Обращение не найдено.", keyboard(b.backRow()))
			return
		}
		ticket, e := b.db.TicketByID(ctx, ticketID)
		if e != nil || !supportTicketOwned(ticket, subject) || ticket.Status != "open" {
			b.send(ctx, chatID, "Обращение закрыто или недоступно. Откройте историю обращений.", keyboard(b.backRow()))
			return
		}
		open = &ticket
	}
	if open != nil {
		if _, err := b.db.AddOwnerTicketMessageFromBot(ctx, open.ID, subject.ID, &tgID, text, fileID, fileType, b.attachmentBot()); err != nil {
			b.send(ctx, chatID, b.texts.Ticket.SendFailed, keyboard(b.backRow()))
			return
		}
		b.setInput(tgID, inputTicket, strconv.FormatInt(open.ID, 10))
		b.notifyStaff(ctx, *open, text)
		b.ticketSent(ctx, tgID, open.ID, false)
		return
	}
	title := firstRunes(text, 60)
	if title == "" {
		title = "Вложение"
	}
	ticket, err := b.db.CreateTicketContent(ctx, subject.ID, &tgID, title, text, b.ticketMode(), contextText, fileID, fileType)
	if err != nil {
		b.send(ctx, chatID, b.texts.Ticket.CreateFail, keyboard(b.backRow()))
		return
	}
	b.setInput(tgID, inputTicket, strconv.FormatInt(ticket.ID, 10))
	b.notifyStaff(ctx, ticket, text)
	b.ticketSent(ctx, tgID, ticket.ID, true)
}

// notifyStaff пересылает новое обращение дежурным: иначе оно ждёт, пока
// кто-нибудь откроет очередь.
func (b *Bot) notifyStaff(ctx context.Context, ticket storage.Ticket, text string) {
	note := fmt.Sprintf("<b>Сообщение клиента · #%d</b>\n\n%s\n\n%s", ticket.ID,
		b.ticketCustomerDetails(ctx, ticket), escapeHTML(firstRunes(text, 1200)))
	kb := keyboard([]telego.InlineKeyboardButton{
		button("Открыть", "ticket:"+strconv.FormatInt(ticket.ID, 10)),
	})
	for adminID := range b.cfg.Current().AdminIDs {
		b.send(ctx, telego.ChatID{ID: adminID}, note, kb)
	}
}

func (b *Bot) handleTicketReply(ctx context.Context, chatID telego.ChatID, tgID int64, rawID, text string) {
	if !b.isStaff(ctx, tgID) {
		b.send(ctx, chatID, b.texts.Ticket.NoRights, keyboard(b.backRow()))
		return
	}
	ticketID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		b.send(ctx, chatID, b.texts.Ticket.NotFound, keyboard(b.backRow()))
		return
	}
	ticket, err := b.db.TicketByID(ctx, ticketID)
	if err != nil {
		b.send(ctx, chatID, b.texts.Ticket.NotFound, keyboard(b.backRow()))
		return
	}
	if ticket.Status != "open" {
		b.clearInput(tgID)
		b.send(ctx, chatID, "Обращение закрыто. Переписка сохранена в истории.", keyboard([]telego.InlineKeyboardButton{button("Открыть историю", "ticket:"+rawID)}))
		return
	}
	fileID, fileType := supportAttachment(ctx)
	if err := b.db.StaffTicketReply(ctx, ticketID, tgID, text, fileID, fileType); err != nil {
		b.send(ctx, chatID, b.texts.Ticket.AnswerFail, keyboard(b.backRow()))
		return
	}
	b.setInput(tgID, inputTicketReply, rawID)
	deliveryStatus := "Ответ сохранён в обращении"
	if ticket.TelegramID != nil && *ticket.TelegramID != tgID {
		if err := b.SendTicketUpdate(ctx, *ticket.TelegramID, ticketID, text, "support"); err != nil {
			b.logError("не удалось доставить ответ клиенту", err)
			deliveryStatus = "Ответ сохранён; Telegram-уведомление не доставлено"
		} else {
			deliveryStatus = "Ответ доставлен клиенту"
		}
		if fileID != "" {
			_ = b.deliverSupportFile(ctx, *ticket.TelegramID, fileID, fileType)
		}
	}
	b.upsertTicket(ctx, chatID, 0, "<b>"+deliveryStatus+" · #"+rawID+"</b>\nМожно отправить ещё сообщение. /cancel — выйти из режима ответа.", keyboard([]telego.InlineKeyboardButton{button("Обращение", "ticket:"+rawID), button("Очередь", b.ticketListBack(tgID, true))}))
}
