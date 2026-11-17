package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func (b *Bot) operationsCallback(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, id int64, action, orderID string) {
	if chatID.ID != id || !b.db.Permission(ctx, id, "orders.view", b.cfg.Current().AdminIDs) {
		b.answer(ctx, queryID, "Недостаточно прав", true)
		return
	}
	b.answer(ctx, queryID, "", false)
	back := []telego.InlineKeyboardButton{button("К заказам", "ops:list"), button("Пульт", "admin:home")}
	if action == "list" {
		orders, err := b.db.AttentionOrders(ctx, 150)
		if err != nil {
			b.upsert(ctx, chatID, messageID, "Не удалось получить заказы", keyboard(back))
			return
		}
		text := "<b>Требует внимания</b>\n\nПервые 6 заказов из всей очереди. Повтор выдачи не создаёт новый платёж."
		if ops, e := b.db.Operations(ctx); e == nil {
			text += fmt.Sprintf("\nЖдут выдачи: %d\nЖдут оплаты больше 30 минут: %d\nОшибки автопродления: %d", ops.AwaitingAccess, ops.StuckPayments, ops.RecurringErrors)
		}
		rows := [][]telego.InlineKeyboardButton{}
		for _, order := range orders {
			{
				rows = append(rows, []telego.InlineKeyboardButton{button(shortID(order.OrderID)+" · "+priceText(order.Amount, order.Currency), "ops:inspect:"+order.OrderID)})
				if len(rows) == 6 {
					break
				}
			}
		}
		if site := b.siteURL(); site != "" {
			rows = append(rows, []telego.InlineKeyboardButton{urlButton("Все проблемы и автопродления", site+"/admin/orders?attention=1")})
		}
		if len(rows) == 0 {
			text += "\n\nТаких заказов нет."
		}
		rows = append(rows, []telego.InlineKeyboardButton{button("Обновить", "ops:list"), button("Пульт", "admin:home")})
		b.upsert(ctx, chatID, messageID, text, keyboard(rows...))
		return
	}
	order, err := b.db.OrderByID(ctx, orderID)
	if err != nil {
		b.upsert(ctx, chatID, messageID, "Заказ не найден", keyboard(back))
		return
	}
	if action == "retry" {
		if !b.db.Permission(ctx, id, "orders.retry", b.cfg.Current().AdminIDs) || order.Status != domain.OrderPaid {
			b.upsert(ctx, chatID, messageID, "Повтор разрешён только для оплаченного, ещё не выданного заказа.", keyboard(back))
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		_ = b.db.LogAction(ctx, id, "order.retry", order.OrderID, 0, "Повтор выдачи из Telegram")
		updated, err := b.pay.CheckOrder(callCtx, order.OrderID)
		if err != nil {
			b.upsert(ctx, chatID, messageID, "<b>Выдача пока не завершена</b>\n\nОплаченный заказ сохранён. Можно повторить проверку позже.", keyboard(back))
			return
		}
		order = updated
	}
	text := fmt.Sprintf("<b>Заказ %s</b>\n\nСтатус: %s\nТариф: %s\nСумма: %s", escapeHTML(shortID(order.OrderID)), escapeHTML(orderStatusLabel(order.Status)), escapeHTML(order.PlanID), priceText(order.Amount, order.Currency))
	rows := [][]telego.InlineKeyboardButton{}
	if order.Status == domain.OrderPaid {
		rows = append(rows, []telego.InlineKeyboardButton{button("Повторить выдачу", "ops:retry:"+order.OrderID)})
	}
	rows = append(rows, back)
	b.upsert(ctx, chatID, messageID, text, keyboard(rows...))
}
