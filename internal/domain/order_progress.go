package domain

import "time"

// OrderProgress describes confirmed state, never the redirect from a gateway.
type OrderProgress struct {
	Label, Detail, Tone           string
	Ready, CanPay, NeedsAttention bool
}

func (o Order) Progress(now time.Time) OrderProgress {
	switch o.Status {
	case OrderProvisioned:
		if o.Kind == KindGift {
			return OrderProgress{Label: "Подарок готов", Detail: "Ссылка на подарок доступна в разделе подарков.", Tone: "success", Ready: true}
		}
		if o.Kind == KindBalance {
			return OrderProgress{Label: "Баланс пополнен", Detail: "Средства доступны в аккаунте.", Tone: "success"}
		}
		if o.Kind == KindDevices {
			return OrderProgress{Label: "Устройства добавлены", Detail: "Дополнительные слоты доступны в разделе устройств.", Tone: "success", Ready: true}
		}
		return OrderProgress{Label: "Доступ выдан", Detail: "Всё готово. Можно подключаться.", Tone: "success", Ready: true}
	case OrderPaid:
		p := OrderProgress{Label: "Оплачено · выдаём доступ", Detail: "Оплата подтверждена. Повторно платить не нужно.", Tone: "warning"}
		if o.FulfillmentError != nil && *o.FulfillmentError != "" || o.PaidAt != nil && now.Sub(*o.PaidAt) >= 2*time.Minute {
			p.Label, p.Detail, p.NeedsAttention = "Выдача задерживается", "Оплата сохранена. Мы повторим выдачу автоматически; можно обратиться в поддержку по этому заказу.", true
		}
		return p
	case OrderPending:
		if o.PaymentExpiresAt != nil && !o.PaymentExpiresAt.After(now) {
			return OrderProgress{Label: "Время оплаты истекло", Detail: "Если вы уже оплатили, проверьте статус. Иначе создайте новый заказ.", Tone: "muted"}
		}
		return OrderProgress{Label: "Ожидаем оплату", Detail: "После оплаты нажмите «Проверить статус».", Tone: "warning", CanPay: o.PaymentURL != nil && *o.PaymentURL != ""}
	case OrderFailed:
		return OrderProgress{Label: "Платёж отклонён", Detail: "Можно выбрать другой способ оплаты. Если деньги списались, обратитесь в поддержку.", Tone: "danger"}
	case OrderCancelled:
		return OrderProgress{Label: "Заказ отменён", Detail: "Можно создать новый заказ. Если деньги списались, обратитесь в поддержку.", Tone: "muted"}
	case OrderRefunded:
		return OrderProgress{Label: "Возврат", Detail: "Возврат оформлен. Срок зачисления зависит от банка.", Tone: "muted"}
	default:
		return OrderProgress{Label: "Проверяем статус", Detail: "Обновите заказ или обратитесь в поддержку.", Tone: "muted"}
	}
}
