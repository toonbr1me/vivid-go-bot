package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
)

// handleAdmin открывает панель сотрудника.
func (b *Bot) handleAdmin(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	if message.Chat.ID != tgID {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID}, "Откройте /admin в личном чате с ботом.", nil)
		return nil
	}
	if !b.isStaff(ctx, tgID) {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID}, "🔐 Команда доступна только сотрудникам.", nil)
		return nil
	}
	text, kb := b.adminScreen(ctx, tgID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

func (b *Bot) adminScreen(ctx context.Context, actor ...int64) (string, *telego.InlineKeyboardMarkup) {
	open, _ := b.db.OpenTicketCount(ctx)
	lines := []string{
		"🛠 <b>Панель управления</b>",
		"",
		fmt.Sprintf("Открытых обращений: <b>%d</b>", open),
		"",
		"🔹 /tickets — очередь обращений",
		"🔹 /reply <обращение> <текст> — ответить",
		"🔹 /close <обращение> — закрыть",
	}
	kb := keyboard(
		[]telego.InlineKeyboardButton{
			button("📋 Обращения", "admin:tickets"),
		},
		b.backRow())
	if len(actor) > 0 && b.db.Permission(ctx, actor[0], "stats.view", b.cfg.Current().AdminIDs) {
		lines = append(lines, "🔹 /stats — статистика")
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telego.InlineKeyboardButton{button("📊 Статистика", "admin:stats")})
	}
	if len(actor) > 0 && b.db.Permission(ctx, actor[0], "subscription.grant", b.cfg.Current().AdminIDs) {
		lines = append(lines, "🔹 /grant <id> <дни> — выдать подписку")
	}
	if len(actor) > 0 && b.db.Permission(ctx, actor[0], "orders.view", b.cfg.Current().AdminIDs) {
		kb.InlineKeyboard = append([][]telego.InlineKeyboardButton{{button("Оплачены, ждут доступа", "ops:list")}}, kb.InlineKeyboard...)
	}
	if len(actor) > 0 && b.db.Permission(ctx, actor[0], "broadcast.send", b.cfg.Current().AdminIDs) {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []telego.InlineKeyboardButton{button("Рассылка", "ann:home")})
	}
	if site := b.siteURL(); site != "" {
		row := []telego.InlineKeyboardButton{}
		if telegramURL(site) {
			row = append(row, urlButton("Веб-пульт", site+"/admin"))
		} else {
			row = append(row, copyButton("Адрес веб-пульта", site+"/admin"))
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, row)
	}
	return strings.Join(lines, "\n"), kb
}

func (b *Bot) adminCallback(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, action string, arguments ...string) {
	if chatID.ID != tgID {
		b.answer(ctx, queryID, "Откройте /admin в личном чате", true)
		return
	}
	if !b.isStaff(ctx, tgID) {
		b.answer(ctx, queryID, "Недостаточно прав", true)
		return
	}
	var text string
	var kb *telego.InlineKeyboardMarkup
	switch action {
	case "stats":
		if !b.db.Permission(ctx, tgID, "stats.view", b.cfg.Current().AdminIDs) {
			b.answer(ctx, queryID, "Недостаточно прав", true)
			return
		}
		text, kb = b.statsScreen(ctx)
	case "tickets":
		page := 0
		if len(arguments) > 0 {
			page = ticketListPage(arguments[0])
		}
		text, kb = b.adminTicketsScreen(ctx, tgID, page)
	default:
		text, kb = b.adminScreen(ctx, tgID)
	}
	b.upsert(ctx, chatID, messageID, text, kb)
	b.answer(ctx, queryID, "", false)
}

func (b *Bot) statsScreen(ctx context.Context) (string, *telego.InlineKeyboardMarkup) {
	summary, err := b.db.Stats(ctx, 7)
	if err != nil {
		return "⚠️ Не удалось собрать статистику.", keyboard(b.backRow())
	}
	lines := []string{
		"📊 <b>Статистика за 7 дней</b>",
		"",
		fmt.Sprintf("👥 Пользователей: <b>%d</b>", summary.Users),
		fmt.Sprintf("📦 Подписок выдано: <b>%d</b>", summary.Subscriptions),
		fmt.Sprintf("✅ Активных сегодня: <b>%d</b>", summary.ActiveToday),
		fmt.Sprintf("💰 Оплаченных заказов: <b>%d</b>", summary.PaidOrders),
		fmt.Sprintf("📈 Оборот: <b>%s</b>", priceText(summary.Revenue, b.cfg.Current().Currency)),
		fmt.Sprintf("🎁 Пробных периодов: <b>%d</b>", summary.Trials),
		fmt.Sprintf("✉️ Открытых обращений: <b>%d</b>", summary.OpenTickets),
	}
	return strings.Join(lines, "\n"), keyboard(
		[]telego.InlineKeyboardButton{
			button("🔄 Обновить", "admin:stats"),
			button("← Панель", "admin:home"),
		})
}

