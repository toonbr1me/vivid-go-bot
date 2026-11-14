package domain

import "time"

type RecurringAgreement struct {
	ID                       string
	SubjectID                int64
	Provider, Method, PlanID string
	Amount                   Money
	Currency                 string
	Days                     int
	DeviceCount              *int
	ExternalID               string
	Enabled, Activated       bool
	NextChargeAt             *time.Time
	PendingOrderID           *string
	LastError                string
	StopReason               string
	StoppedAt                *time.Time
}

func (a RecurringAgreement) ProviderName() string {
	switch a.Provider {
	case "yookassa":
		return "ЮKassa"
	case "platega":
		return "Platega"
	default:
		return a.Provider
	}
}

// StatusText deliberately excludes gateway diagnostics and identifiers.
func (a RecurringAgreement) StatusText() string {
	if !a.Enabled {
		switch a.StopReason {
		case "user":
			return "Отключено вами"
		case "declined":
			return "Остановлено: платёж отклонён. Для возобновления выберите тариф заново."
		case "terms":
			return "Условия тарифа изменились. Выберите тариф заново и подтвердите новые условия."
		default:
			return "Приостановлено: требуется проверка платежа в поддержке. Повторное списание не запускается."
		}
	}
	if a.LastError == "declined" {
		return "Последнее списание отклонено. Автопродление остаётся включено: проверьте способ оплаты или отключите его."
	}
	if a.LastError != "" {
		return "Требуется внимание: проверьте заказ или обратитесь в поддержку."
	}
	if !a.Activated {
		return "Ожидает подтверждения способа оплаты"
	}
	if a.PendingOrderID != nil {
		return "Проверяем очередной платёж"
	}
	return "Автопродление включено"
}
