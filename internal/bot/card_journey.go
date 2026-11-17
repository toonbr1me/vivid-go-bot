package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
)

func calendarDate(t time.Time) string {
	months := []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	return fmt.Sprintf("%d %s %d", t.Day(), months[t.Month()-1], t.Year())
}

func (b *Bot) cardUpdated() string { return time.Now().In(b.timezone()).Format("15:04 MST") }

func (b *Bot) subscriptionCard(ctx context.Context, id int64) (cardState, bool) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return cardState{}, false
	}
	access, accessErr := b.accessFor(ctx, subject)
	page := cardPage{Title: b.cfg.Current().BrandName, Tab: "subscription", Status: "Нет подписки", Value: "Ваш доступ", Note: "Выберите подписку для подключения", Updated: b.cardUpdated(), Hero: true, Used: len(access.Devices), Limit: access.DeviceLimit}
	if page.Title == "" {
		page.Title = "Подписка"
	}
	if access.Active {
		page.Status = "Активна"
		page.Tone = "ok"
		page.Note = ""
	} else if access.ExpireAt != nil && !access.ExpireAt.After(time.Now()) {
		page.Status = "Истекла"
		page.Tone = "warn"
		page.Note = "Продлите подписку, чтобы вернуть доступ"
	} else if access.ExpireAt != nil {
		page.Status = "Требуется проверка"
		page.Tone = "warn"
		page.Note = "Срок ещё действует. Проверьте ограничения или обратитесь в поддержку."
	}
	if access.ExpireAt != nil {
		page.Kicker = "Оплачено до"
		page.Value = calendarDate(access.ExpireAt.In(b.timezone()))
		if days, ok := daysLeft(access.ExpireAt, time.Now()); ok && access.Active && days <= 365 {
			page.Note = fmt.Sprintf("Осталось %d %s", max(0, days), daysWord(max(0, days)))
		}
	}
	if access.Found {
		devices := fmt.Sprintf("%d", len(access.Devices))
		warn := false
		if access.DeviceLimit > 0 {
			devices += fmt.Sprintf(" / %d", access.DeviceLimit)
			warn = len(access.Devices) > access.DeviceLimit
		}
		page.Stats = append(page.Stats, cardStat{Label: "Устройства", Value: devices, Nav: "devices", Warn: warn})
		traffic := bytesText(access.TrafficUsedBytes)
		label := "Использовано"
		if access.TrafficLimit == nil || *access.TrafficLimit == 0 {
			label = "Трафик без лимита"
		}
		page.Stats = append(page.Stats, cardStat{Label: label, Value: traffic})
	}
	var actions []telego.InlineKeyboardButton
	if !access.Active && b.cfg.Current().TrialBotDays > 0 && b.trialAvailable(ctx, subject) && accessErr == nil {
		actions = append(actions, button("Попробовать бесплатно", "nav:trial"))
	}
	if access.Active && access.URL != "" {
		actions = append(actions, button("Подключить устройство", "nav:connect"))
	}
	if b.pay.Registry().Enabled() {
		label := "Выбрать подписку"
		if access.Active {
			label = "Продлить подписку"
		}
		actions = append(actions, button(label, "nav:buy"))
	}
	actions = append(actions, button("История обращений", "support:list"))
	if guidance, err := b.pay.CustomerGuidance(ctx, subject, access, "bot"); err == nil && (accessErr == nil || access.Found || guidance.Action == "order") {
		page.Note = guidance.Detail
		if guidance.Action == "order" {
			page.Status = guidance.Title
			page.Tone = "warn"
			actions = append([]telego.InlineKeyboardButton{button(guidance.Label, "check:"+guidance.OrderID)}, actions...)
		}
	}
	if accessErr != nil {
		page.Status = "Данные неполные"
		page.Tone = "warn"
		page.Note = "Не удалось обновить часть данных"
		if !access.Found {
			page.Value = "Проверим позже"
			page.Kicker = ""
			page.Stats = nil
		}
	}
	return cardUtility(cardWithButtons(page, actions), "nav:subscription", true), true
}

type connectPlatform struct{ Name, Store, URL string }

func connectionPlatform(key string) (connectPlatform, bool) {
	options := map[string]connectPlatform{
		"ios":     {"iPhone / iPad", "App Store", "https://apps.apple.com/app/happ-proxy-utility/id6504287215"},
		"android": {"Android", "Google Play", "https://play.google.com/store/apps/details?id=com.happproxy"},
		"windows": {"Windows", "GitHub", "https://github.com/Happ-proxy/happ-desktop/releases/latest/download/setup-Happ.x64.exe"},
		"macos":   {"macOS", "App Store", "https://apps.apple.com/app/happ-proxy-utility/id6504287215"},
	}
	platform, ok := options[key]
	return platform, ok
}

func (b *Bot) connectPickerCard() cardState {
	page := cardPage{Title: "Подключение", Tab: "subscription", Value: "Какое устройство?", Note: "Покажем подходящее приложение и следующие шаги", Layout: "platform"}
	actions := []telego.InlineKeyboardButton{}
	for _, key := range []string{"ios", "android", "windows", "macos"} {
		platform, _ := connectionPlatform(key)
		actions = append(actions, button(platform.Name, "card:platform:"+key))
	}
	state := cardWithButtons(page, actions)
	for p := range state.Pages {
		for i := range state.Pages[p].Buttons {
			state.Pages[p].Buttons[i].Style = ""
		}
	}
	return cardUtility(state, "nav:subscription", false)
}

