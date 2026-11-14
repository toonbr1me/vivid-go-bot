package bot

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
)

func (b *Bot) viewMode(ctx context.Context, id int64) string {
	if b.cfg != nil && b.cfg.BotStyle != "" {
		if b.cfg.BotStyle == "html" && id > 0 {
			return "cards"
		}
		return "classic"
	}
	if b.db == nil {
		return "rich"
	} // Транспорт без хранилища сохраняет прежний контракт.
	if id <= 0 {
		return "classic"
	}
	view, err := b.db.BotView(ctx, id)
	if err != nil {
		slog.Warn("режим интерфейса недоступен", "err", err)
		return "classic"
	}
	return view.Mode
}

func (b *Bot) handleViewCommand(ctx *telegohandler.Context, m telego.Message) error {
	if m.From != nil && m.Chat.ID == m.From.ID {
		return b.showViewSelector(ctx, m.From.ID)
	}
	return nil
}

func (b *Bot) showViewSelector(ctx context.Context, id int64) error {
	if b.cfg != nil && b.cfg.BotStyle != "" {
		b.showCommandScreen(ctx, telego.ChatID{ID: id}, "main", id)
		return nil
	}
	view, err := b.db.BotView(ctx, id)
	if err != nil {
		return err
	}
	classic, newLabel := "Обычный", "Новый"
	if view.Mode == "cards" {
		newLabel = "✓ " + newLabel
	} else {
		classic = "✓ " + classic
	}
	kb := keyboard([]telego.InlineKeyboardButton{
		{Text: classic, CallbackData: "view:classic", Style: "primary"},
		{Text: newLabel, CallbackData: "view:cards", Style: "success"},
	}, []telego.InlineKeyboardButton{urlButton("Обновить Telegram", "https://telegram.org/apps")})
	text := "<b>Выберите вид бота</b>\n\n" +
		"<b>Обычный</b> — rich-сообщения и кнопки Telegram.\n" +
		"<b>Новый</b> — интерактивная карточка в чате.\n\n" +
		"Для нового вида обновите Telegram. Выбор можно изменить здесь в любой момент."
	if view.SelectorMessageID > 0 {
		err = b.deliverHTML(ctx, telego.ChatID{ID: id}, view.SelectorMessageID, text, kb)
	}
	if view.SelectorMessageID == 0 || err != nil {
		message, sendErr := b.api.SendMessage(ctx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: id}, Text: text, ParseMode: telego.ModeHTML, ReplyMarkup: kb})
		if sendErr != nil {
			return sendErr
		}
		view.SelectorMessageID = message.MessageID
		if err := b.db.SetBotSelector(ctx, id, message.MessageID); err != nil {
			return err
		}
	}
	if err := b.api.PinChatMessage(ctx, &telego.PinChatMessageParams{ChatID: telego.ChatID{ID: id}, MessageID: view.SelectorMessageID, DisableNotification: true}); err != nil {
		slog.Warn("не удалось закрепить выбор вида бота", "err", err)
	}
	return nil
}

// Legacy formatter retained for old content-size checks. Interactive screens
// use interfaceWidgetHTML and private, versioned state instead.
func cardHTML(text string) (string, error) {
	body := screenRichHTML(text)
	body = strings.NewReplacer("<tg-spoiler>", "<span>", "</tg-spoiler>", "</span>").Replace(body)
	html := `<!doctype html><html><head><style>body{margin:16px;color:var(--tg-theme-text-color)}h3{font-size:20px;margin:0 0 14px;color:var(--tg-theme-accent-text-color)}p{line-height:1.5;margin:10px 0}a{color:var(--tg-theme-link-color)}pre,blockquote{padding:12px;border-radius:10px;background:var(--tg-theme-secondary-bg-color)}pre{white-space:pre-wrap;word-break:break-word}code{font-size:13px}footer{color:var(--tg-theme-hint-color);font-size:12px}</style></head><body>` + body + `</body></html>`
	var buf bytes.Buffer
	w, _ := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	_, _ = w.Write([]byte(html))
	_ = w.Close()
	if len(html) > 32768 || buf.Len() > 4096 {
		return "", fmt.Errorf("карточка превышает лимит Telegram")
	}
	return html, nil
}

func (b *Bot) deliverCard(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) error {
	if kb != nil {
		for _, row := range kb.InlineKeyboard {
			for _, button := range row {
				if button.CallbackData == "" && button.URL == "" && button.CopyText == nil {
					return fmt.Errorf("действие требует штатной клавиатуры Telegram")
				}
			}
		}
	}
	return b.deliverUI(ctx, chatID.ID, messageID, textCard(text, kb))
}
