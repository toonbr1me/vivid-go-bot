package bot

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
)

// Every saved message is a page, so the entire conversation is recoverable
// without filling the Telegram chat with copies of it.
func (b *Bot) ticketCard(ctx context.Context, id, ticketID int64) (cardState, error) {
	ticket, err := b.db.TicketByID(ctx, ticketID)
	if err != nil {
		return cardState{}, err
	}
	subject, err := b.subject(ctx, id)
	if err != nil {
		return cardState{}, err
	}
	owner := supportTicketOwned(ticket, subject)
	if !owner && !b.isStaff(ctx, id) {
		return cardState{}, fmt.Errorf("обращение вам не принадлежит")
	}
	messages, err := b.db.TicketMessages(ctx, ticketID, 500)
	if err != nil {
		return cardState{}, err
	}
	actions := []telego.InlineKeyboardButton{}
	if ticket.Status == "open" {
		actions = append(actions, button("Ответить", "ticket:reply:"+strconv.FormatInt(ticketID, 10)))
		if owner {
			actions = append(actions, button("Проблема решена", "ticket:resolve:"+strconv.FormatInt(ticketID, 10)))
		}
	}
	actions = append(actions, button("Все обращения", "support:list"))
	result := cardState{Actions: actions}
	if ticket.Context != "" {
		result.Pages = append(result.Pages, cardPage{Title: fmt.Sprintf("Обращение #%d", ticketID), Tab: "support", Body: ticket.Context, Status: ticketStatusLabel(ticket.Status)})
	}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		author := "Вы"
		if m.FromAdmin {
			author = "Поддержка"
		}
		text := mdhtml.Plain(m.Text)
		messageActions := append([]telego.InlineKeyboardButton{}, actions...)
		fileIndex := -1
		if m.FileID != nil {
			text += "\nВложение: фото или документ"
			fileIndex = len(result.Actions)
			result.Actions = append(result.Actions, button("Открыть вложение", "ticket:file:"+strconv.FormatInt(m.ID, 10)))
		}
		chunks := textCard("Сообщение\n"+text, nil)
		for _, chunk := range chunks.Pages {
			p := cardPage{Title: fmt.Sprintf("Обращение #%d", ticketID), Tab: "support", Status: ticketStatusLabel(ticket.Status), Kicker: author, Value: dateTimeText(m.CreatedAt), Body: chunk.Body}
			for j, a := range messageActions {
				p.Buttons = append(p.Buttons, cardButton{Text: cardLabel(a.Text), Index: j})
			}
			if fileIndex >= 0 {
				p.Buttons = append(p.Buttons, cardButton{Text: "Открыть вложение", Index: fileIndex})
			}
			result.Pages = append(result.Pages, p)
		}
	}
	if len(result.Pages) == 0 {
		result = cardWithButtons(cardPage{Title: fmt.Sprintf("Обращение #%d", ticketID), Tab: "support", Note: "Сообщений пока нет"}, actions)
	}
	for i := range result.Pages {
		result.Pages[i].Page = i
		result.Pages[i].Pages = len(result.Pages)
	}
	result.Current = len(result.Pages) - 1
	return result, nil
}

func ticketStatusLabel(status string) string {
	if status == "open" {
		return "Открыто"
	}
	return "Закрыто"
}

func (b *Bot) showSavedTicket(ctx context.Context, id, ticketID int64) {
	if b.viewMode(ctx, id) == "cards" {
		if state, err := b.ticketCard(ctx, id, ticketID); err == nil {
			if err = b.deliverUI(ctx, id, b.currentCardID(ctx, id), state); err == nil {
				return
			}
		}
	}
	text, kb := b.ticketScreen(ctx, id, ticketID)
	b.send(ctx, telego.ChatID{ID: id}, text, kb)
}