func (b *Bot) platformCard(key string) cardState {
	platform, _ := connectionPlatform(key)
	page := cardPage{Title: platform.Name, Tab: "subscription", Value: "Установите Happ", Note: "Из " + platform.Store + " — затем вернитесь в бот", Step: 1}
	actions := []telego.InlineKeyboardButton{urlButton("Установить Happ", platform.URL), button("Приложение уже установлено", "card:import:"+key)}
	if key == "ios" || key == "android" {
		actions = append(actions, button("Другие приложения", "card:clients:"+key))
	}
	return cardUtility(cardWithButtons(page, actions), "nav:connect", false)
}

func (b *Bot) importCard(ctx context.Context, id int64, key string) (cardState, error) {
	platform, _ := connectionPlatform(key)
	subject, err := b.subject(ctx, id)
	if err != nil {
		return cardState{}, err
	}
	access, err := b.accessFor(ctx, subject)
	if err != nil && !access.Found {
		return cardState{}, err
	}
	page := cardPage{Title: platform.Name, Tab: "subscription", Value: "Добавьте подписку", Step: 2, Note: "Получите ссылку и импортируйте её в Happ"}
	actions := []telego.InlineKeyboardButton{}
	if access.URL != "" {
		actions = append(actions, copyButton("Получить ссылку", access.URL))
		if link := b.happURL(access.URL); telegramURL(link) {
			actions = append(actions, urlButton("Добавить в Happ", link))
		}
		if telegramURL(access.URL) {
			actions = append(actions, urlButton("Открыть страницу подписки", access.URL))
		}
		if telegramURL(b.cfg.Current().ConnectGuideURL) {
			actions = append(actions, urlButton("Подробная инструкция", b.cfg.Current().ConnectGuideURL))
		}
		actions = append(actions, button("Проверить готовность", "nav:diagnostics"))
	} else {
		page.Value = "Ссылка пока недоступна"
		page.Note = "Нужна действующая подписка"
		if b.pay.Registry().Enabled() {
			actions = append(actions, button("Выбрать подписку", "nav:buy"))
		} else {
			actions = append(actions, button("Связаться с поддержкой", "nav:support"))
		}
	}
	return cardUtility(cardWithButtons(page, actions), "card:platform:"+key, false), nil
}

func (b *Bot) journeyAction(ctx *telegohandler.Context, q telego.WidgetQuery, action string) (bool, error) {
	parts := strings.SplitN(action, ":", 3)
	if len(parts) != 3 {
		return false, nil
	}
	id := q.From.ID
	switch parts[1] {
	case "platform", "import", "clients":
		platform, ok := connectionPlatform(parts[2])
		if !ok {
			return true, fmt.Errorf("unknown platform")
		}
		state := b.platformCard(parts[2])
		if parts[1] == "import" {
			var err error
			state, err = b.importCard(ctx, id, parts[2])
			if err != nil {
				return true, err
			}
		}
		if parts[1] == "clients" {
			actions := []telego.InlineKeyboardButton{}
			for _, row := range b.connectKeyboard().InlineKeyboard {
				for _, a := range row {
					if a.URL == "" || strings.Contains(a.Text, "Happ") {
						continue
					}
					label := strings.ToLower(a.Text)
					if (parts[2] == "ios" && strings.Contains(label, "ios")) || (parts[2] == "android" && strings.Contains(label, "android")) {
						actions = append(actions, a)
					}
				}
			}
			actions = append(actions, button("Перейти к добавлению подписки", "card:import:"+parts[2]))
			state = cardUtility(cardWithButtons(cardPage{Title: platform.Name, Tab: "subscription", Value: "Другие приложения", Note: "Выберите клиент, затем добавьте ссылку подписки", Step: 1}, actions), "card:platform:"+parts[2], false)
		}
		return true, b.deliverJourney(ctx, id, q.MessageID, state)
	case "devicespage":
		page, err := strconv.Atoi(parts[2])
		if err != nil {
			return true, err
		}
		state := b.deviceCard(ctx, id)
		if page < 0 || page >= len(state.Pages) {
			page = 0
		}
		state.Current = page
		return true, b.deliverJourney(ctx, id, q.MessageID, state)
	}
	return false, nil
}

func (b *Bot) deviceReturnPage(ctx context.Context, id int64) int {
	_, raw, err := b.db.BotCard(ctx, id)
	if err != nil {
		return 0
	}
	var state cardState
	if json.Unmarshal(raw, &state) != nil {
		return 0
	}
	for _, a := range state.Actions {
		if strings.HasPrefix(a.CallbackData, "card:devicespage:") {
			p, _ := strconv.Atoi(strings.TrimPrefix(a.CallbackData, "card:devicespage:"))
			return max(0, p)
		}
	}
	if state.Current >= 0 && state.Current < len(state.Pages) && state.Pages[state.Current].Tab == "devices" {
		return state.Current
	}
	return 0
}
