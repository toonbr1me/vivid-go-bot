package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// ErrInsufficientBalance — на балансе меньше цены тарифа.
var ErrInsufficientBalance = storage.ErrInsufficientBalance

// PurchaseWithBalance списывает цену тарифа с внутреннего баланса.
//
// Баланс пополняется реферальными начислениями, поэтому отдельного шлюза у
// него нет: заказ пишется сразу оплаченным, а выдача идёт обычным путём.
func (s *Service) PurchaseWithBalance(ctx context.Context, purchase Purchase) (domain.Checkout, error) {
	plan, err := s.Plan(ctx, purchase.PlanID)
	if err != nil {
		return domain.Checkout{}, err
	}
	if plan.IsTrial || !plan.IsActive {
		return domain.Checkout{}, ErrPlanUnavailable
	}
	if purchase.Gift && (!plan.GiftEnabled || purchase.Recurring || purchase.PromoCode != "") {
		return domain.Checkout{}, ErrPlanUnavailable
	}
	kind := domain.KindSubscription
	if purchase.Gift {
		kind = domain.KindGift
	}
	subject, err := s.subject(ctx, purchase.TelegramID, purchase.AccountID)
	if err != nil {
		return domain.Checkout{}, err
	}
	giftOrderID := ""
	if purchase.Gift && purchase.RequestID != "" {
		if len(purchase.RequestID) < 16 || len(purchase.RequestID) > 128 {
			return domain.Checkout{}, errors.New("неверный идентификатор покупки")
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", subject.ID, purchase.RequestID)))
		giftOrderID = "gift-buy-" + hex.EncodeToString(sum[:16])
		if existing, e := s.db.OrderByID(ctx, giftOrderID); e == nil {
			if existing.AccountID == nil || *existing.AccountID != subject.ID || existing.Kind != domain.KindGift || existing.PlanID != plan.ID {
				return domain.Checkout{}, ErrPlanUnavailable
			}
			return domain.Checkout{OrderID: existing.OrderID, Amount: existing.Amount, Free: true, Reused: true}, nil
		} else if !errors.Is(e, storage.ErrNotFound) {
			return domain.Checkout{}, e
		}
	}
	outcome, err := s.applyPromo(ctx, subject.ID, plan, purchase.PromoCode)
	if err != nil {
		return domain.Checkout{}, err
	}
	orderID := s.newOrderID(purchase.TelegramID, purchase.AccountID)
	if giftOrderID != "" {
		orderID = giftOrderID
	}
	if outcome.Amount <= 0 {
		return s.settleFreeOrder(ctx, orderID, subject, purchase, plan, outcome)
	}

	balance, err := s.db.Balance(ctx, subject.ID)
	if err != nil {
		return domain.Checkout{}, err
	}
	if balance < outcome.Amount {
		return domain.Checkout{}, ErrInsufficientBalance
	}

	created, err := s.db.CreateBalanceOrder(ctx, subject.ID, domain.Order{
		OrderID:        orderID,
		Provider:       "balance",
		TelegramID:     subject.TelegramID,
		AccountID:      accountIDPtr(subject, purchase),
		Username:       ptrOrNil(purchase.Username),
		PlanID:         plan.ID,
		PlanProfile:    s.snapshotPlan(plan),
		Kind:           kind,
		Amount:         outcome.Amount,
		OriginalAmount: outcome.OriginalAmount,
		Currency:       s.cfg.Current().Currency,
		PaymentMethod:  ptrOrNil("balance"),
		Status:         domain.OrderPaid,
		PromoCode:      ptrOrNil(outcome.Code),
		PlanDays:       intPtr(plan.Days),
		PromoBonusDays: intPtr(outcome.BonusDays),
		ReferrerID:     purchase.ReferrerID,
		PartnerID:      outcome.PartnerID,
		RewardType:     outcome.RewardType,
		RewardValue:    outcome.RewardValue,
		RewardScope:    outcome.RewardScope,
		DeviceCount:    giftDevices(purchase.Gift, s.planExtraDevices(plan)),
	})
	if err != nil {
		return domain.Checkout{}, err
	}
	// Ошибку панели не показываем: деньги уже списаны, заказ оплачен, а
	// фоновая задача доведёт выдачу. Второй раз брать с человека нельзя.
	_, _ = s.Provision(ctx, created)
	return domain.Checkout{OrderID: orderID, Amount: created.Amount, Free: true}, nil
}

// GrantDays выдаёт дни доступа без оплаты и пишет служебный заказ: выданное
// должно быть видно и в истории человека, и в статистике.
func (s *Service) GrantDays(ctx context.Context, subject domain.Subject, days int, provider string) (*time.Time, error) {
	if days <= 0 {
		return nil, errors.New("дней должно быть больше нуля")
	}
	if provider == "" {
		provider = "manual_grant"
	}
	if subject.TelegramID == nil && subject.RemnawaveUser == nil {
		return nil, errors.New("у веб-аккаунта нет имени подписки")
	}
	username := fallbackUsername(subject)
	created, err := s.db.CreateOrder(ctx, domain.Order{
		OrderID:       s.newOrderID(subject.TelegramID, nil),
		Provider:      provider,
		TelegramID:    subject.TelegramID,
		AccountID:     accountIDPtr(subject, Purchase{}),
		Username:      ptrOrNil(username),
		PlanID:        "gift",
		Kind:          domain.KindSubscription,
		Amount:        0,
		Currency:      s.cfg.Current().Currency,
		PaymentMethod: ptrOrNil(provider),
		Status:        domain.OrderPaid,
		PlanDays:      intPtr(days),
	})
	if err != nil {
		return nil, err
	}
	result, err := s.Provision(ctx, created)
	if err != nil {
		return nil, err
	}
	return result.ExpireAt, nil
}

// CheckOrder доводит оплаченный, но не выданный заказ и возвращает его
// актуальное состояние. Используется кнопкой «Я оплатил».
func (s *Service) CheckOrder(ctx context.Context, orderID string) (domain.Order, error) {
	order, err := s.db.OrderByID(ctx, orderID)
	if err != nil {
		return domain.Order{}, err
	}
	if order.Status == domain.OrderPending || order.Status == domain.OrderFailed || order.Status == domain.OrderCancelled {
		if err := s.checkProviderPayment(ctx, order); err != nil {
			return order, err
		}
		order, err = s.db.OrderByID(ctx, orderID)
		if err != nil {
			return domain.Order{}, err
		}
	}
	if order.Status == domain.OrderPaid && order.ProvisionedAt == nil {
		if _, err := s.Provision(ctx, order); err != nil {
			return order, fmt.Errorf("выдача отложена: %w", err)
		}
		return s.db.OrderByID(ctx, orderID)
	}
	return order, nil
}

// OrderHistory — последние покупки субъекта для экрана «История».
func (s *Service) OrderHistory(ctx context.Context, subject domain.Subject, limit int) ([]domain.Order, error) {
	if limit <= 0 {
		limit = 10
	}
	if subject.TelegramID != nil {
		return s.db.OrdersForTelegram(ctx, *subject.TelegramID, limit)
	}
	return s.db.OrdersForAccount(ctx, subject.ID, limit)
}

func giftDevices(gift bool, n *int) *int {
	if gift && n == nil {
		return new(int)
	}
	return n
}
