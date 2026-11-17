package bot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

var broadcastPromo = regexp.MustCompile(`(?i)(?:^|\s)promo=([^\s]+)`)

func parseBroadcast(raw string) (string, string, error) {
	code := ""
	matches := broadcastPromo.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) > 1 {
		return "", "", fmt.Errorf("Укажите один promo=КОД.")
	}
	if len(matches) == 1 {
		m := matches[0]
		var ok bool
		code, ok = domain.NormalizePromoCode(raw[m[2]:m[3]])
		if !ok {
			return "", "", fmt.Errorf("Неверный формат промокода.")
		}
		raw = raw[:m[0]] + " " + raw[m[1]:]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || len(utf16.Encode([]rune(raw))) > 3000 {
		return "", "", fmt.Errorf("Пришлите текст от 1 до 3000 символов.")
	}
	return raw, code, nil
}

func (b *Bot) handleAnnounce(ctx *telegohandler.Context, m telego.Message) error {
	if m.From == nil || m.Chat.ID != m.From.ID {
		return nil
	}
	b.broadcastCallback(ctx, "", telego.ChatID{ID: m.From.ID}, 0, m.From.ID, "home", "")
	return nil
}

func (b *Bot) broadcastCallback(ctx context.Context, q string, chat telego.ChatID, mid int, actor int64, action, ref string) {
	if chat.ID != actor || !b.db.Permission(ctx, actor, "broadcast.send", b.cfg.Current().AdminIDs) {
		b.answer(ctx, q, "Недостаточно прав", true)
		return
	}
	switch action {
	case "audience":
		if !storage.ValidAudience(ref) {
			b.answer(ctx, q, "Неизвестная аудитория", true)
			return
		}
		b.setInput(actor, inputBroadcast, ref)
		b.upsert(ctx, chat, mid, "<b>Текст рассылки</b>\nПришлите обычный текст до 3000 символов. Для кнопки промокода добавьте promo=КОД.\nПосле этого появится предпросмотр с числом получателей.", keyboard([]telego.InlineKeyboardButton{button("Отмена", "ann:home")}))
	case "send", "cancel", "status":
		if action == "send" {
			if _, err := b.db.ConfirmBroadcast(ctx, ref, actor); err != nil {
				b.answer(ctx, q, "Не удалось подтвердить рассылку", true)
				return
			}
		} else if action == "cancel" {
			if _, err := b.db.CancelBroadcast(ctx, ref, actor); err != nil {
				b.answer(ctx, q, "Не удалось отменить черновик", true)
				return
			}
		}
		b.showBroadcast(ctx, chat, mid, actor, ref)
	default:
		b.clearInput(actor)
		rows := [][]telego.InlineKeyboardButton{{button("Все", "ann:audience:all"), button("С подпиской", "ann:audience:active")}, {button("Без подписки", "ann:audience:inactive")}}
		rows = append(rows, []telego.InlineKeyboardButton{button("Пробовали без покупки", "ann:audience:trial_only"), button("Бросили оплату", "ann:audience:abandoned")}, []telego.InlineKeyboardButton{button("Подписка истекла", "ann:audience:expired"), button("Не подключились", "ann:audience:not_connected")})
		if b.siteURL() != "" {
			rows = append(rows, []telego.InlineKeyboardButton{urlButton("Поведенческие кампании на сайте", b.siteURL()+"/admin/campaigns")})
		}
		recent, err := b.db.RecentBroadcasts(ctx, actor)
		if err != nil {
			b.answer(ctx, q, "Не удалось загрузить рассылки", true)
			return
		}
		for _, x := range recent {
			rows = append(rows, []telego.InlineKeyboardButton{button(fmt.Sprintf("%s · %d получателей", broadcastStatus(x.Status), x.Total), "ann:status:"+x.ID)})
		}
		rows = append(rows, []telego.InlineKeyboardButton{button("Панель", "admin:home")})
		b.upsert(ctx, chat, mid, "<b>Рассылка</b>\nВыберите аудиторию. Отправка начнётся после предпросмотра и подтверждения. Список получателей фиксируется при создании черновика.", keyboard(rows...))
	}
	b.answer(ctx, q, "", false)
}

func (b *Bot) handleBroadcastInput(ctx context.Context, chat telego.ChatID, actor int64, audience, raw string) {
	if chat.ID != actor || !b.db.Permission(ctx, actor, "broadcast.send", b.cfg.Current().AdminIDs) {
		b.clearInput(actor)
		return
	}
	body, promo, err := parseBroadcast(raw)
	if err == nil && promo != "" {
		p, e := b.db.PromoCode(ctx, promo)
		if e != nil || !p.IsActive || !p.Live(time.Now()) || p.Exhausted() {
			err = fmt.Errorf("Промокод не найден, закончился или неактивен.")
		}
	}
	if err != nil {
		b.send(ctx, chat, escapeHTML(err.Error()), keyboard([]telego.InlineKeyboardButton{button("Отмена", "ann:home")}))
		return
	}
	draft, err := b.db.CreateBroadcast(ctx, actor, audience, body, promo)
	if err != nil {
		b.send(ctx, chat, "Не удалось сохранить черновик. Попробуйте ещё раз.", keyboard(b.backRow()))
		return
	}
	b.clearInput(actor)
	b.cleanIncoming(ctx)
	b.showBroadcast(ctx, chat, 0, actor, draft.ID)
}

