package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

type paymentChecker interface {
	CheckPayment(context.Context, domain.Order) (Event, error)
}

func (p *YooKassaProvider) CheckPayment(ctx context.Context, order domain.Order) (Event, error) {
	if deref(order.PaymentLinkID) == "" {
		return Event{}, ErrNoEvent
	}
	body, _ := json.Marshal(map[string]any{"type": "notification", "event": "payment.succeeded",
		"object": map[string]string{"id": *order.PaymentLinkID}})
	return p.Verify(ctx, body, nil)
}

// Protocol: https://docs.platega.io/ (GET /transaction/{id}).
func (p *PlategaProvider) CheckPayment(ctx context.Context, order domain.Order) (Event, error) {
	// A recurring checkout ID identifies a subscription, not a transaction.
	// Only authenticated debit callbacks may grant time for this checkout.
	if order.RecurringAgreementID != nil {
		return Event{}, ErrNoEvent
	}
	if deref(order.PaymentLinkID) == "" {
		return Event{}, ErrNoEvent
	}
	data, err := p.api.do(ctx, http.MethodGet, "/transaction/"+url.PathEscape(*order.PaymentLinkID), nil, nil)
	if err != nil {
		return Event{}, err
	}
	if id := jsonString(data["id"]); id != *order.PaymentLinkID {
		return Event{}, fmt.Errorf("Platega вернула другой платёж")
	}
	if payload := jsonString(data["payload"]); payload != order.OrderID {
		return Event{}, fmt.Errorf("Platega вернула другой заказ")
	}
	if jsonString(data["status"]) != "CONFIRMED" {
		return Event{}, ErrNoEvent
	}
	money, _ := data["paymentDetails"].(map[string]any)
	cents := moneyFromMajor(money["amount"])
	if cents == nil || jsonString(money["currency"]) == "" {
		return Event{}, ErrVerification
	}
	amount := domain.Money(*cents)
	return Event{OrderID: order.OrderID, Status: StatusPaid, TransactionID: *order.PaymentLinkID,
		Amount: &amount, Currency: jsonString(money["currency"])}, nil
}

// Protocol: https://api.wata.pro/transactions (search successful payments by orderId).
func (p *WataProvider) CheckPayment(ctx context.Context, order domain.Order) (Event, error) {
	token := p.token
	if methodToken := p.methodTokens[deref(order.PaymentMethod)]; methodToken != "" {
		token = methodToken
	}
	data, err := p.api.do(ctx, http.MethodGet, "/v2/transactions?orderId="+url.QueryEscape(order.OrderID)+
		"&statuses=Paid&maxResultCount=100", nil, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return Event{}, err
	}
	items, _ := data["items"].([]any)
	for _, item := range items {
		transaction, _ := item.(map[string]any)
		if jsonString(transaction["orderId"]) != order.OrderID || jsonString(transaction["status"]) != "Paid" ||
			jsonString(transaction["kind"]) != "Payment" {
			continue
		}
		cents := moneyFromMajor(transaction["amount"])
		if cents == nil || jsonString(transaction["currency"]) == "" {
			return Event{}, ErrVerification
		}
		amount := domain.Money(*cents)
		return Event{OrderID: order.OrderID, Status: StatusPaid, TransactionID: jsonString(transaction["id"]),
			Amount: &amount, Currency: jsonString(transaction["currency"]), PaidAt: parseISOTime(transaction["paymentTime"])}, nil
	}
	return Event{}, ErrNoEvent
}

func (s *Service) ReconcilePayments(ctx context.Context, limit int) error {
	orders, err := s.db.OrdersForPaymentCheck(ctx, limit)
	if err != nil {
		return err
	}
	for _, order := range orders {
		if err := s.checkProviderPayment(ctx, order); err != nil {
			slog.Warn("сверка платежа", "order", order.OrderID, "err", err)
		}
	}
	return nil
}

func (s *Service) checkProviderPayment(ctx context.Context, order domain.Order) error {
	if s.registry == nil {
		return nil
	}
	provider, ok := s.registry.Get(order.Provider)
	if !ok {
		return nil
	}
	checker, ok := provider.(paymentChecker)
	if !ok {
		return nil
	}
	claimed, err := s.db.ClaimPaymentCheck(ctx, order.OrderID)
	if err != nil || !claimed {
		return err
	}
	event, err := checker.CheckPayment(ctx, order)
	if errors.Is(err, ErrNoEvent) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.ReceiveEvent(ctx, order.Provider, event)
	return err
}
