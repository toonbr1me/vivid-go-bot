package domain

import "time"

// CustomerGuidance is independent of staff/partner roles and UI transport.
type CustomerGuidance struct {
	Title, Detail, Action, Label, OrderID string
}

func GuideCustomer(access Access, focus *Order, recurring *RecurringAgreement, trialAvailable bool, now time.Time) CustomerGuidance {
	if focus != nil && focus.Status == OrderPaid && focus.ProvisionedAt == nil {
		p := focus.Progress(now)
		return CustomerGuidance{Title: p.Label, Detail: p.Detail, Action: "order", Label: "Проверить выдачу", OrderID: focus.OrderID}
	}
	if access.Active {
		if recurring != nil && (recurring.Enabled || recurring.StopReason != "user") {
			return CustomerGuidance{Title: "Автопродление", Detail: recurring.StatusText(), Action: "recurring", Label: "Управлять автопродлением"}
		}
		if focus != nil && focus.Provider == "trial" {
			return CustomerGuidance{Title: "Пробный доступ", Detail: "Подключите устройство и проверьте работу сервиса. Затем можно выбрать платную подписку.", Action: "connect", Label: "Подключиться"}
		}
		if access.ExpireAt != nil && access.ExpireAt.Sub(now) <= 24*time.Hour {
			return CustomerGuidance{Title: "Подписка заканчивается", Detail: "Продлите доступ заранее. Оставшийся оплаченный срок сохранится.", Action: "buy", Label: "Продлить подписку"}
		}
		if !access.Found {
			return CustomerGuidance{Title: "Оплаченный срок сохранён", Detail: "Текущее состояние подключения временно недоступно. Показываем последний известный срок.", Action: "support", Label: "Помощь с подключением"}
		}
		return CustomerGuidance{Title: "Подписка активна", Detail: "Подключайте устройства и управляйте доступом.", Action: "connect", Label: "Подключить устройство"}
	}
	if focus != nil && focus.Status == OrderPending && focus.Progress(now).CanPay {
		return CustomerGuidance{Title: "Ожидаем оплату", Detail: "У вас уже есть счёт. Если вы оплатили, проверьте его статус.", Action: "order", Label: "Открыть заказ", OrderID: focus.OrderID}
	}
	if access.ExpireAt != nil {
		if access.ExpireAt.After(now) {
			return CustomerGuidance{Title: "Доступ требует проверки", Detail: "Срок подписки ещё не истёк. Проверьте ограничения или обратитесь в поддержку.", Action: "support", Label: "Проверить доступ с поддержкой"}
		}
		return CustomerGuidance{Title: "Подписка истекла", Detail: "Продлите подписку, чтобы восстановить доступ.", Action: "buy", Label: "Восстановить доступ"}
	}
	if trialAvailable {
		return CustomerGuidance{Title: "Попробуйте сервис", Detail: "Начните с пробного доступа и подключите первое устройство.", Action: "trial", Label: "Попробовать бесплатно"}
	}
	return CustomerGuidance{Title: "Ваш доступ", Detail: "Выберите подписку или обратитесь в поддержку, если уже оплатили.", Action: "buy", Label: "Выбрать подписку"}
}
