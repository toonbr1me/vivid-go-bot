package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
)

func (b *Bot) interfaceScreen(ctx context.Context, id int64, name string) (cardState, bool) {
	switch name {
	case "diagnostics":
		return b.diagnosticCard(ctx, id)
	case "main", "subscription":
		return b.subscriptionCard(ctx, id)
	case "devices":
		state := b.deviceCard(ctx, id)
		return state, true
	case "connect":
		return b.connectHubCard(ctx, id), true
	case "profile":
		return b.profileCard(ctx, id)
	case "buy":
		return b.buyCard(ctx, id)
	case "support":
		_, kb := b.supportScreen(ctx, id)
		actions := []telego.InlineKeyboardButton{button("Проверить подписку и устройства", "nav:diagnostics"), button("Инструкция подключения", "nav:connect")}
		for _, row := range kb.InlineKeyboard {
			for _, a := range row {
				if a.CallbackData != "nav:main" && a.CallbackData != "nav:profile" {
					actions = append(actions, a)
				}
			}
		}
		return cardWithButtons(cardPage{Title: "Чем помочь?", Tab: "support", Note: "Выберите тему или напишите нам", Pages: 1}, actions), true
	case "more":
		return cardWithButtons(cardPage{Title: "Аккаунт", Tab: "more", Layout: "menu", Note: "Баланс, приглашения и настройки"}, []telego.InlineKeyboardButton{button("Профиль и баланс", "nav:profile"), button("Пригласить друга", "nav:referral"), button("Настройки", "nav:settings"), button("О сервисе", "nav:info")}), true
	}
	return cardState{}, false
}

func cardWithButtons(page cardPage, actions []telego.InlineKeyboardButton) cardState {
	state := cardState{Actions: actions}
	for p := 0; p < max(1, (len(actions)+3)/4); p++ {
		current := page
		current.Page = p
		current.Pages = max(1, (len(actions)+3)/4)
		current.Buttons = nil
		for i := p * 4; i < min(len(actions), (p+1)*4); i++ {
			style := actions[i].Style
			if style == "" {
				style = actionStyle(actions[i].CallbackData)
			}
			if p == 0 && i == 0 && style != "danger" && page.Layout != "menu" {
				style = "primary"
			}
			current.Buttons = append(current.Buttons, cardButton{Text: cardLabel(actions[i].Text), Index: i, Style: style})
		}
		state.Pages = append(state.Pages, current)
	}
	return state
}

func (b *Bot) deviceCard(ctx context.Context, id int64) cardState {
	if !b.cfg.DeviceLimitEnabled {
		return textCard("Устройства\nУправление устройствами отключено.", keyboard(b.backRow()))
	}
	subject, err := b.subject(ctx, id)
	if err != nil {
		return textCard("Устройства\nПрофиль временно недоступен.", keyboard(b.backRow()))
	}
	access, err := b.accessFor(ctx, subject)
	if err != nil {
		return textCard("Устройства\nНе удалось получить актуальный список. Попробуйте позже.", keyboard(b.backRow()))
	}
	state := cardState{}
	count := max(1, (len(access.Devices)+2)/3)
	for p := 0; p < count; p++ {
		page := cardPage{Title: "Устройства", Tab: "devices", Page: p, Pages: count, Note: fmt.Sprintf("Подключено: %d", len(access.Devices)), Updated: b.cardUpdated()}
		if access.DeviceLimit > 0 {
			page.Note += fmt.Sprintf(" · лимит %d", access.DeviceLimit)
		}
		if access.DeviceLimit > 0 && len(access.Devices) > access.DeviceLimit {
			page.Body = "Устройств больше лимита. Выберите лишнее для отвязки."
		}
		if len(access.Devices) == 0 {
			page.Body = "Подключённых устройств пока нет."
		}
		for i := p * 3; i < min(len(access.Devices), (p+1)*3); i++ {
			d := access.Devices[i]
			label := d.Model
			if label == "" {
				label = "Устройство"
			}
			if d.Platform != "" {
				label += " · " + d.Platform
			}
			index := len(state.Actions)
			state.Actions = append(state.Actions, button(label, "card:device:"+d.HWID))
			tail := d.HWID
			if len(tail) > 4 {
				tail = tail[len(tail)-4:]
			}
			model := d.Model
			if model == "" {
				model = "Устройство"
			}
			page.Buttons = append(page.Buttons, cardButton{Text: cardLabel(model), Index: index, Style: "device", Detail: cardLabel(d.Platform + " · " + tail)})
		}
		index := len(state.Actions)
		state.Actions = append(state.Actions, button("Дополнительные места", "card:slots"))
		page.Buttons = append(page.Buttons, cardButton{Text: "Дополнительные места", Index: index})
		state.Pages = append(state.Pages, page)
	}
	return state
}

