package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/botui/legacy"
)

// The ordinary interface is a native rich document: a heading, facts table,
// short guidance and grouped Telegram buttons. It never embeds a widget.
func (b *Bot) standardRichScreen(ctx context.Context, id int64, name string) (string, *telego.InlineKeyboardMarkup, bool) {
	switch name {
	case "main":
		return b.richMain(ctx, id)
	case "subscription":
		return b.richSubscription(ctx, id)
	case "profile":
		return b.standardRichProfile(ctx, id)
	case "history":
		return b.standardRichHistory(ctx, id)
	case "settings":
		settings, err := b.db.NotificationSettingsFor(ctx, id)
		if err != nil {
			return "", nil, false
		}
		label := func(on bool) string {
			if on {
				return "Включены"
			}
			return "Выключены"
		}
		html := "<h3>Уведомления</h3><p>Выберите, о чём напоминать.</p><table compact>" + richFact("Об окончании подписки", label(settings.Expiry))
		if b.activityNotificationsEnabled() {
			html += richFact("Об оплате и подключении", label(settings.Activity))
		}
		html += "</table><footer>Изменения сохраняются сразу.</footer>"
		return html, quietRichKeyboard(b.settingsKeyboard(settings)), true
	}
	if name == "buy" {
		if !b.pay.Registry().Enabled() {
			return "", nil, false
		}
		return b.richBuy(ctx, id)
	}
	if name == "devices" {
		html, kb := b.standardRichDevices(ctx, id, 0)
		return html, kb, true
	}
	state, ok := b.interfaceScreen(ctx, id, name)
	if !ok {
		switch name {
		case "promo", "referral", "info", "help":
			text, kb := b.navigationScreen(ctx, id, name)
			return screenRichHTML(text), quietRichKeyboard(kb), true
		default:
			return "", nil, false
		}
	}

	html, kb := richCardDocument(state)
	return html, kb, true
}

func quietRichKeyboard(kb *telego.InlineKeyboardMarkup) *telego.InlineKeyboardMarkup {
	if kb == nil {
		return nil
	}
	rows := make([][]telego.InlineKeyboardButton, len(kb.InlineKeyboard))
	for i, row := range kb.InlineKeyboard {
		rows[i] = append([]telego.InlineKeyboardButton{}, row...)
		for j := range rows[i] {
			rows[i][j].Text = cardLabel(rows[i][j].Text)
		}
	}
	return keyboard(rows...)
}

func (b *Bot) standardRichProfile(ctx context.Context, id int64) (string, *telego.InlineKeyboardMarkup, bool) {
	state, ok := b.profileCard(ctx, id)
	if !ok {
		return "", nil, false
	}
	rendered, kb := richCardDocument(state)
	return rendered, kb, true
}

func (b *Bot) standardRichHistory(ctx context.Context, id int64) (string, *telego.InlineKeyboardMarkup, bool) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return "", nil, false
	}
	orders, err := b.pay.OrderHistory(ctx, subject, 10)
	if err != nil {
		return "", nil, false
	}
	html := "<h3>Покупки</h3>"
	rows := [][]telego.InlineKeyboardButton{}
	if len(orders) == 0 {
		html += "<p>Здесь появятся ваши покупки и их статусы.</p>"
	} else {
		html += "<table compact><tr><th>Покупка</th><th>Статус</th><th align=\"right\">Сумма</th></tr>"
		for _, order := range orders {
			title := orderKindLabel(order.Kind) + " · " + order.CreatedAt.In(b.timezone()).Format("02.01.06")
			html += "<tr><td>" + escapeHTML(title) + "</td><td>" + escapeHTML(order.Progress(time.Now()).Label) + "</td><td align=\"right\">" + escapeHTML(priceText(order.Amount, order.Currency)) + "</td></tr>"
			{
				rows = append(rows, []telego.InlineKeyboardButton{button("Проверить "+shortID(order.OrderID), "check:"+order.OrderID)})
			}
		}
		html += "</table><footer>Последние 10 заказов</footer>"
	}
	rows = append(rows, []telego.InlineKeyboardButton{button("Аккаунт", "nav:profile"), button("Главная", "nav:main")})
	return html, keyboard(rows...), true
}

func (b *Bot) standardRichDevices(ctx context.Context, id int64, page int) (string, *telego.InlineKeyboardMarkup) {
	state := b.deviceCard(ctx, id)
	if page < 0 || page >= len(state.Pages) {
		page = 0
	}
	state.Current = page
	return richCardDocument(state)
}

func richFact(label, value string) string {
	return `<tr><td>` + escapeHTML(label) + `</td><td align="right"><b>` + escapeHTML(value) + `</b></td></tr>`
}

func richCardDocument(state cardState) (string, *telego.InlineKeyboardMarkup) {
	p := state.Pages[state.Current]
	facts := []legacy.Stat{}
	for _, stat := range p.Stats {
		facts = append(facts, legacy.Stat{Label: stat.Label, Value: stat.Value, Warn: stat.Warn})
	}
	var out strings.Builder
	out.WriteString(legacy.Document(legacy.Page{Title: p.Title, Status: p.Status, Kicker: p.Kicker, Value: p.Value, Note: p.Note, Body: p.Body, Updated: p.Updated, Step: p.Step, Stats: facts}))
	_, kb := nativeCardScreen(state)
	if p.Title == "Устройства" && len(state.Pages) > 1 {
		rows := kb.InlineKeyboard
		// Keep paging beside its counter, above the navigation back home.
		footer := rows[len(rows)-1]
		rows = rows[:len(rows)-1]
		pager := []telego.InlineKeyboardButton{}
		if state.Current > 0 {
			pager = append(pager, button("← Назад", fmt.Sprintf("devices:page:%d", state.Current-1)))
		}
		if state.Current+1 < len(state.Pages) {
			pager = append(pager, button("Далее →", fmt.Sprintf("devices:page:%d", state.Current+1)))
		}
		out.WriteString(fmt.Sprintf("<footer>Страница %d из %d</footer>", state.Current+1, len(state.Pages)))
		rows = append(rows, pager, footer)
		kb = keyboard(rows...)
	}
	return out.String(), kb
}