func (b *Bot) adminTicketsScreen(ctx context.Context, tgID int64, requested ...int) (string, *telego.InlineKeyboardMarkup) {
	page := 0
	if len(requested) > 0 {
		page = requested[0]
	}
	result, err := b.db.QueueTicketsPage(ctx, tgID, page, 10)
	if err != nil {
		return "⚠️ Не удалось загрузить очередь.", keyboard(b.backRow())
	}
	tickets := result.Tickets
	b.rememberTicketList(tgID, result.Page, true)
	lines := []string{"📋 <b>Очередь обращений</b>", fmt.Sprintf("Страница %d из %d · всего %d", result.Page+1, result.Pages, result.Total), ""}
	if len(tickets) == 0 {
		lines = append(lines, "Открытых обращений нет.")
	}
	rows := [][]telego.InlineKeyboardButton{{button("Свободные", "queue:free:0"), button("Мои", "queue:mine:0")}, {button("Ждут ответа", "queue:waiting:0"), button("Закрытые", "queue:closed:0")}}
	for _, ticket := range tickets {
		assignee := "свободно"
		if ticket.AssignedTo != nil {
			assignee = strconv.FormatInt(*ticket.AssignedTo, 10)
		}
		lines = append(lines, fmt.Sprintf("#%d · %s · %s · %s",
			ticket.ID, dateText(ticket.CreatedAt), assignee, escapeHTML(firstRunes(ticket.Subject, 80))))
		rows = append(rows, []telego.InlineKeyboardButton{
			button(fmt.Sprintf("Открыть #%d", ticket.ID), "ticket:"+strconv.FormatInt(ticket.ID, 10)),
		})
	}
	if pager := ticketListPager(result, "admin:tickets"); len(pager) > 0 {
		rows = append(rows, pager)
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("🔄 Обновить", fmt.Sprintf("admin:tickets:%d", result.Page)),
		button("← Панель", "admin:home"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// handleStats печатает сводку.
func (b *Bot) handleStats(ctx *telegohandler.Context, message telego.Message) error {
	if message.Chat.ID != message.From.ID || !b.db.Permission(ctx, message.From.ID, "stats.view", b.cfg.Current().AdminIDs) {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID}, "🔐 Команда доступна только сотрудникам.", nil)
		return nil
	}
	text, kb := b.statsScreen(ctx)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

// handleTickets показывает очередь.
func (b *Bot) handleTickets(ctx *telegohandler.Context, message telego.Message) error {
	if !b.isStaff(ctx, message.From.ID) {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID}, "🔐 Команда доступна только сотрудникам.", nil)
		return nil
	}
	text, kb := b.adminTicketsScreen(ctx, message.From.ID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

// handleGrant — /grant <telegram_id> <дни> [комментарий].
func (b *Bot) handleGrant(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	chatID := telego.ChatID{ID: message.Chat.ID}
	if !b.db.Permission(ctx, tgID, "subscription.grant", b.cfg.Current().AdminIDs) {
		b.send(ctx, chatID, "🔐 Недостаточно прав.", nil)
		return nil
	}
	fields := strings.Fields(message.Text)
	if len(fields) < 3 {
		b.send(ctx, chatID, "Формат: <code>/grant 123456789 30 [комментарий]</code>", nil)
		return nil
	}
	targetID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		b.send(ctx, chatID, "Первым аргументом нужен Telegram ID получателя.", nil)
		return nil
	}
	days, err := strconv.Atoi(fields[2])
	if err != nil || days <= 0 || days > 3650 {
		b.send(ctx, chatID, "Вторым аргументом нужно число дней от 1 до 3650.", nil)
		return nil
	}
	reason := strings.Join(fields[3:], " ")
	subject, err := b.subject(ctx, targetID)
	if err != nil {
		b.send(ctx, chatID, "⚠️ Не удалось открыть профиль получателя: "+err.Error(), nil)
		return nil
	}
	expireAt, err := b.pay.GrantDays(ctx, subject, days, "manual_grant")
	if err != nil {
		b.send(ctx, chatID, "⚠️ Не удалось выдать дни: "+err.Error(), nil)
		return nil
	}
	_ = b.db.LogAction(ctx, tgID, "subscription.grant", strconv.FormatInt(targetID, 10), 0, reason)
	text := fmt.Sprintf("✅ Выдано <b>%d %s</b> пользователю <code>%d</code>.", days, daysWord(days), targetID)
	if expireAt != nil {
		text += "\nПодписка действует до <b>" + dateText(*expireAt) + "</b>."
	}
	b.send(ctx, chatID, text, nil)

	b.send(ctx, telego.ChatID{ID: targetID},
		fmt.Sprintf("🎁 Поддержка начислила вам <b>%d %s</b> подписки.", days, daysWord(days)),
		keyboard([]telego.InlineKeyboardButton{
			button("📱 Моя подписка", "nav:subscription"),
		}))
	return nil
}

// handleReply — /reply <обращение> <текст>.
func (b *Bot) handleReply(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	chatID := telego.ChatID{ID: message.Chat.ID}
	if !b.db.Permission(ctx, tgID, "support.answer", b.cfg.Current().AdminIDs) {
		b.send(ctx, chatID, "🔐 Недостаточно прав.", nil)
		return nil
	}
	fields := strings.SplitN(strings.TrimSpace(message.Text), " ", 3)
	if len(fields) < 3 {
		b.send(ctx, chatID, "Формат: <code>/reply 42 Текст ответа</code>", nil)
		return nil
	}
	ticketID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		b.send(ctx, chatID, "Нужен номер обращения.", nil)
		return nil
	}
	ticket, err := b.db.TicketByID(ctx, ticketID)
	if err != nil {
		b.send(ctx, chatID, "Обращение не найдено.", nil)
		return nil
	}
	if err := b.db.StaffTicketReply(ctx, ticketID, tgID, fields[2], "", ""); err != nil {
		b.send(ctx, chatID, "⚠️ Не удалось отправить ответ: "+err.Error(), nil)
		return nil
	}
	if ticket.TelegramID != nil {
		_ = b.SendTicketUpdate(ctx, *ticket.TelegramID, ticketID, fields[2], "support")
	}
	_ = b.db.LogAction(ctx, tgID, "support.answer", "ticket:"+fields[1], 0, "")
	b.send(ctx, chatID, "✅ Ответ отправлен.", nil)
	return nil
}