func (b *Bot) cardAction(ctx *telegohandler.Context, q telego.WidgetQuery, action string) error {
	if action == "card:setup" {
		return b.deliverJourney(ctx, q.From.ID, q.MessageID, b.connectPickerCard())
	}
	id := q.From.ID
	chat := telego.ChatID{ID: id}
	if handled, err := b.journeyAction(ctx, q, action); handled {
		return err
	}
	if action == "card:slots" {
		subject, err := b.subject(ctx, id)
		if err != nil {
			return err
		}
		access, err := b.accessFor(ctx, subject)
		if err != nil {
			return err
		}
		quotes, err := b.pay.DeviceQuotes(ctx, access.ExpireAt, time.Now())
		if err != nil {
			return err
		}
		text := "Дополнительные места\nСтоимость рассчитана до конца текущей подписки."
		if access.ExpireAt != nil {
			text += "\nПериод: до " + access.ExpireAt.In(b.timezone()).Format("02.01.2006")
		}
		rows := [][]telego.InlineKeyboardButton{}
		for _, quote := range quotes {
			rows = append(rows, []telego.InlineKeyboardButton{button(fmt.Sprintf("+%d · %s", quote.Pack.Devices, priceText(quote.Amount, b.cfg.Currency)), "dev:buy:"+quote.Pack.ID)})
		}
		slots, _ := b.pay.DeviceSlots(ctx, subject.ID, access.ExpireAt)
		for _, slot := range slots {
			if slot.Renewable {
				rows = append(rows, []telego.InlineKeyboardButton{button("Продлить "+slot.Title, "dev:renew:"+slot.OrderID)})
			}
		}
		if len(rows) == 0 {
			text += "\nДоступных пакетов сейчас нет."
		}
		rows = append(rows, []telego.InlineKeyboardButton{button("К устройствам", "nav:devices")})
		return b.deliver(ctx, telego.ChatID{ID: id}, q.MessageID, text, keyboard(rows...))
	}
	parts := strings.SplitN(action, ":", 3)
	if len(parts) != 3 {
		return errors.New("unknown card action")
	}
	subject, err := b.subject(ctx, id)
	if err != nil {
		return err
	}
	access, err := b.accessFor(ctx, subject)
	if err != nil {
		return err
	}
	for _, d := range access.Devices {
		if d.HWID != parts[2] {
			continue
		}
		if parts[1] == "device" || parts[1] == "confirm" {
			name := d.Model
			if name == "" {
				name = "Устройство"
			}
			back := fmt.Sprintf("card:devicespage:%d", b.deviceReturnPage(ctx, id))
			page := cardPage{Title: "Устройство", Tab: "devices", Value: firstRunes(name, 36), Note: d.Platform, Body: "Отвязка удалит запись из списка. При новом подключении устройство может появиться снова."}
			actions := []telego.InlineKeyboardButton{{Text: "Отвязать устройство", CallbackData: "card:confirm:" + d.HWID, Style: "danger"}}
			if parts[1] == "confirm" {
				page.Title = "Подтверждение"
				page.Value = "Отвязать устройство?"
				page.Note = firstRunes(name, 30)
				actions = []telego.InlineKeyboardButton{{Text: "Да, отвязать", CallbackData: "card:detach:" + d.HWID, Style: "danger"}, button("Оставить подключённым", back)}
			}
			return b.deliverJourney(ctx, id, q.MessageID, cardUtility(cardWithButtons(page, actions), back, false))
		}
		if parts[1] == "detach" {
			removed, err := b.pay.RemoveAccessDevice(ctx, access.PanelUserID, d.HWID)
			if err != nil {
				return err
			}
			if !removed {
				return errors.New("device was not removed")
			}
			b.showScreen(ctx, chat, q.MessageID, "devices", id)
			return nil
		}
	}
	b.showScreen(ctx, chat, q.MessageID, "devices", id)
	return nil
}

