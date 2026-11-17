package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// The default screen is a compact ticket card. Full history is an explicit action.
func (b *Bot) ticketSummaryScreen(ctx context.Context, id, ticketID int64) (string, *telego.InlineKeyboardMarkup, error) {
	c, err := b.loadTicketConversation(ctx, id, ticketID)
	if err != nil {
		return "Обращение недоступно.", keyboard(b.backRow()), err
	}
	text := fmt.Sprintf("<b>Обращение #%d</b> · %s\n%s", ticketID, ticketStatusLabel(c.ticket.Status), escapeHTML(firstRunes(c.ticket.Subject, 100)))
	if !c.owner && b.isStaff(ctx, id) {
		text += "\n\n" + b.ticketCustomerDetails(ctx, c.ticket)
		assignee := "свободно"
		if c.ticket.AssignedTo != nil {
			assignee = strconv.FormatInt(*c.ticket.AssignedTo, 10)
			if *c.ticket.AssignedTo == id {
				assignee = "вы"
			}
		}
		text += "\nИсполнитель: " + assignee
	}
	if c.ticket.Context != "" {
		text += "\n\n" + escapeHTML(firstRunes(c.ticket.Context, 260))
	}
	for i := len(c.messages) - 1; i >= 0; i-- {
		m := c.messages[i]
		if m.Internal {
			continue
		}
		author := "Клиент"
		if c.owner {
			author = "Вы"
		}
		if m.FromAdmin {
			author = "Поддержка"
		}
		if m.Assistant {
			author = "Помощник"
		}
		text += fmt.Sprintf("\n\n<b>Последнее сообщение · %s</b>\n%s", author, escapeHTML(firstRunes(m.Text, 700)))
		if m.FileID != nil {
			text += "\n📎 Вложение"
		}
		break
	}
	if !c.owner && b.isStaff(ctx, id) {
		if history, e := b.db.StaffTicketHistory(ctx, ticketID); e == nil {
			for i := len(history) - 1; i >= 0; i-- {
				if history[i].Internal {
					text += "\n\n<b>Последняя внутренняя заметка</b>\n" + escapeHTML(firstRunes(history[i].Text, 500))
					break
				}
			}
		}
	}
	rows := [][]telego.InlineKeyboardButton{{button("История переписки", fmt.Sprintf("ticket:page:%d,-1", ticketID))}}
	for _, action := range c.actions {
		rows = append(rows, []telego.InlineKeyboardButton{action})
	}
	if c.ticket.Status == "open" {
		kind, ref := b.input(id)
		if (kind == inputTicket || kind == inputTicketReply) && ref == strconv.FormatInt(ticketID, 10) {
			text += "\n\n<b>Режим ответа · #" + ref + "</b>\nСледующие сообщения отправятся в это обращение. /cancel — выйти."
		}
	}
	return text, keyboard(rows...), nil
}

func (b *Bot) ticketCustomerDetails(ctx context.Context, ticket storage.Ticket) string {
	lines := []string{}
	if ticket.TelegramID != nil {
		lines = append(lines, fmt.Sprintf("Telegram ID: <code>%d</code>", *ticket.TelegramID))
	}
	var subjectID int64
	if ticket.SubjectID != nil {
		subjectID = *ticket.SubjectID
	} else if ticket.TelegramID != nil {
		if s, e := b.subject(ctx, *ticket.TelegramID); e == nil {
			subjectID = s.ID
		}
	}
	if subjectID == 0 {
		return strings.Join(lines, "\n")
	}
	subject, err := b.db.SubjectByID(ctx, subjectID)
	if err != nil {
		return strings.Join(lines, "\n")
	}
	lines = append(lines, fmt.Sprintf("Аккаунт: <code>%d</code>", subject.ID))
	if subject.Name != nil && *subject.Name != "" {
		lines = append(lines, "Клиент: "+escapeHTML(firstRunes(*subject.Name, 70)))
	}
	if b.pay == nil {
		return strings.Join(lines, "\n")
	}
	panelCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	access, panelErr := b.pay.SupportAccess(panelCtx, subject)
	if access.Found {
		lines = append(lines, "Профиль: <code>"+escapeHTML(access.Username)+"</code>", fmt.Sprintf("Remnawave ID: <code>%d</code>", access.PanelUserID))
		if access.PanelUserUUID != "" {
			lines = append(lines, "Remnawave UUID: <code>"+escapeHTML(access.PanelUserUUID)+"</code>")
		}
	} else if subject.RemnawaveUser != nil {
		lines = append(lines, "Профиль: <code>"+escapeHTML(*subject.RemnawaveUser)+"</code>")
	}
	if access.ExpireAt != nil {
		state := "истекла"
		if access.Active {
			state = "активна"
		}
		lines = append(lines, fmt.Sprintf("Подписка %s до %s", state, access.ExpireAt.In(b.timezone()).Format("02.01.2006")))
	}
	if panelErr != nil {
		lines = append(lines, "Панель недоступна: идентификатор не удалось обновить.")
	} else if !access.Found {
		lines = append(lines, "Профиль Remnawave пока не найден.")
	}
	return strings.Join(lines, "\n")
}

