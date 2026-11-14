package bot

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
)

type incomingMessageKey struct{}
type deliveryMarkKey struct{}
type deliveryMark struct{ success bool }
type keepMessageKey struct{}

func markDelivered(ctx context.Context) {
	if mark, ok := ctx.Value(deliveryMarkKey{}).(*deliveryMark); ok {
		mark.success = true
	}
}

// Delete only the incoming private message whose content has been handled.
// Callers processing support invoke this after the database write succeeds.
func (b *Bot) cleanIncoming(ctx context.Context) {
	m, ok := ctx.Value(incomingMessageKey{}).(telego.Message)
	if !ok || b.cfg == nil || !b.cfg.BotCleanChat || m.From == nil || m.Chat.ID != m.From.ID {
		return
	}
	if err := b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: m.Chat.ID}, MessageID: m.MessageID}); err != nil {
		slog.Debug("сообщение не удалено", "message", m.MessageID, "err", err)
	}
}

func (b *Bot) cleanCommand(handler func(*telegohandler.Context, telego.Message) error) func(*telegohandler.Context, telego.Message) error {
	return func(ctx *telegohandler.Context, m telego.Message) error {
		ctx = ctx.WithContext(context.WithValue(ctx, incomingMessageKey{}, m))
		mark := &deliveryMark{}
		ctx = ctx.WithContext(context.WithValue(ctx, deliveryMarkKey{}, mark))
		err := handler(ctx, m)
		if err == nil && mark.success && strings.HasPrefix(m.Text, "/") {
			b.cleanIncoming(ctx)
			if b.cfg.BotCleanChat && b.db != nil && m.From != nil && m.Chat.ID == m.From.ID {
				b.retireOldInterface(ctx, m.From.ID)
			}
		}
		return err
	}
}

func (b *Bot) retireOldInterface(ctx context.Context, id int64) {
	remove := func(message int) {
		if message > 0 {
			_ = b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: id}, MessageID: message})
		}
	}
	if view, err := b.db.BotView(ctx, id); err == nil && view.SelectorMessageID > 0 {
		remove(view.SelectorMessageID)
		_ = b.db.SetBotSelector(ctx, id, 0)
	}
	if b.viewMode(ctx, id) == "cards" {
		old := b.db.NativeMessageID(ctx, id)
		if old > 0 {
			remove(old)
			_ = b.db.SaveNativeMessage(ctx, id, 0)
		}
	} else {
		old, _, _ := b.db.BotCard(ctx, id)
		if old > 0 && old != b.db.NativeMessageID(ctx, id) {
			remove(old)
			_ = b.db.SaveBotCard(ctx, id, 0, map[string]any{})
		}
	}
}

func (b *Bot) cleanNativeEnabled(ctx context.Context, id int64) bool {
	keep, _ := ctx.Value(keepMessageKey{}).(bool)
	return !keep && b.db != nil && b.cfg != nil && b.cfg.BotCleanChat && id > 0 && b.viewMode(ctx, id) != "cards"
}
func (b *Bot) nativeTarget(ctx context.Context, id int64, message int) int {
	if message == 0 && !forceNewCard(ctx) && b.cleanNativeEnabled(ctx, id) {
		return b.db.NativeMessageID(ctx, id)
	}
	return message
}
func (b *Bot) rememberNative(ctx context.Context, id int64, message int) error {
	markDelivered(ctx)
	if !b.cleanNativeEnabled(ctx, id) || message <= 0 {
		return nil
	}
	old := b.db.NativeMessageID(ctx, id)
	if err := b.db.SaveNativeMessage(ctx, id, message); err != nil {
		return err
	}
	if old > 0 && old != message {
		_ = b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: id}, MessageID: old})
	}
	return nil
}

// Continue editing the current cabinet when text arrives from the chat.
func (b *Bot) currentCardID(ctx context.Context, id int64) int {
	if b.db == nil || b.viewMode(ctx, id) != "cards" {
		return 0
	}
	messageID, _, _ := b.db.BotCard(ctx, id)
	return messageID
}
