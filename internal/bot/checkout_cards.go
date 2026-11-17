package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/mymmrac/telego"
)

type promoPlanKey struct{}

func (b *Bot) checkoutRecurring(ctx context.Context, id int64, plan string) bool {
	p, on, err := b.db.CheckoutPreference(ctx, id)
	return err == nil && p == plan && on && b.cfg.Current().RecurringEnabled
}

func (b *Bot) checkoutCard(ctx context.Context, id int64, planID string) (cardState, error) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return cardState{}, err
	}
	plan, err := b.pay.Plan(ctx, planID)
	if err != nil {
		return cardState{}, err
	}
	amount := plan.Price
	bonusDays := 0
	quotes, err := b.pay.QuotePlans(ctx, subject.ID, b.currentPromo(id))
	if err != nil {
		return cardState{}, err
	}
	for _, q := range quotes {
		if q.Plan.ID == planID {
			if q.Rejection != "" {
				return cardState{}, fmt.Errorf("%s", q.Rejection)
			}
			amount = q.Amount
			bonusDays = q.BonusDays
		}
	}
	recurring := b.checkoutRecurring(ctx, id, planID)
	p := cardPage{Title: "Оформление подписки", Tab: "subscription", Kicker: plan.Title, Value: priceText(amount, b.cfg.Current().Currency), Layout: "checkout", Note: fmt.Sprintf("%d %s · %d устройств", plan.Days, daysWord(plan.Days), plan.Devices)}
	if bonusDays > 0 {
		p.Note += fmt.Sprintf(" · +%d бонусных дней к этой покупке", bonusDays)
	}
	if code := b.currentPromo(id); code != "" {
		p.Note += " · промокод " + code
	}
	if access, e := b.accessFor(ctx, subject); e == nil {
		end := time.Now()
		if access.ExpireAt != nil && access.ExpireAt.After(end) {
			end = *access.ExpireAt
		}
		p.Stats = []cardStat{{Label: "Доступ до", Value: calendarDate(end.AddDate(0, 0, plan.Days+bonusDays))}}
	}
	state := cardState{Pages: []cardPage{p}}
	add := func(a telego.InlineKeyboardButton, style, detail string, checked *bool) {
		i := len(state.Actions)
		state.Actions = append(state.Actions, a)
		state.Pages[0].Buttons = append(state.Pages[0].Buttons, cardButton{Text: cardLabel(a.Text), Index: i, Style: style, Detail: detail, Checked: checked})
	}
	if b.cfg.Current().RecurringEnabled {
		detail := "Выключено · разовая оплата"
		if recurring {
			detail = fmt.Sprintf("%s каждые %d дней · отмена в аккаунте", priceText(amount, b.cfg.Current().Currency), plan.Days)
		}
		add(button("Продлевать автоматически?", "recurring:toggle:"+planID), "checkbox", detail, &recurring)
	}
	options := b.pay.PaymentOptions(false)
	payments := 0
	for _, o := range options {
		if recurring && !b.pay.RecurringSupported(o, plan.Days) {
			continue
		}
		prefix := "pay:"
		if recurring {
			prefix = "payr:"
		}
		style := ""
		if payments == 0 {
			style = "primary"
		}
		add(button(o.ProviderLabel+" · "+o.MethodLabel, prefix+planID+":"+o.Token()), style, "", nil)
		payments++
	}
	if !recurring {
		balance, _ := b.db.Balance(ctx, subject.ID)
		if balance > 0 && balance >= amount {
			add(button("Оплатить с баланса", "payb:"+planID), "", "", nil)
		}
	}
	if recurring && payments == 0 {
		state.Pages[0].Body = "Для этого тарифа нет доступных автоплатежей. Снимите галочку для разовой оплаты."
	}
	add(button("Промокод", "promo:checkout:"+planID), "", "", nil)
	return cardUtility(state, "nav:buy", false), nil
}

func (b *Bot) showCheckout(ctx context.Context, id, messageOwner int64, messageID int, plan string) {
	if id != messageOwner {
		return
	}
	if b.viewMode(ctx, id) == "cards" {
		state, err := b.checkoutCard(ctx, id, plan)
		if err == nil {
			if err = b.deliverUI(ctx, id, messageID, state); err == nil {
				return
			}
		}
		b.send(ctx, telego.ChatID{ID: id}, "Не удалось открыть оформление. Повторите выбор тарифа.", keyboard(b.backRow()))
		return
	}
	text, kb := b.planScreen(ctx, id, plan)
	b.upsert(ctx, telego.ChatID{ID: id}, messageID, text, kb)
}
