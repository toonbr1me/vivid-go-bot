package bot

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"strconv"
	"strings"
)

func (b *Bot) handleGiftsCommand(ctx *telegohandler.Context, m telego.Message) error {
	if m.From == nil || m.Chat.ID != m.From.ID {
		return nil
	}
	args := strings.Fields(m.Text)
	if len(args) > 1 {
		b.claimGift(ctx, telego.ChatID{ID: m.Chat.ID}, m.From.ID, strings.TrimPrefix(args[1], "gift_"))
		return nil
	}
	text, kb := b.giftsScreen(ctx, m.From.ID)
	b.send(ctx, telego.ChatID{ID: m.Chat.ID}, text, kb)
	return nil
}
func (b *Bot) giftsScreen(ctx context.Context, id int64) (string, *telego.InlineKeyboardMarkup) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return "Не удалось открыть подарки.", keyboard(b.backRow())
	}
	plans, err := b.pay.SellablePlans(ctx)
	if err != nil {
		return "Каталог временно недоступен.", keyboard(b.backRow())
	}
	text := "<b>Подарки</b>\nКупите подписку и передайте ссылку другу. Срок начнётся при активации. Свой подарок активировать нельзя.\nАктивировать код: /gift gift_КОД"
	rows := [][]telego.InlineKeyboardButton{}
	for _, p := range plans {
		if p.GiftEnabled {
			rows = append(rows, []telego.InlineKeyboardButton{button(fmt.Sprintf("%s · %s", p.Title, priceText(p.Price, b.cfg.Current().Currency)), "gift:methods:"+p.ID)})
		}
	}
	gifts, err := b.db.Gifts(ctx, subject.ID)
	if err != nil {
		return "Не удалось загрузить историю подарков.", keyboard(b.backRow())
	}
	for i, g := range gifts {
		if i == 10 {
			break
		}
		state := "Ожидает оплаты"
		if g.ClaimedAt != nil {
			state = "Активирован"
		} else if g.Status == domain.OrderPaid || g.Status == domain.OrderProvisioned {
			state = "Готов к передаче"
			text += "\n\nКод: <code>gift_" + g.ID + "</code>"
			if b.cfg.Current().BotUsername != "" {
				text += "\nhttps://t.me/" + escapeHTML(b.cfg.Current().BotUsername) + "?start=gift_" + g.ID
			}
		}
		text += fmt.Sprintf("\n%s · %d дней · %s", escapeHTML(g.PlanID), g.Days, state)
	}
	if b.siteURL() != "" {
		rows = append(rows, []telego.InlineKeyboardButton{urlButton("Вся история подарков", b.siteURL()+"/gifts")})
	}
	rows = append(rows, b.backRow())
	return text, keyboard(rows...)
}
func (b *Bot) claimGift(ctx context.Context, chat telego.ChatID, id int64, code string) {
	subject, err := b.subject(ctx, id)
	if err != nil {
		return
	}
	order, err := b.pay.ClaimGift(ctx, subject, code)
	if err != nil {
		b.send(ctx, chat, escapeHTML(err.Error()), keyboard(b.backRow()))
		return
	}
	text := "Подарок активирован. Откройте подписку для подключения."
	if order.Status != domain.OrderProvisioned {
		text = "Подарок принят. Выдача продолжится автоматически, повторно активировать его не нужно."
	}
	b.send(ctx, chat, text, keyboard([]telego.InlineKeyboardButton{button("Моя подписка", "nav:subscription")}))
}
func (b *Bot) giftCallback(ctx context.Context, q string, chat telego.ChatID, mid int, id int64, action, rest string) {
	if chat.ID != id {
		b.answer(ctx, q, "Откройте бота в личном чате", true)
		return
	}
	switch action {
	case "methods":
		rows := [][]telego.InlineKeyboardButton{{button("С внутреннего баланса", "gift:balance:"+rest+","+rand.Text()[:16])}}
		for _, o := range b.pay.PaymentOptions(false) {
			rows = append(rows, []telego.InlineKeyboardButton{button(o.Label(), "gift:pay:"+rest+","+o.Token())})
		}
		rows = append(rows, []telego.InlineKeyboardButton{button("Назад", "gift:home")})
		b.upsert(ctx, chat, mid, "<b>Оплата подарка</b>\nВыберите способ оплаты.", keyboard(rows...))
	case "pay", "balance":
		plan, option, _ := strings.Cut(rest, ",")
		p := payments.Purchase{TelegramID: &id, PlanID: plan, Option: option, Gift: true}
		if action == "balance" {
			p.RequestID = option
		}
		var c domain.Checkout
		var err error
		if action == "balance" {
			c, err = b.pay.PurchaseWithBalance(ctx, p)
		} else {
			c, err = b.pay.Checkout(ctx, p)
		}
		if err != nil {
			b.answer(ctx, q, err.Error(), true)
			return
		}
		if c.URL != "" {
			b.upsert(ctx, chat, mid, "Счёт на подарок создан. После оплаты ссылка появится в подарках.", keyboard([]telego.InlineKeyboardButton{urlButton("Оплатить", c.URL)}, []telego.InlineKeyboardButton{button("Мои подарки", "gift:home")}))
		} else {
			text, kb := b.giftsScreen(ctx, id)
			b.upsert(ctx, chat, mid, text, kb)
		}
	default:
		text, kb := b.giftsScreen(ctx, id)
		b.upsert(ctx, chat, mid, text, kb)
	}
	b.answer(ctx, q, "", false)
}

