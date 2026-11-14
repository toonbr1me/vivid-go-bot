package bot

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/mymmrac/telego"
)

// Production widgets are protected. Telegram Web currently refuses their
// clipboard calls without returning an API error; use a native copy button.
func (b *Bot) sendCardCopy(ctx context.Context, id int64, value string) error {
	title, label := "Данные для копирования", "Скопировать"
	if strings.HasPrefix(value, "https://") {
		title = "Ссылка для подключения"
		label = "Скопировать ссылку"
	}
	rows := [][]telego.InlineKeyboardButton{}
	if len(utf16.Encode([]rune(value))) <= 256 {
		rows = append(rows, []telego.InlineKeyboardButton{{Text: label, CopyText: &telego.CopyTextButton{Text: value}, Style: "primary"}})
	}
	if telegramURL(value) {
		rows = append(rows, []telego.InlineKeyboardButton{urlButton("Открыть страницу подписки", value)})
	}
	_, err := b.api.SendMessage(ctx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: id}, Text: "<b>" + title + "</b>\n\n<code>" + escapeHTML(value) + "</code>", ParseMode: telego.ModeHTML, ReplyMarkup: keyboard(rows...), LinkPreviewOptions: disabledPreview()})
	return err
}

// The same connection journey has two renderers. Classic never sends widgets.
func nativeCardScreen(state cardState) (string, *telego.InlineKeyboardMarkup) {
	page := state.Pages[state.Current]
	lines := []string{"<b>" + escapeHTML(page.Title) + "</b>"}
	if page.Status != "" {
		lines = append(lines, escapeHTML(page.Status))
	}
	if page.Kicker != "" {
		lines = append(lines, escapeHTML(page.Kicker))
	}
	if page.Step > 0 {
		lines = append(lines, fmt.Sprintf("Шаг %d из 2", page.Step))
	}
	if page.Value != "" {
		lines = append(lines, "", "<b>"+escapeHTML(page.Value)+"</b>")
	}
	if page.Note != "" {
		lines = append(lines, escapeHTML(page.Note))
	}
	if page.Body != "" {
		lines = append(lines, "", escapeHTML(page.Body))
	}
	for _, stat := range page.Stats {
		lines = append(lines, "<b>"+escapeHTML(stat.Label)+":</b> "+escapeHTML(stat.Value))
	}
	if page.Updated != "" {
		lines = append(lines, "", "Данные на "+escapeHTML(page.Updated))
	}
	rows := [][]telego.InlineKeyboardButton{}
	for _, control := range page.Buttons {
		action := state.Actions[control.Index]
		if action.CopyText != nil {
			action.Text = "Скопировать"
			if strings.HasPrefix(action.CopyText.Text, "https://") {
				action.Text = "Скопировать ссылку"
			}
		}
		action.Style = control.Style
		if control.Checked != nil {
			prefix := "☐ "
			if *control.Checked {
				prefix = "☑ "
			}
			action.Text = prefix + control.Text
			action.Style = ""
		}
		if page.Layout == "platform" && len(rows) > 0 && len(rows[len(rows)-1]) == 1 {
			rows[len(rows)-1] = append(rows[len(rows)-1], action)
		} else {
			rows = append(rows, []telego.InlineKeyboardButton{action})
		}
	}
	if page.Back > 0 {
		action := state.Actions[page.Back-1]
		action.Text = "← Назад"
		rows = append(rows, []telego.InlineKeyboardButton{action})
	}
	if page.Refresh > 0 {
		action := state.Actions[page.Refresh-1]
		action.Text = "Обновить"
		rows = append(rows, []telego.InlineKeyboardButton{action})
	}
	rows = append(rows, []telego.InlineKeyboardButton{button("Главное меню", "nav:main"), button("Помощь", "nav:support")})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

func (b *Bot) deliverJourney(ctx context.Context, id int64, messageID int, state cardState) error {
	if b.viewMode(ctx, id) == "cards" {
		return b.deliverUI(ctx, id, messageID, state)
	}
	text, kb := nativeCardScreen(state)
	if b.richAvailable() {
		html, richKB := richCardDocument(state)
		if delivered, _ := b.deliverRich(ctx, telego.ChatID{ID: id}, messageID, html, richKB); delivered {
			return nil
		}
	}
	return b.deliverLegacy(ctx, telego.ChatID{ID: id}, messageID, text, kb)
}
