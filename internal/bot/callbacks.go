package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"

	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// handleCallback разбирает нажатия. Формат данных — «раздел:действие:аргумент»;
// всё, что не разобрано, отвечает подсказкой, а не молчанием.
func (b *Bot) handleCallback(ctx *telegohandler.Context, q telego.CallbackQuery) error {
	tgID := q.From.ID
	chatID := telego.ChatID{ID: tgID}
	messageID := 0
	if q.Message != nil {
		if message := q.Message.Message(); message != nil {
			chatID = telego.ChatID{ID: message.Chat.ID}
			messageID = message.MessageID
		}
	}

	parts := strings.SplitN(q.Data, ":", 3)
	kind := parts[0]
	action := ""
	rest := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if len(parts) > 2 {
		rest = parts[2]
	}

	switch kind {
	case "ops":
		b.operationsCallback(ctx, q.ID, chatID, messageID, tgID, action, rest)
	case "card":
		if chatID.ID != tgID {
			b.answer(ctx, q.ID, "Откройте бота в личном чате", true)
			return nil
		}
		b.answer(ctx, q.ID, "", false)
		if err := b.cardAction(ctx, telego.WidgetQuery{From: q.From, MessageID: messageID, Chat: &telego.Chat{ID: tgID, Type: "private"}}, q.Data); err != nil {
			b.upsert(ctx, chatID, messageID, "<b>Не удалось открыть шаг</b>\nПопробуйте ещё раз позже.", keyboard(b.backRow()))
		}
	case "devices":
		b.answer(ctx, q.ID, "", false)
		if action == "slots" {
			return b.cardAction(ctx, telego.WidgetQuery{From: q.From, MessageID: messageID, Chat: &telego.Chat{ID: tgID, Type: "private"}}, "card:slots")
		}
		page, _ := strconv.Atoi(rest)
		if b.viewMode(ctx, tgID) == "classic" && b.richAvailable() {
			html, kb := b.standardRichDevices(ctx, tgID, page)
			if delivered, _ := b.deliverRich(ctx, chatID, messageID, html, kb); delivered {
				return nil
			}
		}
		text, kb := b.classicDevices(ctx, tgID, page)
		b.upsert(ctx, chatID, messageID, text, kb)
	case "view":
		if b.cfg.BotStyle != "" {
			b.answer(ctx, q.ID, "Вид интерфейса задаёт владелец сервиса", false)
			b.showCommandScreen(ctx, chatID, "main", tgID)
			return nil
		}
		if chatID.ID != tgID || (action != "classic" && action != "cards") {
			b.answer(ctx, q.ID, "Откройте /view в личном чате", true)
			return nil
		}
		if err := b.db.SetBotView(ctx, tgID, action); err != nil {
			b.answer(ctx, q.ID, "Не удалось сохранить выбор", true)
			return nil
		}
		b.answer(ctx, q.ID, "Вид бота изменён", false)
		if err := b.showViewSelector(ctx, tgID); err != nil {
			return err
		}
		b.showCommandScreen(ctx, chatID, "main", tgID)
	case "widget":
		b.answer(ctx, q.ID, "", false)
		if action == "subscription" {
			b.showSubscriptionWidget(ctx, chatID, tgID)
		}
	case "nav":
		b.showScreen(ctx, chatID, messageID, action, tgID)
		b.answer(ctx, q.ID, "", false)

	case "plan":
		_ = b.db.SaveCheckoutPreference(ctx, tgID, action, false)
		b.showCheckout(ctx, tgID, chatID.ID, messageID, action)
		b.answer(ctx, q.ID, "", false)
	case "recurring":
		if chatID.ID != tgID {
			return nil
		}
		if action == "toggle" && b.cfg.RecurringEnabled {
			on := !b.checkoutRecurring(ctx, tgID, rest)
			if err := b.db.SaveCheckoutPreference(ctx, tgID, rest, on); err != nil {
				b.answer(ctx, q.ID, "Не удалось сохранить выбор", true)
				return nil
			}
			b.showCheckout(ctx, tgID, chatID.ID, messageID, rest)
		} else if action == "cancel" {
			subject, err := b.subject(ctx, tgID)
			if err == nil {
				err = b.pay.CancelRecurring(ctx, subject.ID)
			}
			if err != nil {
				b.answer(ctx, q.ID, "Не удалось отключить автопродление. Попробуйте ещё раз или обратитесь в поддержку.", true)
				return nil
			}
			b.showScreen(ctx, chatID, messageID, "profile", tgID)
		}
		b.answer(ctx, q.ID, "", false)

	case "pay":
		planID, token := action, rest
		b.startCheckout(ctx, q.ID, chatID, messageID, tgID, planID, token)
	case "payr":
		if !b.checkoutRecurring(ctx, tgID, action) {
			b.answer(ctx, q.ID, "Выбор оплаты изменился. Откройте тариф ещё раз.", true)
			return nil
		}
		b.startRecurringCheckout(ctx, q.ID, chatID, messageID, tgID, action, rest)

	case "payb":
		b.startBalanceCheckout(ctx, q.ID, chatID, messageID, tgID, action)

	case "check":
		b.checkOrder(ctx, q.ID, chatID, messageID, tgID, action)

	case "dev":
		b.deviceCallback(ctx, q.ID, chatID, messageID, tgID, action, rest)

	case "settings":
		b.toggleSettings(ctx, q.ID, chatID, messageID, tgID, action)

	case "promo":
		switch action {
		case "checkout":
			b.setInput(tgID, inputPromo, rest)
			b.upsert(ctx, chatID, messageID, "<b>Введите промокод</b>\nПришлите код одним сообщением. После проверки вернёмся к выбранному тарифу.", keyboard([]telego.InlineKeyboardButton{button("Отмена", "plan:"+rest)}))
			b.answer(ctx, q.ID, "", false)
		case "enter":
			b.setInput(tgID, inputPromo, "")
			b.upsert(ctx, chatID, messageID,
				"🏷️ <b>Введите промокод</b>\n\nПришлите код одним сообщением.\nОтменить — /cancel.",
				keyboard([]telego.InlineKeyboardButton{button("← Отмена", "nav:profile")}))
			b.answer(ctx, q.ID, "", false)
		case "clear":
			b.setPromo(tgID, "")
			b.answer(ctx, q.ID, "Промокод убран", false)
			text, kb := b.promoScreen(ctx, tgID)
			b.upsert(ctx, chatID, messageID, text, kb)
		default:
			b.answer(ctx, q.ID, "Неизвестное действие", true)
		}

	case "support":
		switch action {
		case "app":
			name, ok := supportApps[rest]
			if !ok {
				b.answer(ctx, q.ID, "Приложение не найдено", true)
				return nil
			}
			b.setInput(tgID, inputTicket, "setup:"+rest)
			b.upsert(ctx, chatID, messageID, "<b>Поддержка · "+name+"</b>\n\nВыберите систему или сразу опишите проблему. Можно приложить фото или документ.", keyboard([]telego.InlineKeyboardButton{button("Android", "support:device:"+rest+",android"), button("iOS", "support:device:"+rest+",ios")}, []telego.InlineKeyboardButton{button("Windows", "support:device:"+rest+",windows"), button("macOS", "support:device:"+rest+",macos")}, []telego.InlineKeyboardButton{button("Отмена", "nav:support")}))
			b.answer(ctx, q.ID, "", false)
		case "device":
			parts := strings.Split(rest, ",")
			if len(parts) != 2 || supportApps[parts[0]] == "" || supportDevices[parts[1]] == "" {
				b.answer(ctx, q.ID, "Выбор недоступен", true)
				return nil
			}
			b.setInput(tgID, inputTicket, "setup:"+rest)
			b.upsert(ctx, chatID, messageID, "<b>Сообщение в поддержку</b>\n\n"+supportApps[parts[0]]+" · "+supportDevices[parts[1]]+"\nОпишите проблему или пришлите фото/документ. Выбранное приложение и система будут приложены автоматически.", keyboard([]telego.InlineKeyboardButton{button("Отмена", "nav:support")}))
			b.answer(ctx, q.ID, "", false)
		case "order":
			order, err := b.pay.Order(ctx, rest)
			if err != nil || order.TelegramID == nil || *order.TelegramID != tgID {
				b.answer(ctx, q.ID, "Заказ не найден", true)
				return nil
			}
			b.setInput(tgID, inputTicket, "order:"+rest)
			b.upsert(ctx, chatID, messageID, "<b>Поддержка по заказу</b>\n\nЗаказ: <code>"+escapeHTML(rest)+"</code>\nОпишите проблему. Номер заказа будет приложен автоматически.", keyboard([]telego.InlineKeyboardButton{button("Отмена", "check:"+rest)}))
			b.answer(ctx, q.ID, "", false)
		case "new":
			b.setInput(tgID, inputTicket, "")
			b.upsert(ctx, chatID, messageID,
				"✉️ <b>Сообщение в поддержку</b>\n\n"+
					"Опишите проблему: что случилось, устройство и приложение. Можно прислать фото или документ.\n"+
					"Отменить — /cancel.",
				keyboard([]telego.InlineKeyboardButton{button("Happ", "support:app:happ"), button("Incy", "support:app:incy")}, []telego.InlineKeyboardButton{button("Clash Mi", "support:app:clashmi"), button("FlClashX", "support:app:flclashx")}, []telego.InlineKeyboardButton{button("← Отмена", "nav:support")}))
			b.answer(ctx, q.ID, "", false)
		case "list":
			text, kb := b.ticketsScreen(ctx, tgID, false)
			b.upsert(ctx, chatID, messageID, text, kb)
			b.answer(ctx, q.ID, "", false)
		default:
			b.answer(ctx, q.ID, "Неизвестное действие", true)
		}

	case "ticket":
		b.ticketCallback(ctx, q.ID, chatID, messageID, tgID, action, rest)

	case "admin":
		b.adminCallback(ctx, q.ID, chatID, messageID, tgID, action)

	case "poll":
		b.handlePollCallback(ctx, q.ID, chatID, messageID, tgID, action, rest)

	case "pw":
		b.handlePartnerCallback(ctx, q.ID, chatID, messageID, tgID, action, rest)

	case "ref":
		if action == "mode" {
			b.setReferralMode(ctx, q, chatID, messageID, tgID, rest)
			return nil
		}
		b.answer(ctx, q.ID, "", false)

	default:
		b.answer(ctx, q.ID, "Кнопка устарела — откройте меню заново", true)
	}
	return nil
}