func broadcastStatus(s string) string {
	switch s {
	case "draft":
		return "Черновик"
	case "queued":
		return "В очереди"
	case "sending":
		return "Отправляется"
	case "completed":
		return "Завершена"
	case "cancelled":
		return "Отменена"
	}
	return s
}
func (b *Bot) showBroadcast(ctx context.Context, chat telego.ChatID, mid int, actor int64, id string) {
	x, err := b.db.Broadcast(ctx, id, actor)
	if err != nil {
		b.upsert(ctx, chat, mid, "Рассылка не найдена.", keyboard([]telego.InlineKeyboardButton{button("Рассылки", "ann:home")}))
		return
	}
	text := fmt.Sprintf("<b>%s</b> · получателей: %d\n\n%s", broadcastStatus(x.Status), x.Total, escapeHTML(x.Text))
	if x.PromoCode != "" {
		text += "\n\nКнопка: Применить " + escapeHTML(x.PromoCode)
	}
	rows := [][]telego.InlineKeyboardButton{}
	if x.Status == "draft" {
		text += "\n\nПодтверждение доступно 20 минут. После отправки отмена недоступна."
		if x.Total > 0 {
			rows = append(rows, []telego.InlineKeyboardButton{button(fmt.Sprintf("Отправить · %d", x.Total), "ann:send:"+id)})
		}
		rows = append(rows, []telego.InlineKeyboardButton{button("Отменить черновик", "ann:cancel:"+id)})
	} else {
		text += fmt.Sprintf("\n\nДоставлено: %d · ошибок: %d · без подтверждения: %d · ожидают: %d", x.Sent, x.Failed, x.Uncertain, x.Pending)
		text += fmt.Sprintf(" · пропущено: %d", x.Skipped)
		rows = append(rows, []telego.InlineKeyboardButton{button("Обновить", "ann:status:"+id)})
	}
	rows = append(rows, []telego.InlineKeyboardButton{button("Рассылки", "ann:home")})
	b.upsert(ctx, chat, mid, text, keyboard(rows...))
}

// Telegram cannot deduplicate sendMessage. An ambiguous result is recorded for
// review, never blindly resent; only an explicit flood-control rejection retries.
func deliveryResult(err error) (string, time.Duration) {
	if err == nil {
		return "sent", 0
	}
	var apiErr *telegoapi.Error
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode == 429 && apiErr.Parameters != nil && apiErr.Parameters.RetryAfter > 0 {
			return "pending", time.Duration(apiErr.Parameters.RetryAfter) * time.Second
		}
		if apiErr.ErrorCode >= 400 && apiErr.ErrorCode < 500 {
			return "failed", 0
		}
	}
	return "uncertain", 0
}

func (b *Bot) ProcessBroadcasts(ctx context.Context, limit int) error {
	if err := b.db.RecoverBroadcastDeliveries(ctx); err != nil {
		return err
	}
	for i := 0; i < limit; i++ {
		d, err := b.db.ClaimBroadcast(ctx)
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !b.db.Permission(ctx, d.ActorID, "broadcast.send", b.cfg.Current().AdminIDs) {
			if err = b.db.BlockBroadcast(ctx, d.BroadcastID); err != nil {
				return err
			}
			continue
		}
		now := time.Now()
		if zone := b.cfg.Current().Timezone; zone != "" {
			location, e := time.LoadLocation(zone)
			if e != nil {
				if finishErr := b.db.FinishBroadcastDelivery(ctx, d, "pending", time.Minute); finishErr != nil {
					return finishErr
				}
				return e
			}
			now = now.In(location)
		}
		allowed, wait, err := b.db.BroadcastDeliveryReady(ctx, d, now)
		if err != nil {
			if e := b.db.FinishBroadcastDelivery(ctx, d, "pending", time.Minute); e != nil {
				return e
			}
			return err
		}
		if wait > 0 {
			if err = b.db.FinishBroadcastDelivery(ctx, d, "pending", wait); err != nil {
				return err
			}
			continue
		}
		if !allowed {
			if err = b.db.FinishBroadcastDelivery(ctx, d, "skipped", 0); err != nil {
				return err
			}
			continue
		}
		params := &telego.SendMessageParams{ChatID: telego.ChatID{ID: d.TelegramID}, Text: d.Text}
		if d.PromoCode != "" {
			params.ReplyMarkup = keyboard([]telego.InlineKeyboardButton{button("Применить "+d.PromoCode, "promo:use:"+d.PromoCode)})
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err = b.api.SendMessage(sendCtx, params)
		cancel()
		status, retry := deliveryResult(err)
		if err = b.db.FinishBroadcastDelivery(ctx, d, status, retry); err != nil {
			return err
		}
		if status == "pending" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}
