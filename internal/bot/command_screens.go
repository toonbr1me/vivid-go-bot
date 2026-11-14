package bot

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	"github.com/mymmrac/telego"
)

type newCardKey struct{}

func forceNewCard(ctx context.Context) bool { value, _ := ctx.Value(newCardKey{}).(bool); return value }

// Commands recreate the configured interface at the bottom of a clean chat.
// The previous screen is removed only after the replacement is persisted.
func (b *Bot) showCommandScreen(ctx context.Context, chatID telego.ChatID, name string, id int64) {
	if b.cfg.BotCleanChat {
		ctx = context.WithValue(ctx, newCardKey{}, true)
	}
	if b.db == nil || chatID.ID != id || b.viewMode(ctx, id) != "cards" {
		b.showScreen(ctx, chatID, 0, name, id)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	unlock, err := b.db.LockBotCard(ctx, id)
	if err != nil {
		_, _ = b.api.SendMessage(ctx, &telego.SendMessageParams{ChatID: chatID, Text: "Не удалось открыть кабинет. Попробуйте ещё раз."})
		return
	}
	defer unlock()
	previousID, previous, _ := b.db.BotCard(ctx, id)
	if b.cfg.BotCleanChat {
		ctx = context.WithValue(ctx, newCardKey{}, true)
	}
	b.showScreen(ctx, chatID, 0, name, id)
	if b.cfg.BotCleanChat {
		return
	}
	currentID, current, err := b.db.BotCard(ctx, id)
	if err != nil || currentID != previousID || currentID <= 0 || bytes.Equal(previous, current) {
		return
	}
	_, err = b.api.SendMessage(ctx, &telego.SendMessageParams{
		ChatID:          chatID,
		Text:            "HTML-кабинет обновлён.",
		ReplyParameters: &telego.ReplyParameters{MessageID: currentID, AllowSendingWithoutReply: true},
	})
	if err != nil {
		slog.Warn("ответ на команду не доставлен", "err", err)
	}
}
