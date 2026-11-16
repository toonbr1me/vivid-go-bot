package bot

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/mymmrac/telego"
)

type ticketThreadPage struct {
	html        string
	attachments []telego.InlineKeyboardButton
}

// Keep the conversation in one expandable Telegram quote. Only split when the
// transcript exceeds the message limit; markup and escaping do not consume it.
func (b *Bot) ticketThreadPages(c ticketConversation) []ticketThreadPage {
	const budget = 3300 // Reserve space for the title, context and reply hint.
	pages := []ticketThreadPage{{}}
	used := 0
	for _, m := range c.messages {
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
		text := m.Text
		if m.FileID != nil {
			label := "Документ"
			if m.FileType != nil && *m.FileType == "photo" {
				label = "Фото"
			}
			text += "\n📎 " + label
		}
		parts := ticketTextParts(text, budget-150)
		for i, part := range parts {
			label := author
			if i > 0 {
				label += " · продолжение"
			}
			prefix := label + " · " + m.CreatedAt.In(b.timezone()).Format("02.01.2006 15:04") + "\n"
			entry := "<b>" + label + "</b> · " + m.CreatedAt.In(b.timezone()).Format("02.01.2006 15:04") + "\n" + part + "\n\n"
			size := ticketTextLength(prefix + html.UnescapeString(part) + "\n\n")
			current := &pages[len(pages)-1]
			if used+size > budget || len(current.attachments) >= 80 {
				pages = append(pages, ticketThreadPage{})
				current = &pages[len(pages)-1]
				used = 0
			}
			current.html += entry
			used += size
			if m.FileID != nil && i == len(parts)-1 {
				current.attachments = append(current.attachments, button(fmt.Sprintf("📎 Вложение #%d", m.ID), "ticket:file:"+strconv.FormatInt(m.ID, 10)))
			}
		}
	}
	if len(c.messages) == 0 {
		pages[0].html = "Сообщений пока нет.\n"
	} else {
		for i := range pages {
			pages[i].html = "<blockquote expandable>" + strings.TrimRight(pages[i].html, "\n") + "</blockquote>\n"
		}
	}
	return pages
}

func ticketTextParts(text string, limit int) []string {
	parts := []string{}
	var chunk strings.Builder
	used := 0
	for _, r := range text {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if used+size > limit {
			parts = append(parts, chunk.String())
			chunk.Reset()
			used = 0
		}
		chunk.WriteString(escapeHTML(string(r)))
		used += size
	}
	return append(parts, chunk.String())
}

func ticketTextLength(text string) int { return len(utf16.Encode([]rune(text))) }

// Native HTML carries Telegram's expandable_blockquote entity. Sending this
// through the rich-document renderer would turn it into an ordinary quote.
func (b *Bot) deliverTicket(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) error {
	if b.viewMode(ctx, chatID.ID) == "cards" {
		return b.deliver(ctx, chatID, messageID, text, kb)
	}
	return b.deliverHTML(ctx, chatID, messageID, text, kb)
}

func (b *Bot) upsertTicket(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) {
	if err := b.deliverTicket(ctx, chatID, messageID, text, kb); err != nil {
		b.logError("не удалось показать историю обращения", err)
	}
}
