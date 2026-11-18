package bot

import (
	"context"
	"github.com/mymmrac/telego"
	"net/url"
	"strings"
)

func (b *Bot) connectHubCard(ctx context.Context, id int64) cardState {
	state := b.connectAppsCard(ctx, id)
	if len(state.Pages) == 0 || state.Pages[0].Layout != "apps" {
		return state
	}
	picker := b.connectPickerCard()
	if state.Pages[0].Tone == "warn" {
		picker.Pages[0].Note = state.Pages[0].Note
	}
	return picker
}

func (b *Bot) connectAppsCard(ctx context.Context, id int64) cardState {
	if b.pay == nil {
		return b.connectPickerCard()
	}
	subject, err := b.subject(ctx, id)
	if err != nil {
		return b.connectPickerCard()
	}
	access, err := b.accessFor(ctx, subject)
	if !access.Found || !access.Active || access.URL == "" {
		return cardWithButtons(cardPage{Title: "Подключение", Tab: "subscription", Value: "Нужен действующий доступ", Note: "Выберите подписку или проверьте её состояние"}, []telego.InlineKeyboardButton{button("Моя подписка", "nav:subscription"), button("Помощь", "nav:support")})
	}
	p := cardPage{Title: "Подключение", Tab: "subscription", Value: "Выберите приложение", Note: "Приложение откроется и предложит добавить подписку. Затем включите подключение.", Used: len(access.Devices), Limit: access.DeviceLimit, Layout: "apps"}
	if err != nil {
		p.Note = "Подписка и ссылка доступны. Список устройств временно не удалось обновить."
		p.Tone = "warn"
	}
	actions := []telego.InlineKeyboardButton{}
	if site := b.siteURL(); strings.HasPrefix(site, "https://") {
		for _, app := range []struct{ key, name string }{{"happ", "Happ"}, {"incy", "Incy"}, {"clashmi", "Clash Mi"}, {"flclashx", "FlClashX"}} {
			actions = append(actions, urlButton(app.name, site+"/app-import/"+app.key+"?u="+url.QueryEscape(access.URL)))
		}
	} else {
		p.Note = "Получите ссылку и добавьте её в установленное приложение. Для кнопок быстрого импорта владелец должен настроить HTTPS-адрес сайта."
	}
	actions = append(actions, copyButton("Получить ссылку", access.URL), button("Как установить приложение", "card:setup"), button("Проверить подписку", "nav:diagnostics"))
	return cardUtility(cardWithButtons(p, actions), "nav:subscription", false)
}