func (b *Bot) staffQueue(ctx context.Context, id int64, scope string, page int) (string, *telego.InlineKeyboardMarkup) {
	if !b.isStaff(ctx, id) {
		return "Недостаточно прав.", keyboard(b.backRow())
	}
	queue, err := b.db.StaffQueue(ctx, id, scope, page)
	if err != nil {
		return "Очередь недоступна.", keyboard(b.backRow())
	}
	b.rememberTicketList(id, queue.Page, true)
	b.mu.Lock()
	p := b.ticketLists[id]
	p.scope = queue.Scope
	b.ticketLists[id] = p
	b.mu.Unlock()
	text := fmt.Sprintf("<b>Очередь поддержки</b>\nСтраница %d / %d · всего %d", queue.Page+1, queue.Pages, queue.Total)
	rows := [][]telego.InlineKeyboardButton{}
	for _, item := range []struct{ k, label string }{{"free", "Свободные"}, {"mine", "Мои"}, {"waiting", "Ждут"}, {"open", "Открытые"}, {"closed", "Закрытые"}} {
		rows = append(rows, []telego.InlineKeyboardButton{button(fmt.Sprintf("%s · %d", item.label, queue.Counts[item.k]), "queue:"+item.k+":0")})
	}
	for _, t := range queue.Tickets {
		text += fmt.Sprintf("\n\n#%d · %s\n%s", t.ID, escapeHTML(firstRunes(t.Subject, 60)), escapeHTML(firstRunes(t.Preview, 100)))
		rows = append(rows, []telego.InlineKeyboardButton{button(fmt.Sprintf("Открыть #%d", t.ID), fmt.Sprintf("ticket:%d", t.ID))})
	}
	rows = append(rows, []telego.InlineKeyboardButton{button("←", fmt.Sprintf("queue:%s:%d", queue.Scope, max(0, queue.Page-1))), button("→", fmt.Sprintf("queue:%s:%d", queue.Scope, min(queue.Pages-1, queue.Page+1)))}, []telego.InlineKeyboardButton{button("Панель", "admin:home")})
	return text, keyboard(rows...)
}
func (b *Bot) staffTicketCallback(ctx context.Context, q string, chat telego.ChatID, mid int, actor int64, action, rest string) {
	permission := "tickets." + action
	if action == "reopen" {
		permission = "tickets.close"
	}
	if chat.ID != actor || !b.db.Permission(ctx, actor, permission, b.cfg.Current().AdminIDs) {
		b.answer(ctx, q, "Недостаточно прав", true)
		return
	}
	raw, targetRaw, _ := strings.Cut(rest, ",")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return
	}
	target, _ := strconv.ParseInt(targetRaw, 10, 64)
	if action == "note" {
		b.setInput(actor, "staff_note", raw)
		b.upsert(ctx, chat, mid, "Напишите внутреннюю заметку. Клиент её не увидит. /cancel — отменить.", keyboard([]telego.InlineKeyboardButton{button("К обращению", "ticket:"+raw)}))
		b.answer(ctx, q, "", false)
		return
	}
	if action == "transfer" && target == 0 {
		staff, err := b.db.Staff(ctx)
		if err != nil {
			return
		}
		rows := [][]telego.InlineKeyboardButton{}
		for _, m := range staff {
			if m.TelegramID != actor {
				rows = append(rows, []telego.InlineKeyboardButton{button(fmt.Sprintf("%s · %d", m.DisplayName, m.TelegramID), fmt.Sprintf("staffticket:transfer:%d,%d", id, m.TelegramID))})
			}
		}
		b.upsert(ctx, chat, mid, "Кому передать обращение?", keyboard(rows...))
		b.answer(ctx, q, "", false)
		return
	}
	if err = b.db.StaffTicketAction(ctx, id, actor, action, "", target); err != nil {
		b.answer(ctx, q, err.Error(), true)
		return
	}
	text, kb := b.ticketScreen(ctx, actor, id)
	b.upsertTicket(ctx, chat, mid, text, kb)
	b.answer(ctx, q, "Обращение обновлено", false)
}