// navigationScreen отдаёт экран по имени раздела.
func (b *Bot) navigationScreen(ctx context.Context, tgID int64, name string) (string, *telego.InlineKeyboardMarkup) {
	switch name {
	case "diagnostics":
		state, ok := b.diagnosticCard(ctx, tgID)
		if !ok {
			return "Проверка пока недоступна", keyboard(b.backRow())
		}
		return nativeCardScreen(state)
	case "main":
		return b.mainScreen(ctx, tgID)
	case "subscription":
		return b.subscriptionScreen(ctx, tgID)
	case "buy":
		return b.buyScreen(ctx, tgID)
	case "trial":
		return b.trialAction(ctx, tgID)
	case "devices":
		return b.devicesScreen(ctx, tgID)
	case "profile":
		return b.profileScreen(ctx, tgID)
	case "history":
		return b.historyScreen(ctx, tgID)
	case "settings":
		return b.settingsScreen(ctx, tgID)
	case "promo":
		return b.promoScreen(ctx, tgID)
	case "referral":
		return b.referralScreen(ctx, tgID)
	case "support":
		return b.supportScreen(ctx, tgID)
	case "info":
		return b.infoScreen(ctx, tgID)
	case "help":
		return b.helpScreen(ctx, tgID)
	case "more":
		return b.moreScreen(ctx, tgID)
	case "connect":
		return b.connectScreen(ctx, tgID)
	default:
		return b.mainScreen(ctx, tgID)
	}
}

