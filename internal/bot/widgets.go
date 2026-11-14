package bot

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

//go:embed widgets/subscription.html
var subscriptionWidgetHTML string

const subscriptionWidgetRefresh = "subscription:refresh"

// Персональные сведения передаются только в chat_data, никогда в public_data.
// Номер пользователя из JavaScript не принимается: владелец — query.From.
type subscriptionWidgetData struct {
	Status  string `json:"status"`
	Until   string `json:"until"`
	Devices string `json:"devices"`
	Traffic string `json:"traffic"`
	Updated string `json:"updated"`
	Error   string `json:"error,omitempty"`
}

func widgetDataForAccess(access domain.Access, accessErr error) subscriptionWidgetData {
	data := subscriptionWidgetData{Status: "Нет подписки", Until: "—", Devices: "—", Traffic: "—", Updated: time.Now().Format("15:04")}
	if access.Active {
		data.Status = "Активна"
	} else if access.ExpireAt != nil {
		data.Status = "Истекла"
	}
	if access.ExpireAt != nil {
		data.Until = dateTimeText(*access.ExpireAt)
	}
	if access.Found {
		data.Devices = fmt.Sprintf("%d подключено", len(access.Devices))
		if access.DeviceLimit > 0 {
			data.Devices = fmt.Sprintf("%d из %d", len(access.Devices), access.DeviceLimit)
		}
		data.Traffic = bytesText(access.TrafficUsedBytes)
		if access.TrafficLimit != nil && *access.TrafficLimit > 0 {
			data.Traffic += " из " + bytesText(*access.TrafficLimit)
		}
	}
	if accessErr != nil {
		data.Error = "Часть данных недоступна. Попробуйте обновить позже."
		if !access.Found && access.ExpireAt == nil {
			data.Status = "Не удалось проверить"
		}
		// Запрос устройств может завершиться ошибкой после получения срока.
		data.Devices = "Не удалось проверить"
	}
	return data
}

func (b *Bot) subscriptionWidgetSnapshot(ctx context.Context, tgID int64) (subscriptionWidgetData, error) {
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		return subscriptionWidgetData{}, err
	}
	access, accessErr := b.accessFor(ctx, subject)
	return widgetDataForAccess(access, accessErr), nil
}

func (b *Bot) handleStatusCommand(ctx *telegohandler.Context, message telego.Message) error {
	if message.From != nil {
		b.showScreen(ctx, telego.ChatID{ID: message.Chat.ID}, 0, "subscription", message.From.ID)
	}
	return nil
}

func (b *Bot) showSubscriptionWidget(ctx context.Context, chatID telego.ChatID, tgID int64) {
	if tgID <= 0 || chatID.ID != tgID {
		b.send(ctx, chatID, "Живая карточка доступна в личном чате с ботом: /status", nil)
		return
	}
	b.mu.Lock()
	unsupported := b.widgetsUnsupported
	b.mu.Unlock()
	if !unsupported {
		snapshotCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		data, err := b.subscriptionWidgetSnapshot(snapshotCtx, tgID)
		cancel()
		if err == nil {
			err = b.sendSubscriptionWidgetCard(ctx, tgID, data)
		}
		if err == nil {
			return
		}
		if isWidgetUnsupported(err) {
			b.mu.Lock()
			b.widgetsUnsupported = true
			b.mu.Unlock()
		}
		slog.Warn("виджет подписки недоступен, показываю обычный экран", "err", err)
	}
	b.showScreen(ctx, chatID, 0, "subscription", tgID)
}

func (b *Bot) sendSubscriptionWidgetCard(ctx context.Context, tgID int64, data subscriptionWidgetData) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if len(payload) > 1024 {
		return errors.New("состояние виджета превышает 1024 байта")
	}
	_, err = b.api.SendWidget(ctx, &telego.SendWidgetParams{
		ChatID: telego.ChatID{ID: tgID}, HTML: subscriptionWidgetHTML, ChatData: string(payload), AllowQueries: true,
		Caption:     "Живая карточка подписки. Обычный экран доступен по кнопке ниже.",
		ReplyMarkup: keyboard([]telego.InlineKeyboardButton{button("Моя подписка", "nav:subscription"), button("Продлить", "nav:buy")}),
	})
	return err
}

func isWidgetUnsupported(err error) bool {
	var apiErr *telegoapi.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	text := strings.ToLower(apiErr.Description)
	return strings.Contains(text, "method not found") || strings.Contains(text, "unknown method") ||
		strings.Contains(text, "unsupported method") || strings.Contains(text, "not implemented") ||
		(apiErr.ErrorCode == 404 && strings.Contains(text, "not found"))
}

func privateSubscriptionWidgetQuery(query telego.WidgetQuery) bool {
	_, dataQuery := query.Type.(*telego.WidgetQueryTypeData)
	return dataQuery && query.Data == subscriptionWidgetRefresh && query.Chat != nil && query.Chat.Type == "private" &&
		query.From.ID > 0 && query.Chat.ID == query.From.ID && query.MessageID > 0 && query.InlineMessageID == "" && query.WidgetID == ""
}

func (b *Bot) handleWidgetQuery(ctx *telegohandler.Context, query telego.WidgetQuery) error {
	if strings.HasPrefix(query.Data, "ui:") {
		return b.answerCardQuery(ctx, query)
	}
	return b.answerSubscriptionWidget(ctx, query)
}

func (b *Bot) answerSubscriptionWidget(ctx context.Context, query telego.WidgetQuery) error {
	answerCtx, cancelAnswer := context.WithTimeout(ctx, 12*time.Second)
	defer cancelAnswer()
	answer := func(data string) error {
		return b.api.AnswerWidgetQuery(answerCtx, &telego.AnswerWidgetQueryParams{WidgetQueryID: query.ID, Data: data})
	}
	if !privateSubscriptionWidgetQuery(query) {
		return answer(`{"error":"Откройте свою карточку в личном чате: /status"}`)
	}
	now := time.Now()
	b.mu.Lock()
	if b.widgetRefresh == nil {
		b.widgetRefresh = map[int64]time.Time{}
	}
	last := b.widgetRefresh[query.From.ID]
	allowed := now.Sub(last) >= 5*time.Second
	if allowed {
		b.widgetRefresh[query.From.ID] = now
	}
	if len(b.widgetRefresh) > 1024 {
		for id, at := range b.widgetRefresh {
			if now.Sub(at) > time.Minute {
				delete(b.widgetRefresh, id)
			}
		}
	}
	b.mu.Unlock()
	if !allowed {
		return answer(`{"error":"Обновлять можно раз в 5 секунд"}`)
	}
	snapshotCtx, cancel := context.WithTimeout(answerCtx, 8*time.Second)
	data, err := b.subscriptionWidgetSnapshot(snapshotCtx, query.From.ID)
	cancel()
	if err != nil {
		return answer(`{"error":"Не удалось обновить. Попробуйте позже."}`)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// Сначала отвечаем в 15-секундный срок. После этого сохраняем новое
	// chat_data, чтобы карточка осталась актуальной при следующем открытии.
	if err := answer(string(payload)); err != nil {
		return err
	}
	editCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return b.api.EditMessageWidgetData(editCtx, &telego.EditMessageWidgetDataParams{
		ChatID: telego.ChatID{ID: query.From.ID}, MessageID: query.MessageID, ChatData: string(payload),
	})
}