func (b *Bot) compactClassic(ctx context.Context, id int64, name string) (string, *telego.InlineKeyboardMarkup, bool) {
	if name == "diagnostics" {
		state, ok := b.diagnosticCard(ctx, id)
		if !ok {
			return "", nil, false
		}
		text, kb := nativeCardScreen(state)
		return text, kb, true
	}
	if name == "connect" {
		text, kb := nativeCardScreen(b.connectHubCard(ctx, id))
		return text, kb, true
	}
	if name == "devices" {
		text, kb := b.classicDevices(ctx, id, 0)
		return text, kb, true
	}
	if name != "main" && name != "subscription" {
		return "", nil, false
	}
	subject, err := b.subject(ctx, id)
	if err != nil {
		return "", nil, false
	}
	access, err := b.accessFor(ctx, subject)
	title := "<b>Подписка не активна</b>"
	if access.Active {
		title = "<b>Подписка активна</b>"
	}
	if err != nil && !access.Found {
		title = "<b>Не удалось проверить подписку</b>"
	}
	if access.ExpireAt != nil {
		title += "\nДо " + access.ExpireAt.In(b.timezone()).Format("02.01.2006")
	}
	if access.Found {
		title += fmt.Sprintf("\nУстройства: %d", len(access.Devices))
		if access.DeviceLimit > 0 {
			title += fmt.Sprintf(" из %d", access.DeviceLimit)
		}
	}
	if name == "subscription" {
		if access.TrafficLimit != nil && *access.TrafficLimit > 0 {
			title += "\nТрафик: " + bytesText(access.TrafficUsedBytes) + " из " + bytesText(*access.TrafficLimit)
		}
		return title, b.subscriptionKeyboard(access), true
	}
	rows := [][]telego.InlineKeyboardButton{}
	if access.Active {
		rows = append(rows, []telego.InlineKeyboardButton{{Text: "Подключить устройство", CallbackData: "nav:connect", Style: "primary"}})
	}
	if b.pay.Registry().Enabled() {
		label := "Выбрать подписку"
		if access.Active {
			label = "Продлить подписку"
		}
		rows = append(rows, []telego.InlineKeyboardButton{{Text: label, CallbackData: "nav:buy", Style: "success"}})
	}
	if !access.Active && b.cfg.TrialBotDays > 0 && b.trialAvailable(ctx, subject) {
		rows = append(rows, []telego.InlineKeyboardButton{button("Попробовать бесплатно", "nav:trial")})
	}
	rows = append(rows, []telego.InlineKeyboardButton{button("Моя подписка", "nav:subscription"), button("Устройства", "nav:devices")}, []telego.InlineKeyboardButton{button("Помощь", "nav:support"), button("Ещё", "nav:more")})
	return title, keyboard(rows...), true
}

func (b *Bot) classicDevices(ctx context.Context, id int64, page int) (string, *telego.InlineKeyboardMarkup) {
	state := b.deviceCard(ctx, id)
	if page < 0 || page >= len(state.Pages) {
		page = 0
	}
	p := state.Pages[page]
	text := "<b>" + escapeHTML(p.Title) + "</b>\n" + escapeHTML(p.Note)
	if p.Body != "" {
		text += "\n\n" + escapeHTML(p.Body)
	}
	rows := [][]telego.InlineKeyboardButton{}
	for _, item := range p.Buttons {
		a := state.Actions[item.Index]
		if strings.HasPrefix(a.CallbackData, "card:device:") {
			hwid := strings.TrimPrefix(a.CallbackData, "card:device:")
			tail := hwid
			if len(tail) > 8 {
				tail = tail[len(tail)-8:]
			}
			a.CallbackData = "dev:rm:" + tail
		} else if a.CallbackData == "card:slots" {
			a.CallbackData = "devices:slots"
		}
		rows = append(rows, []telego.InlineKeyboardButton{a})
	}
	var pager []telego.InlineKeyboardButton
	if page > 0 {
		pager = append(pager, button("← Назад", fmt.Sprintf("devices:page:%d", page-1)))
	}
	if page+1 < len(state.Pages) {
		pager = append(pager, button("Далее →", fmt.Sprintf("devices:page:%d", page+1)))
	}
	if len(pager) > 0 {
		rows = append(rows, pager)
		text += fmt.Sprintf("\n\nСтраница %d из %d", page+1, len(state.Pages))
	}
	rows = append(rows, b.backRow())
	return text, keyboard(rows...)
}