func (b *Bot) upsert(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) {
	if messageID > 0 {
		b.show(ctx, chatID, messageID, text, kb)
		return
	}
	b.send(ctx, chatID, text, kb)
}

// ---------------------------------------------------------------------------
// Устройства
// ---------------------------------------------------------------------------

func (b *Bot) deviceCallback(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, action, rest string) {
	switch action {
	case "buy":
		text, kb := b.deviceMethodsScreen(ctx, tgID, rest, "")
		b.upsert(ctx, chatID, messageID, text, kb)
		b.answer(ctx, queryID, "", false)
	case "pay":
		packID, token, _ := strings.Cut(rest, ":")
		b.startDeviceCheckout(ctx, queryID, chatID, messageID, tgID, packID, "", token)
	case "renew":
		grantID, token, _ := strings.Cut(rest, ":")
		b.startDeviceCheckout(ctx, queryID, chatID, messageID, tgID, "", grantID, token)
	case "rm":
		b.answer(ctx, queryID, "", false)
		b.upsert(ctx, chatID, messageID, "<b>Отвязать устройство?</b>\n\nУдалится запись из списка устройств. Подписка и установленный профиль сохранятся. При следующем подключении устройство может появиться в списке снова.", keyboard(
			[]telego.InlineKeyboardButton{button("Да, отвязать", "dev:confirmrm:"+rest)},
			[]telego.InlineKeyboardButton{button("Оставить устройство", "nav:devices")},
		))
	case "confirmrm":
		b.removeDevice(ctx, queryID, chatID, messageID, tgID, rest)
	default:
		b.answer(ctx, queryID, "Неизвестное действие", true)
	}
}

