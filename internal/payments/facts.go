package payments

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Facts — сведения о сервисе, которые помощник поддержки вправе называть.
//
// Отдельный тип: помощник должен знать ровно столько, сколько можно
// показать человеку, и ни байтом больше.
type Facts struct {
	svc *Service
}

// Facts возвращает сборщик сведений о сервисе.
func (s *Service) Facts() *Facts { return &Facts{svc: s} }

// ServiceFacts — что можно рассказать любому: тарифы, пробный период,
// способы оплаты и лимит устройств.
func (f *Facts) ServiceFacts(ctx context.Context) (string, error) {
	if f.svc == nil {
		return "", nil
	}
	lines := make([]string, 0, 8)

	plans, err := f.svc.SellablePlans(ctx)
	if err != nil {
		return "", err
	}
	if len(plans) > 0 {
		rows := make([]string, 0, len(plans))
		for _, plan := range plans {
			rows = append(rows, fmt.Sprintf("- %s: %s за %d %s",
				plan.Title, f.svc.moneyText(plan.Price), plan.Days, daysWord(plan.Days)))
		}
		lines = append(lines, "Тарифы:\n"+strings.Join(rows, "\n"))
	}

	trial := make([]string, 0, 2)
	if f.svc.cfg.Current().TrialBotDays > 0 {
		trial = append(trial, fmt.Sprintf("в боте — %d %s", f.svc.cfg.Current().TrialBotDays, daysWord(f.svc.cfg.Current().TrialBotDays)))
	}
	if f.svc.cfg.Current().TrialWebDays > 0 && f.svc.cfg.Current().SiteEnabled {
		trial = append(trial, fmt.Sprintf("на сайте — %d %s", f.svc.cfg.Current().TrialWebDays, daysWord(f.svc.cfg.Current().TrialWebDays)))
	}
	if len(trial) > 0 {
		lines = append(lines, "Пробный период: "+strings.Join(trial, ", ")+".")
	}

	options := f.svc.PaymentOptions(false)
	if len(options) > 0 {
		methods := make([]string, 0, len(options))
		seen := map[string]bool{}
		for _, option := range options {
			if seen[option.Method] {
				continue
			}
			seen[option.Method] = true
			methods = append(methods, option.Method)
		}
		lines = append(lines, "Способы оплаты: "+strings.Join(methods, ", ")+".")
	}
	if f.svc.cfg.Current().DeviceLimitEnabled {
		lines = append(lines, fmt.Sprintf("В тариф входит %d устройств; дополнительные слоты докупаются на срок подписки.",
			f.svc.cfg.Current().DeviceLimitBase))
	}
	if f.svc.cfg.Current().RefBalancePercent > 0 {
		lines = append(lines, fmt.Sprintf("За приглашённых начисляется %d%% от их заказа на внутренний баланс.",
			f.svc.cfg.Current().RefBalancePercent))
	}
	return strings.Join(lines, "\n"), nil
}

// UserFacts — что можно рассказать конкретному клиенту: его подписка,
// баланс и последние заказы.
func (f *Facts) UserFacts(ctx context.Context, subjectID int64) (string, error) {
	if f.svc == nil {
		return "", nil
	}
	subject, err := f.svc.db.SubjectByID(ctx, subjectID)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, 6)

	access, accessErr := f.svc.Access(ctx, subject, fallbackUsername(subject))
	if accessErr == nil && access.Found && access.ExpireAt != nil {
		state := "действует"
		if !access.Active {
			state = "истекла"
		}
		lines = append(lines, fmt.Sprintf("Подписка %s до %s.", state,
			access.ExpireAt.In(time.Local).Format("02.01.2006")))
		if access.DeviceLimit > 0 {
			lines = append(lines, fmt.Sprintf("Лимит устройств: %d.", access.DeviceLimit))
		}
	} else {
		lines = append(lines, "Активной подписки сейчас нет.")
	}

	if balance, err := f.svc.db.Balance(ctx, subjectID); err == nil && balance > 0 {
		lines = append(lines, fmt.Sprintf("Внутренний баланс: %s.", f.svc.moneyText(balance)))
	}

	if orders, err := f.svc.db.OrdersForAccount(ctx, subjectID, 3); err == nil && len(orders) > 0 {
		rows := make([]string, 0, len(orders))
		for _, order := range orders {
			rows = append(rows, fmt.Sprintf("- %s от %s: %s, %s",
				order.OrderID, order.CreatedAt.In(time.Local).Format("02.01 15:04"),
				f.svc.moneyText(order.Amount), orderStatusText(order.Status)))
		}
		lines = append(lines, "Последние заказы:\n"+strings.Join(rows, "\n"))
	}
	return strings.Join(lines, "\n"), nil
}

// moneyText — сумма словами: «1 234 ₽».
func (s *Service) moneyText(amount domain.Money) string {
	value := int64(amount)
	negative := value < 0
	if negative {
		value = -value
	}
	text := fmt.Sprintf("%d", value/100)
	if cents := value % 100; cents != 0 {
		text += fmt.Sprintf(",%02d", cents)
	}
	if negative {
		text = "−" + text
	}
	return text + " ₽"
}

// daysWord — «день», «дня», «дней».
func daysWord(days int) string {
	switch {
	case days%10 == 1 && days%100 != 11:
		return "день"
	case days%10 >= 2 && days%10 <= 4 && (days%100 < 12 || days%100 > 14):
		return "дня"
	default:
		return "дней"
	}
}

// orderStatusText — статус заказа словами.
func orderStatusText(status string) string {
	switch status {
	case domain.OrderPending:
		return "ждёт оплаты"
	case domain.OrderPaid:
		return "оплачен, выдаём"
	case domain.OrderProvisioned:
		return "готов"
	case domain.OrderFailed:
		return "не прошёл"
	case domain.OrderCancelled:
		return "отменён"
	case domain.OrderRefunded:
		return "возврат"
	default:
		return status
	}
}
