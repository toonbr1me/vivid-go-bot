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
		"🔹 /stats — статистика",
		"🔹 /tickets — очередь обращений",
		"🔹 /grant <id> <дни> — выдать подписку",
		"🔹 /reply <обращение> <текст> — ответить",
		"🔹 /close <обращение> — закрыть",
	}
	kb := keyboard(
		[]telego.InlineKeyboardButton{
			button("📊 Статистика", "admin:stats"),
			button("📋 Обращения", "admin:tickets"),
		},
		b.backRow())
	if len(actor) > 0 && b.db.Permission(ctx, actor[0], "orders.view", b.cfg.AdminIDs) {
		kb.InlineKeyboard = append([][]telego.InlineKeyboardButton{{button("Оплачены, ждут доступа", "ops:list")}}, kb.InlineKeyboard...)
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

func (b *Bot) adminCallback(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, action string) {
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
		text, kb = b.statsScreen(ctx)
	case "tickets":
		text, kb = b.adminTicketsScreen(ctx, tgID)
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
		fmt.Sprintf("📈 Оборот: <b>%s</b>", priceText(summary.Revenue, b.cfg.Currency)),
		fmt.Sprintf("🎁 Пробных периодов: <b>%d</b>", summary.Trials),
		fmt.Sprintf("✉️ Открытых обращений: <b>%d</b>", summary.OpenTickets),
	}
	return strings.Join(lines, "\n"), keyboard(
		[]telego.InlineKeyboardButton{
			button("🔄 Обновить", "admin:stats"),
			button("← Панель", "admin:home"),
		})
}

func (b *Bot) adminTicketsScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	tickets, err := b.db.QueueTickets(ctx, tgID, 10)
	if err != nil {
		return "⚠️ Не удалось загрузить очередь.", keyboard(b.backRow())
	}
	lines := []string{"📋 <b>Очередь обращений</b>", ""}
	if len(tickets) == 0 {
		lines = append(lines, "Открытых обращений нет.")
	}
	var rows [][]telego.InlineKeyboardButton
	for _, ticket := range tickets {
		assignee := "свободно"
		if ticket.AssignedTo != nil {
			assignee = strconv.FormatInt(*ticket.AssignedTo, 10)
		}
		lines = append(lines, fmt.Sprintf("#%d · %s · %s · %s",
			ticket.ID, dateText(ticket.CreatedAt), assignee, escapeHTML(ticket.Subject)))
		rows = append(rows, []telego.InlineKeyboardButton{
			button(fmt.Sprintf("Открыть #%d", ticket.ID), "ticket:"+strconv.FormatInt(ticket.ID, 10)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("🔄 Обновить", "admin:tickets"),
		button("← Панель", "admin:home"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// handleStats печатает сводку.
func (b *Bot) handleStats(ctx *telegohandler.Context, message telego.Message) error {
	if !b.isStaff(ctx, message.From.ID) {
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
	if !b.db.Permission(ctx, tgID, "subscription.grant", b.cfg.AdminIDs) {
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
	if !b.db.Permission(ctx, tgID, "support.answer", b.cfg.AdminIDs) {
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
	subjectID := int64(0)
	if ticket.SubjectID != nil {
		subjectID = *ticket.SubjectID
	} else if ticket.TelegramID != nil {
		if subject, err := b.subject(ctx, *ticket.TelegramID); err == nil {
			subjectID = subject.ID
		}
	}
	if _, err := b.db.AddTicketMessage(ctx, ticketID, subjectID, &tgID, fields[2], true, "", ""); err != nil {
		b.send(ctx, chatID, "⚠️ Не удалось отправить ответ: "+err.Error(), nil)
		return nil
	}
	if ticket.TelegramID != nil {
		header := "🛠 Ответ поддержки по обращению #" + fields[1] + ":"
		_ = b.deliverMarkdown(ctx, telego.ChatID{ID: *ticket.TelegramID}, 0,
			header+"\n\n"+fields[2],
			keyboard([]telego.InlineKeyboardButton{
				button("📨 Открыть обращение", "ticket:"+fields[1]),
			}))
	}
	_ = b.db.LogAction(ctx, tgID, "support.answer", "ticket:"+fields[1], 0, "")
	b.send(ctx, chatID, "✅ Ответ отправлен.", nil)
	return nil
}

// handleCloseTicketCommand — /close <обращение>.
func (b *Bot) handleCloseTicketCommand(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	chatID := telego.ChatID{ID: message.Chat.ID}
	if !b.db.Permission(ctx, tgID, "support.answer", b.cfg.AdminIDs) {
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
	closed, err := b.db.CloseTicket(ctx, ticketID)
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