func (b *Bot) deviceMethodsScreen(ctx context.Context, tgID int64, packID, grantOrderID string) (string, *telego.InlineKeyboardMarkup) {
	var title string
	if grantOrderID != "" {
		title = "🔄 <b>Продление устройств</b>"
	} else {
		pack, err := b.db.DevicePack(ctx, packID)
		if err != nil {
			return "Пакет не найден.", keyboard(b.backRow())
		}
		title = "🧩 <b>" + escapeHTML(pack.Title) + "</b>"
	}
	options := b.pay.PaymentOptions(false)
	if len(options) == 0 {
		return "⚠️ Платёжные способы пока не настроены.", keyboard(b.backRow())
	}
	lines := []string{title, "", "Выберите способ оплаты:"}
	var rows [][]telego.InlineKeyboardButton
	for _, option := range options {
		data := "dev:pay:" + packID + ":" + option.Token()
		if grantOrderID != "" {
			data = "dev:renew:" + grantOrderID + ":" + option.Token()
		}
		rows = append(rows, []telego.InlineKeyboardButton{
			button(option.ProviderLabel+" · "+option.MethodLabel, data),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← Устройства", "nav:devices"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// removeDevice отвязывает устройство по хвосту HWID: целиком он не влезает
// в callback-данные, а хвоста хватает, чтобы отличить устройства.
func (b *Bot) removeDevice(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, tail string) {
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		b.answer(ctx, queryID, "Профиль недоступен", true)
		return
	}
	access, err := b.accessFor(ctx, subject)
	if err != nil || !access.Found || access.PanelUserID == 0 {
		b.answer(ctx, queryID, "Подписка не найдена", true)
		return
	}
	hwid := ""
	matches := 0
	for _, device := range access.Devices {
		if strings.HasSuffix(device.HWID, tail) {
			hwid = device.HWID
			matches++
		}
	}
	if matches > 1 || tail == "" {
		b.answer(ctx, queryID, "Не удалось однозначно определить устройство. Откройте новый вид бота.", true)
		return
	}
	if hwid == "" {
		b.answer(ctx, queryID, "Устройство уже отвязано", true)
		text, kb := b.devicesScreen(ctx, tgID)
		b.upsert(ctx, chatID, messageID, text, kb)
		return
	}
	removed, err := b.pay.RemoveAccessDevice(ctx, access.PanelUserID, hwid)
	if err != nil {
		b.answer(ctx, queryID, "Не удалось отвязать: "+err.Error(), true)
		return
	}
	if !removed {
		b.answer(ctx, queryID, "Устройство не найдено в панели", true)
	} else {
		b.answer(ctx, queryID, "Устройство отвязано", false)
	}
	text, kb := b.devicesScreen(ctx, tgID)
	b.upsert(ctx, chatID, messageID, text, kb)
}

// setReferralMode переключает, чем начислять пригласившему: днями или
// деньгами на баланс.
func (b *Bot) setReferralMode(ctx context.Context, q telego.CallbackQuery, chatID telego.ChatID, messageID int, tgID int64, mode string) {
	if mode != "days" && mode != "balance" && mode != "both" {
		b.answer(ctx, q.ID, "Неизвестный режим", true)
		return
	}
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		b.answer(ctx, q.ID, "Профиль недоступен", true)
		return
	}
	if err := b.db.SetReferralMode(ctx, subject.ID, mode); err != nil {
		b.answer(ctx, q.ID, b.texts.Settings.Failed, true)
		return
	}
	b.answer(ctx, q.ID, "Режим: "+referralModeLabel(mode), false)
	text, kb := b.referralScreen(ctx, tgID)
	b.upsert(ctx, chatID, messageID, text, kb)
}

// ---------------------------------------------------------------------------
// Настройки и обращения
// ---------------------------------------------------------------------------

func (b *Bot) toggleSettings(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, key string) {
	settings, err := b.db.NotificationSettingsFor(ctx, tgID)
	if err != nil {
		b.answer(ctx, queryID, "Настройки недоступны", true)
		return
	}
	switch key {
	case "expiry":
		settings.Expiry = !settings.Expiry
	case "activity":
		settings.Activity = !settings.Activity
	default:
		b.answer(ctx, queryID, "Неизвестная настройка", true)
		return
	}
	if err := b.db.SaveNotificationSettings(ctx, tgID, settings); err != nil {
		b.answer(ctx, queryID, "Не удалось сохранить", true)
		return
	}
	b.answer(ctx, queryID, "Сохранено", false)
	b.showScreen(ctx, chatID, messageID, "settings", tgID)
}

func (b *Bot) ticketsScreen(ctx context.Context, tgID int64, staff bool) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	tickets, err := b.db.TicketsForSubject(ctx, subject.ID, 10)
	if err != nil {
		return "⚠️ Не удалось загрузить обращения.", keyboard(b.backRow())
	}
	lines := []string{"📋 <b>Мои обращения</b>", ""}
	if len(tickets) == 0 {
		lines = append(lines, "Обращений пока нет.")
	}
	var rows [][]telego.InlineKeyboardButton
	for _, ticket := range tickets {
		status := "🟢 открыто"
		if ticket.Status != "open" {
			status = "⚪️ закрыто"
		}
		if ticket.Unread() {
			status += " · новый ответ"
		}
		lines = append(lines, fmt.Sprintf("#%d · %s · %s · %s",
			ticket.ID, dateText(ticket.CreatedAt), status, escapeHTML(ticket.Subject)))
		rows = append(rows, []telego.InlineKeyboardButton{
			button(fmt.Sprintf("Открыть #%d", ticket.ID), "ticket:"+strconv.FormatInt(ticket.ID, 10)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("✉️ Новое обращение", "support:new"),
		button("← Поддержка", "nav:support"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

func (b *Bot) ticketCallback(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, action, rest string) {
	switch action {
	case "resolve":
		b.resolveOwnTicket(ctx, queryID, chatID, messageID, tgID, rest)
	case "file":
		b.ticketAttachment(ctx, queryID, chatID, tgID, rest)
	case "page":
		parts := strings.Split(rest, ",")
		if len(parts) != 2 {
			b.answer(ctx, queryID, "Страница недоступна", true)
			return
		}
		id, e1 := strconv.ParseInt(parts[0], 10, 64)
		page, e2 := strconv.Atoi(parts[1])
		if e1 != nil || e2 != nil {
			b.answer(ctx, queryID, "Страница недоступна", true)
			return
		}
		text, kb, err := b.ticketPageScreen(ctx, tgID, id, page)
		if err != nil {
			b.answer(ctx, queryID, "Обращение недоступно", true)
			return
		}
		_ = b.deliver(ctx, chatID, messageID, text, kb)
		b.answer(ctx, queryID, "", false)
	case "reply":
		b.answer(ctx, queryID, "", false)
		b.beginTicketReply(ctx, chatID, messageID, tgID, rest)
	case "close":
		b.closeTicket(ctx, queryID, chatID, messageID, tgID, rest)
	default:
		id, err := strconv.ParseInt(action, 10, 64)
		if err != nil {
			b.answer(ctx, queryID, "Обращение не найдено", true)
			return
		}
		text, kb := b.ticketScreen(ctx, tgID, id)
		ticket, _ := b.db.TicketByID(ctx, id)
		markRead := func() {
			if subject, e := b.subject(ctx, tgID); e == nil {
				_ = b.db.MarkTicketRead(ctx, id, subject.ID, ticket.LastStaffMessageID)
			}
		}
		if b.viewMode(ctx, tgID) == "cards" {
			if state, err := b.ticketCard(ctx, tgID, id); err == nil {
				if b.deliverUI(ctx, tgID, messageID, state) == nil {
					markRead()
				}
				b.answer(ctx, queryID, "", false)
				return
			}
		}
		if b.deliver(ctx, chatID, messageID, text, kb) == nil {
			markRead()
		}
		b.answer(ctx, queryID, "", false)
	}
}

// ticketScreen показывает переписку. Пользователь видит свои обращения, а
// сотрудник — любое: отвечать можно из того же экрана.
func (b *Bot) ticketScreen(ctx context.Context, tgID, ticketID int64) (string, *telego.InlineKeyboardMarkup) {
	text, kb, _ := b.ticketPageScreen(ctx, tgID, ticketID, -1)
	return text, kb
}

func (b *Bot) beginTicketReply(ctx context.Context, chatID telego.ChatID, messageID int, tgID int64, rawID string) {
	ticketID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		b.send(ctx, chatID, "Обращение не найдено.", keyboard(b.backRow()))
		return
	}
	ticket, err := b.db.TicketByID(ctx, ticketID)
	if err != nil {
		b.send(ctx, chatID, "Обращение не найдено.", keyboard(b.backRow()))
		return
	}
	staff := b.isStaff(ctx, tgID)
	if ticket.Status != "open" {
		b.send(ctx, chatID, "Обращение закрыто. Создайте новое, если нужна помощь.", keyboard([]telego.InlineKeyboardButton{button("Поддержка", "nav:support")}))
		return
	}
	subject, subjectErr := b.subject(ctx, tgID)
	if !staff && (subjectErr != nil || !supportTicketOwned(ticket, subject)) {
		b.send(ctx, chatID, "Это обращение вам не принадлежит.", keyboard(b.backRow()))
		return
	}
	current := inputTicket
	if staff {
		current = inputTicketReply
	}
	b.setInput(tgID, current, rawID)
	b.upsert(ctx, chatID, messageID,
		b.texts.Ticket.Hint,
		keyboard([]telego.InlineKeyboardButton{
			button(b.texts.Ticket.ToTicket, "ticket:"+rawID),
		}))
}

func (b *Bot) closeTicket(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, rawID string) {
	if !b.isStaff(ctx, tgID) {
		b.answer(ctx, queryID, "Недостаточно прав", true)
		return
	}
	ticketID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		b.answer(ctx, queryID, "Обращение не найдено", true)
		return
	}
	closed, err := b.db.CloseTicket(ctx, ticketID)
	if err != nil {
		b.answer(ctx, queryID, "Не удалось закрыть", true)
		return
	}
	if !closed {
		b.answer(ctx, queryID, "Обращение уже закрыто", true)
	} else {
		b.answer(ctx, queryID, "Обращение закрыто", false)
		if ticket, err := b.db.TicketByID(ctx, ticketID); err == nil {
			b.notifyTicketOwner(ctx, ticket, "✅ Обращение #"+rawID+" закрыто.\n\nЕсли проблема вернётся — напишите снова.")
		}
	}
	text, kb := b.ticketScreen(ctx, tgID, ticketID)
	b.upsert(ctx, chatID, messageID, text, kb)
}

// notifyTicketOwner пишет человеку, чьё обращение изменилось.
func (b *Bot) notifyTicketOwner(ctx context.Context, ticket storage.Ticket, text string) {
	if ticket.TelegramID == nil {
		return
	}
	b.send(ctx, telego.ChatID{ID: *ticket.TelegramID}, text, nil)
}

// ---------------------------------------------------------------------------
// Промокод, триал и оплата: сами действия живут в payments.go
// ---------------------------------------------------------------------------

func (b *Bot) trialAction(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	checkout, err := b.pay.Checkout(ctx, payments.Purchase{
		TelegramID: &tgID,
		PlanID:     "trial_bot",
	})
	if err != nil {
		return b.trialErrorText(err), keyboard(b.backRow())
	}
	_ = checkout
	return "🎁 <b>Пробный доступ активирован</b>\n\n" +
			"Откройте «Мою подписку» — там уже ссылка для подключения.",
		keyboard([]telego.InlineKeyboardButton{
			button("📱 Моя подписка", "nav:subscription"),
			button("🏠 Меню", "nav:main"),
		})
}

func (b *Bot) trialErrorText(err error) string {
	switch {
	case errors.Is(err, payments.ErrTrialUsed):
		return "🎁 Пробный период уже был использован.\n\nВыберите тариф — доступ появится после оплаты."
	case errors.Is(err, payments.ErrTrialWhileActive):
		return "✅ Подписка ещё действует — пробный период не положен."
	default:
		return "⚠️ Не удалось выдать пробный доступ: " + escapeHTML(err.Error())
	}
}