func ticketUpdateKeyboard(ticketID int64, open bool) *telego.InlineKeyboardMarkup {
	raw := strconv.FormatInt(ticketID, 10)
	rows := [][]telego.InlineKeyboardButton{}
	if open {
		rows = append(rows, []telego.InlineKeyboardButton{button("Ответить", "ticket:reply:"+raw), button("Проблема решена", "ticket:resolve:"+raw)})
	}
	return keyboard(append(rows, []telego.InlineKeyboardButton{button("История", "ticket:page:"+raw+",-1"), button("Мои обращения", "support:list")})...)
}

// SendTicketUpdate sends only the newly added message, never the transcript.
// The assistant and web server use this optional notifier as well.
func (b *Bot) SendTicketUpdate(ctx context.Context, id, ticketID int64, text, kind string) error {
	ticket, err := b.db.TicketByID(ctx, ticketID)
	if err != nil {
		return err
	}
	ownerID := ticket.TelegramID
	if ticket.SubjectID != nil {
		if s, e := b.db.SubjectByID(ctx, *ticket.SubjectID); e == nil && s.TelegramID != nil {
			ownerID = s.TelegramID
		}
	}
	if ownerID == nil || *ownerID != id {
		return fmt.Errorf("получатель не владелец обращения")
	}
	label := "Поддержка"
	if kind == "assistant" {
		label = "Помощник"
	}
	if kind == "handoff" {
		label = "Статус обращения"
	}
	parts := ticketTextParts(text, 3000)
	if kind == "assistant" {
		parts = mdhtml.Chunks(text, 3000)
	}
	for i, part := range parts {
		body := fmt.Sprintf("<b>%s · #%d</b>\n\n%s", label, ticketID, part)
		var kb *telego.InlineKeyboardMarkup
		if i == len(parts)-1 {
			kb = ticketUpdateKeyboard(ticketID, ticket.Status == "open")
		}
		if err = b.deliverHTML(ctx, telego.ChatID{ID: id}, 0, body, kb); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) ticketSent(ctx context.Context, id, ticketID int64, created bool) {
	label := "Сообщение отправлено"
	if created {
		label = "Обращение создано"
	}
	text := fmt.Sprintf("<b>%s · #%d</b>\nМожно отправить ещё сообщение или вложение. /cancel — выйти из режима ответа.", label, ticketID)
	b.upsertTicket(ctx, telego.ChatID{ID: id}, 0, text, ticketUpdateKeyboard(ticketID, true))
}

func (b *Bot) deliverSupportFile(ctx context.Context, id int64, fileID, fileType string) error {
	chat := telego.ChatID{ID: id}
	if fileType == "photo" {
		_, err := b.api.SendPhoto(ctx, &telego.SendPhotoParams{ChatID: chat, Photo: telego.InputFile{FileID: fileID}})
		return err
	}
	_, err := b.api.SendDocument(ctx, &telego.SendDocumentParams{ChatID: chat, Document: telego.InputFile{FileID: fileID}})
	return err
}
