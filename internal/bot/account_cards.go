package bot

import (
	"context"
	"fmt"
	"github.com/mymmrac/telego"
)

func (b *Bot) profileCard(ctx context.Context, id int64) (cardState, bool) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return cardState{}, false
	}
	balance, err := b.db.Balance(ctx, subject.ID)
	if err != nil {
		return cardState{}, false
	}
	p := cardPage{Title: "Аккаунт", Tab: "more", Kicker: "Баланс", Value: priceText(balance, b.cfg.Current().Currency), Note: "Покупки, обращения и настройки"}
	actions := []telego.InlineKeyboardButton{button("История покупок", "nav:history"), button("История обращений", "support:list"), button("Промокод", "nav:promo"), button("Уведомления", "nav:settings")}
	if a, err := b.db.LatestRecurringForSubject(ctx, subject.ID); err == nil {
		p.Body = fmt.Sprintf("Автопродление · %s\n%s каждые %d %s\n%s", a.ProviderName(), priceText(a.Amount, a.Currency), a.Days, daysWord(a.Days), a.StatusText())
		if a.Enabled && a.NextChargeAt != nil {
			p.Body += "\nСледующее списание: " + dateTimeText(*a.NextChargeAt)
		}
		if a.StoppedAt != nil {
			p.Body += "\nОстановлено: " + dateTimeText(*a.StoppedAt)
		}
		if a.Enabled {
			actions = append([]telego.InlineKeyboardButton{button("Отключить автопродление", "recurring:cancel")}, actions...)
		}
		if a.PendingOrderID != nil {
			actions = append(actions, button("Заказ автопродления", "check:"+*a.PendingOrderID))
		}
	}
	state := cardState{Pages: []cardPage{p}, Actions: actions}
	for i, a := range actions {
		state.Pages[0].Buttons = append(state.Pages[0].Buttons, cardButton{Text: cardLabel(a.Text), Index: i, Style: actionStyle(a.CallbackData)})
	}
	return state, true
}

func (b *Bot) buyCard(ctx context.Context, id int64) (cardState, bool) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return cardState{}, false
	}
	quotes, err := b.pay.QuotePlans(ctx, subject.ID, b.currentPromo(id))
	if err != nil {
		return cardState{}, false
	}
	actions := []telego.InlineKeyboardButton{}
	for _, q := range sellableQuotes(quotes) {
		if q.Rejection != "" {
			continue
		}
		actions = append(actions, button(fmt.Sprintf("%s · %s · %d устройств", q.Plan.Title, priceText(q.Amount, b.cfg.Current().Currency), q.Plan.Devices), "plan:"+q.Plan.ID))
	}
	actions = append(actions, button("Есть промокод", "nav:promo"))
	return cardUtility(cardWithButtons(cardPage{Title: "Подписка", Tab: "subscription", Value: "Выберите срок", Note: "Оплаченный срок сохранится. Итог и способы оплаты — на следующем шаге.", Layout: "plans"}, actions), "nav:subscription", false), true
}