// handleCloseTicketCommand — /close <обращение>.
func (b *Bot) handleCloseTicketCommand(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	chatID := telego.ChatID{ID: message.Chat.ID}
	if !b.db.Permission(ctx, tgID, "support.answer", b.cfg.Current().AdminIDs) {
		b.send(ctx, chatID, "🔐 Недостаточно прав.", nil)
		return nil
	}
	fields := strings.Fields(message.Text)
	if len(fields) < 2 {
		b.send(ctx, chatID, "Формат: <code>/close 42</code>", nil)
		return nil
	}
	ticketID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		b.send(ctx, chatID, "Нужен номер обращения.", nil)
		return nil
	}
	ticket, err := b.db.TicketByID(ctx, ticketID)
	if err != nil {
		b.send(ctx, chatID, "Обращение не найдено.", nil)
		return nil
	}
	err = b.db.StaffTicketAction(ctx, ticketID, tgID, "close", "", 0)
	closed := err == nil
	if err != nil {
		b.send(ctx, chatID, "⚠️ Не удалось закрыть: "+err.Error(), nil)
		return nil
	}
	if closed && ticket.TelegramID != nil {
		b.send(ctx, telego.ChatID{ID: *ticket.TelegramID},
			"✅ Обращение #"+fields[1]+" закрыто.\n\nЕсли проблема вернётся — напишите снова.",
			keyboard([]telego.InlineKeyboardButton{
				button("✉️ Поддержка", "nav:support"),
			}))
	}
	b.send(ctx, chatID, "✅ Обращение закрыто.", nil)
	return nil
}
