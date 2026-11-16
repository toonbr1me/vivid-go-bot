package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type ticketConversation struct {
	ticket   storage.Ticket
	messages []storage.Message
	owner    bool
	actions  []telego.InlineKeyboardButton
}

func (b *Bot) loadTicketConversation(ctx context.Context, id, ticketID int64) (ticketConversation, error) {
	ticket, err := b.db.TicketByID(ctx, ticketID)
	if err != nil {
		return ticketConversation{}, err
	}
	subject, err := b.subject(ctx, id)
	if err != nil {
		return ticketConversation{}, err
	}
	owner := supportTicketOwned(ticket, subject)
	staff := b.isStaff(ctx, id)
	if !owner && !staff {
		return ticketConversation{}, fmt.Errorf("обращение вам не принадлежит")
	}
	messages, err := b.db.TicketHistory(ctx, ticketID)
	if err != nil {
		return ticketConversation{}, err
	}
	actions := []telego.InlineKeyboardButton{}
	if ticket.Status == "open" {
		actions = append(actions, button("Ответить", "ticket:reply:"+strconv.FormatInt(ticketID, 10)))
		if owner {
			actions = append(actions, button("Проблема решена", "ticket:resolve:"+strconv.FormatInt(ticketID, 10)))
		}
		if staff {
			actions = append(actions, button("Закрыть обращение", "ticket:close:"+strconv.FormatInt(ticketID, 10)))
			if b.db.Permission(ctx, id, "support.compensate", b.cfg.AdminIDs) {
				if _, e := b.db.TicketCompensation(ctx, ticketID); errors.Is(e, storage.ErrNotFound) {
					actions = append(actions, button("Компенсация · 3 дня", "ticket:compensate:"+strconv.FormatInt(ticketID, 10)))
				}
			}
		}
	}
	back := b.ticketListBack(id, false)
	b.mu.Lock()
	lastList := b.ticketLists[id]
	b.mu.Unlock()
	if staff && (!owner || lastList.staff) {
		back = b.ticketListBack(id, true)
	}
	actions = append(actions, button("Все обращения", back))
	return ticketConversation{ticket: ticket, messages: messages, owner: owner, actions: actions}, nil
}

// The compact widget retains message pages; the ordinary Telegram interface
// renders the same saved conversation as a chronological chat transcript.
func (b *Bot) ticketCard(ctx context.Context, id, ticketID int64) (cardState, error) {
	conversation, err := b.loadTicketConversation(ctx, id, ticketID)
	if err != nil {
		return cardState{}, err
	}
	ticket, messages, owner, actions := conversation.ticket, conversation.messages, conversation.owner, conversation.actions
	result := cardState{Actions: actions}
	if ticket.Context != "" {
		result.Pages = append(result.Pages, cardPage{Title: fmt.Sprintf("Обращение #%d", ticketID), Tab: "support", Body: ticket.Context, Status: ticketStatusLabel(ticket.Status)})
	}
	for _, m := range messages {
		author := "Вы"
		if !owner {
			author = "Клиент"
		}
		if m.FromAdmin {
			author = "Поддержка"
		}
		if m.Assistant {
			author = "Помощник"
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
	text, kb := b.ticketScreen(ctx, id, ticketID)
	b.upsertTicket(ctx, telego.ChatID{ID: id}, 0, text, kb)
}
