package bot

import (
	"strings"

	"github.com/mymmrac/telego"

	"github.com/toonbr1me/vivid-go-bot/internal/bot/texts"
)

// menuConditions — что сейчас включено в сервисе. От этого зависит, какие
// кнопки вообще имеют смысл.
type menuConditions struct {
	Payments bool
	Trial    bool
	Devices  bool
}

// menuState — состояние пользователя для меню.
func menuState(status string) string {
	switch status {
	case "ACTIVE":
		return texts.MenuStateActive
	case "NONE", "INACTIVE", "EXPIRED":
		return texts.MenuStateInactive
	default:
		return texts.MenuStateUnknown
	}
}

// buildMenu собирает главное меню по раскладке из настроек.
//
// Раскладка описывает, какие кнопки и в каком порядке показывать в
// зависимости от состояния подписки. Кнопка с условием (платежи включены,
// есть пробный период) исчезает сама, когда условие не выполнено.
func (b *Bot) buildMenu(status string, conditions menuConditions) *telego.InlineKeyboardMarkup {
	state := menuState(status)
	rows := b.texts.Menu.Layout[state]
	if len(rows) == 0 {
		rows = b.texts.Menu.Layout[texts.MenuStateUnknown]
	}
	if len(rows) == 0 {
		rows = texts.DefaultMenuLayout()[texts.MenuStateUnknown]
	}

	var out [][]telego.InlineKeyboardButton
	for _, row := range rows {
		buttons := b.menuRow(row, state, conditions)
		if len(buttons) == 0 {
			continue
		}
		out = append(out, buttons...)
	}
	return keyboard(out...)
}

// menuRow — кнопки одного ряда.
func (b *Bot) menuRow(row texts.MenuRow, state string, conditions menuConditions) [][]telego.InlineKeyboardButton {
	visible := make([]telego.InlineKeyboardButton, 0, len(row.Buttons))
	for _, id := range row.Buttons {
		button, ok := b.menuButton(id, state, conditions)
		if !ok {
			continue
		}
		switch style := b.texts.Menu.Buttons[id].Style; style {
		case "primary", "success", "danger":
			button.Style = style
		}
		visible = append(visible, button)
	}
	if len(visible) == 0 {
		return nil
	}
	if row.Pack <= 1 {
		return [][]telego.InlineKeyboardButton{visible}
	}
	var packed [][]telego.InlineKeyboardButton
	for i := 0; i < len(visible); i += row.Pack {
		end := i + row.Pack
		if end > len(visible) {
			end = len(visible)
		}
		packed = append(packed, visible[i:end])
	}
	return packed
}

// menuButton — кнопка по идентификатору: подпись, действие и условия.
func (b *Bot) menuButton(id, state string, conditions menuConditions) (telego.InlineKeyboardButton, bool) {
	definition, ok := b.texts.Menu.Buttons[id]
	if !ok {
		return telego.InlineKeyboardButton{}, false
	}
	if len(definition.When) > 0 && !containsString(definition.When, state) {
		return telego.InlineKeyboardButton{}, false
	}
	for _, requirement := range definition.Require {
		switch requirement {
		case texts.MenuRequirePayments:
			if !conditions.Payments {
				return telego.InlineKeyboardButton{}, false
			}
		case texts.MenuRequireTrial:
			if !conditions.Trial {
				return telego.InlineKeyboardButton{}, false
			}
		case texts.MenuRequireDevices:
			if !conditions.Devices {
				return telego.InlineKeyboardButton{}, false
			}
		}
	}
	label := definition.Label
	if label == "" {
		label = b.builtinMenuLabel(id)
	}
	switch {
	case definition.URL != "":
		return urlButton(label, definition.URL), true
	case definition.WebApp != "":
		return telego.InlineKeyboardButton{Text: label, WebApp: &telego.WebAppInfo{URL: definition.WebApp}}, true
	case definition.Action != "":
		return button(label, definition.Action), true
	default:
		return telego.InlineKeyboardButton{}, false
	}
}

// builtinMenuLabel — подпись кнопки, когда в настройках её не задали.
func (b *Bot) builtinMenuLabel(id string) string {
	switch id {
	case "subscription":
		return b.texts.Buttons.Subscription
	case "renew":
		return b.texts.Buttons.Renew
	case "devices":
		return b.texts.Buttons.Devices
	case "trial":
		return b.texts.Buttons.Trial
	case "buy":
		return b.texts.Buttons.Buy
	case "promo":
		return b.texts.Buttons.Promo
	case "connect":
		return b.texts.Buttons.Connect
	case "profile":
		return b.texts.Buttons.Profile
	case "support":
		return b.texts.Buttons.Support
	case "referral":
		return b.texts.Buttons.Referral
	case "more":
		return b.texts.Buttons.More
	default:
		return id
	}
}

// containsString — есть ли значение в списке.
func containsString(items []string, value string) bool {
	for _, item := range items {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
