package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
)

func (b *Bot) diagnosticCard(ctx context.Context, id int64) (cardState, bool) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return cardState{}, false
	}
	access, accessErr := b.accessFor(ctx, subject)
	page := cardPage{Title: "Проверка подключения", Tab: "support", Value: "Проверим настройки", Note: "Это проверка подписки и мест, а не сети вашего устройства", Updated: b.cardUpdated()}
	lines := []string{}
	actions := []telego.InlineKeyboardButton{}
	if access.Active {
		lines = append(lines, "Подписка · активна")
	} else {
		lines = append(lines, "Подписка · не активна")
	}
	if access.URL != "" {
		lines = append(lines, "Ссылка · доступна")
	} else {
		lines = append(lines, "Ссылка · пока недоступна")
	}
	if access.DeviceLimit > 0 {
		free := access.DeviceLimit - len(access.Devices)
		if free <= 0 {
			lines = append(lines, fmt.Sprintf("Устройства · занято %d из %d", len(access.Devices), access.DeviceLimit))
			page.Value = "Проверьте свободные места"
			actions = append(actions, button("Управлять устройствами", "nav:devices"))
		} else {
			lines = append(lines, fmt.Sprintf("Устройства · свободно %d", free))
		}
	} else {
		lines = append(lines, fmt.Sprintf("Подключённых устройств · %d", len(access.Devices)))
	}
	if access.Active && access.URL != "" {
		if len(actions) == 0 {
			page.Value = "Готово к настройке"
			page.Status = "Данные проверены"
			page.Tone = "ok"
		}
		actions = append(actions, button("Открыть подключение", "nav:connect"))
	} else {
		page.Value = "Нужна действующая подписка"
		if b.pay.Registry().Enabled() {
			actions = append(actions, button("Выбрать подписку", "nav:buy"))
		}
	}
	if accessErr != nil {
		page.Value = "Проверка пока неполная"
		page.Status = "Данные недоступны"
		page.Tone = "warn"
		lines = []string{"Не удалось получить все сведения из панели", "Оплаченный срок не меняется", "Повторите проверку позже"}
		actions = nil
	}
	actions = append(actions, button("Написать в поддержку", "support:new"))
	page.Body = strings.Join(lines, "\n")
	return cardUtility(cardWithButtons(page, actions), "nav:diagnostics", true), true
}

func (b *Bot) handleDiagnosticsCommand(ctx *telegohandler.Context, m telego.Message) error {
	if m.From != nil {
		b.showCommandScreen(ctx, telego.ChatID{ID: m.Chat.ID}, "diagnostics", m.From.ID)
	}
	return nil
}
