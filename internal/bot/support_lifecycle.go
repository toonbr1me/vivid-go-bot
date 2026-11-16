package bot

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type ticketRefKey struct{}

func supportTicketOwned(t storage.Ticket, s domain.Subject) bool {
	return t.SubjectID != nil && *t.SubjectID == s.ID || t.TelegramID != nil && s.TelegramID != nil && *t.TelegramID == *s.TelegramID
}

var supportApps = map[string]string{"happ": "Happ", "incy": "Incy", "clashmi": "Clash Mi", "flclashx": "FlClashX"}
var supportDevices = map[string]string{"android": "Android", "ios": "iOS", "windows": "Windows", "macos": "macOS"}

func (b *Bot) ticketPageScreen(ctx context.Context, id, ticketID int64, page int) (string, *telego.InlineKeyboardMarkup, error) {
	conversation, err := b.loadTicketConversation(ctx, id, ticketID)
	if err != nil {
		return "Обращение недоступно.", keyboard(b.backRow()), err
	}
	pages := b.ticketThreadPages(conversation)
	if page < 0 || page >= len(pages) {
		page = len(pages) - 1
	}
	current := pages[page]
	header := fmt.Sprintf("<b>Обращение #%d</b>\n\n<b>%s</b> · %s\n\n", ticketID,
		ticketStatusLabel(conversation.ticket.Status), escapeHTML(firstRunes(conversation.ticket.Subject, 60)))
	if conversation.ticket.Context != "" {
		header += escapeHTML(firstRunes(conversation.ticket.Context, 180)) + "\n\n"
	}
	text := header + current.html
	if len(pages) > 1 {
		text += fmt.Sprintf("\n<i>История переписки · страница %d из %d</i>", page+1, len(pages))
	}
	rows := [][]telego.InlineKeyboardButton{}
	for _, attachment := range current.attachments {
		rows = append(rows, []telego.InlineKeyboardButton{attachment})
	}
	pager := []telego.InlineKeyboardButton{}
	if page > 0 {
		pager = append(pager, button("← Раньше", fmt.Sprintf("ticket:page:%d,%d", ticketID, page-1)))
	}
	if page+1 < len(pages) {
		pager = append(pager, button("Позже →", fmt.Sprintf("ticket:page:%d,%d", ticketID, page+1)))
	}
	if len(pages) > 1 && b.siteURL() != "" {
		rows = append(rows, []telego.InlineKeyboardButton{urlButton("Вся переписка на сайте", fmt.Sprintf("%s/support/tickets/%d", b.siteURL(), ticketID))})
	}
	if len(pager) > 0 {
		rows = append(rows, pager)
	}
	if page+1 < len(pages) {
		rows = append(rows, []telego.InlineKeyboardButton{button("К последним сообщениям", fmt.Sprintf("ticket:page:%d,-1", ticketID))})
	}
	rows = append(rows, []telego.InlineKeyboardButton{button("Обновить переписку", fmt.Sprintf("ticket:page:%d,-1", ticketID))})
	for _, action := range conversation.actions {
		rows = append(rows, []telego.InlineKeyboardButton{action})
	}
	if conversation.ticket.Status == "open" {
		kind, ref := b.input(id)
		if (kind == inputTicket || kind == inputTicketReply) && ref == strconv.FormatInt(ticketID, 10) {
			text += "\n\n<b>Вы отвечаете в это обращение.</b> Пишите сюда: можно отправить несколько сообщений, фото или документ. Выход — /cancel или главное меню."
		} else {
			text += "\n\nНажмите «Ответить», чтобы продолжить переписку."
		}
	}
	return text, keyboard(rows...), nil
}

func supportAttachment(ctx context.Context) (string, string) {
	m, ok := ctx.Value(incomingMessageKey{}).(telego.Message)
	if !ok {
		return "", ""
	}
	if len(m.Photo) > 0 {
		return m.Photo[len(m.Photo)-1].FileID, "photo"
	}
	if m.Document != nil {
		return m.Document.FileID, "document"
	}
	return "", ""
}

func (b *Bot) handleSupportAttachment(ctx *telegohandler.Context, m telego.Message) error {
	if m.From == nil {
		return nil
	}
	kind, _ := b.input(m.From.ID)
	if (kind != inputTicket && kind != inputTicketReply) || (len(m.Photo) == 0 && m.Document == nil) {
		b.send(ctx, telego.ChatID{ID: m.Chat.ID}, "Фото или документ можно отправить после выбора «Ответить» в обращении.", keyboard([]telego.InlineKeyboardButton{button("Поддержка", "nav:support")}))
		return nil
	}
	m.Text = m.Caption
	return b.handleText(ctx, m)
}

func (b *Bot) resolveOwnTicket(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, raw string) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		b.answer(ctx, queryID, "Обращение не найдено", true)
		return
	}
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		b.answer(ctx, queryID, "Не удалось загрузить аккаунт", true)
		return
	}
	ticket, err := b.db.TicketByID(ctx, id)
	if err != nil || !supportTicketOwned(ticket, subject) {
		b.answer(ctx, queryID, "Обращение не найдено", true)
		return
	}
	if _, err = b.db.ResolveOwnTicket(ctx, id, subject.ID); err != nil {
		b.answer(ctx, queryID, "Не удалось закрыть обращение", true)
		return
	}
	b.clearInput(tgID)
	text, kb := b.ticketScreen(ctx, tgID, id)
	b.upsertTicket(ctx, chatID, messageID, text, kb)
	b.answer(ctx, queryID, "Проблема решена. Обращение сохранено в истории.", false)
}

// File identifiers stay on the server; only an authorised message ID is in callbacks.
func (b *Bot) ticketAttachment(ctx context.Context, queryID string, chatID telego.ChatID, tgID int64, raw string) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		b.answer(ctx, queryID, "Вложение не найдено", true)
		return
	}
	var ticketID int64
	err = b.db.Pool.QueryRow(ctx, `SELECT ticket_id FROM support_messages WHERE id=$1 AND NOT is_internal`, id).Scan(&ticketID)
	if err != nil {
		b.answer(ctx, queryID, "Вложение не найдено", true)
		return
	}
	ticket, err := b.db.TicketByID(ctx, ticketID)
	subject, e := b.subject(ctx, tgID)
	if err != nil || e != nil || !supportTicketOwned(ticket, subject) && !b.isStaff(ctx, tgID) {
		b.answer(ctx, queryID, "Вложение недоступно", true)
		return
	}
	messages, err := b.db.TicketHistory(ctx, ticketID)
	if err == nil {
		for _, m := range messages {
			if m.ID != id || m.FileID == nil || m.FileType == nil {
				continue
			}
			if *m.FileType == "photo" {
				_, err = b.api.SendPhoto(ctx, &telego.SendPhotoParams{ChatID: chatID, Photo: telego.InputFile{FileID: *m.FileID}})
			} else {
				_, err = b.api.SendDocument(ctx, &telego.SendDocumentParams{ChatID: chatID, Document: telego.InputFile{FileID: *m.FileID}})
			}
			if err != nil {
				b.answer(ctx, queryID, "Не удалось открыть вложение", true)
			} else {
				b.answer(ctx, queryID, "", false)
			}
			return
		}
	}
	b.answer(ctx, queryID, "Вложение не найдено", true)
}
