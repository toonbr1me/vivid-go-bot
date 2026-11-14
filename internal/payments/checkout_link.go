package payments

import (
	"context"
	"errors"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func (s *Service) createPaymentOrder(ctx context.Context, provider Provider, order domain.Order, request CreateRequest) (Link, error) {
	// Сначала фиксируем обещанные условия: вебхук может прийти в момент
	// создания ссылки, а таймаут API не означает, что счёт не создан.
	if _, err := s.db.CreateOrder(ctx, order); err != nil {
		return Link{}, err
	}
	if request.Recurring {
		subject, err := s.subject(ctx, order.TelegramID, order.AccountID)
		if err != nil {
			return Link{}, err
		}
		if err = s.db.CreateRecurring(ctx, domain.RecurringAgreement{ID: order.OrderID, SubjectID: subject.ID, Provider: order.Provider, Method: request.Method, PlanID: order.PlanID, Amount: order.Amount, Currency: order.Currency, Days: request.PlanDays, DeviceCount: order.DeviceCount}); err != nil {
			return Link{}, err
		}
	}
	link, err := provider.CreateLink(ctx, request)
	if err != nil {
		return Link{}, err
	}
	if link.URL == "" {
		return Link{}, errors.New("платёжный шлюз не вернул ссылку")
	}
	if link.ExpiresAt == nil {
		expiry := time.Now().Add(request.ExpiresIn)
		link.ExpiresAt = &expiry
	}
	if err := s.db.SetPaymentLink(ctx, order.OrderID, link.URL, link.ID, link.ExpiresAt); err != nil {
		return Link{}, err
	}
	if request.Recurring && order.Provider == "platega" {
		if err := s.db.SetRecurringExternal(ctx, order.OrderID, link.ID, false); err != nil {
			return Link{}, err
		}
	}
	return link, nil
}
