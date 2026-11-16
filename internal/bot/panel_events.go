package bot

import (
	"context"
	"errors"
	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"time"
)

func (b *Bot) ProcessPanelEvents(ctx context.Context, limit int) error {
	if err := b.db.RecoverPanelEvents(ctx); err != nil {
		return err
	}
	for i := 0; i < limit; i++ {
		e, err := b.db.ClaimPanelEvent(ctx)
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if e.Status != "sending" {
			continue
		}
		text := "К подписке подключено новое устройство."
		if e.DeviceLabel != "" {
			text += "\n" + e.DeviceLabel
		}
		text += "\n\nЕсли вы его не узнаёте, проверьте устройства и обратитесь в поддержку."
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err = b.api.SendMessage(sendCtx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: e.TelegramID}, Text: text, ReplyMarkup: keyboard([]telego.InlineKeyboardButton{button("Устройства", "devices:page:0"), button("Поддержка", "nav:support")})})
		cancel()
		status, retry := deliveryResult(err)
		if err = b.db.FinishPanelEvent(ctx, e, status, retry); err != nil {
			return err
		}
		if status == "pending" {
			return nil
		}
	}
	return nil
}
