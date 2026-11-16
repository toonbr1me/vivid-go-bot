package payments

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Platega — эквайринг с несколькими способами оплаты под одним кабинетом.
// Уведомление не подписано, но приходит с парой заголовков кабинета: их и
// сверяем.
type PlategaProvider struct {
	api        *apiClient
	merchantID string
	secret     string
	methods    []string
}

var plategaMethods = map[string]int{
	"card":          11,
	"sbp":           2,
	"erip":          3,
	"international": 12,
	"crypto":        13,
}

func NewPlategaProvider(baseURL, merchantID, secret string, methods []string, timeout time.Duration) *PlategaProvider {
	client := newAPIClient(baseURL, timeout)
	client.headers["X-MerchantId"] = merchantID
	client.headers["X-Secret"] = secret
	supported := make([]string, 0, len(methods))
	for _, method := range methods {
		if _, ok := plategaMethods[method]; ok {
			supported = append(supported, method)
		}
	}
	return &PlategaProvider{api: client, merchantID: merchantID, secret: secret, methods: supported}
}

func (p *PlategaProvider) Name() string       { return "platega" }
func (p *PlategaProvider) Methods() []string  { return append([]string(nil), p.methods...) }
func (p *PlategaProvider) TelegramOnly() bool { return false }

func (p *PlategaProvider) CreateLink(ctx context.Context, req CreateRequest) (Link, error) {
	method := req.Method
	if method == "" && len(p.methods) > 0 {
		method = p.methods[0]
	}
	methodID, ok := plategaMethods[method]
	if !ok {
		return Link{}, fmt.Errorf("Platega не поддерживает способ оплаты %q", method)
	}
	payload := map[string]any{
		"paymentMethod": methodID,
		"paymentDetails": map[string]any{
			"amount":   float64(req.Amount) / 100,
			"currency": req.Currency,
		},
		"description": req.Description,
		"return":      req.SuccessURL,
		"failedUrl":   req.FailURL,
		"payload":     req.OrderID,
	}
	if req.Recurring {
		if req.Method != "sbp" || req.PlanDays < 1 || req.PlanDays > 730 {
			return Link{}, fmt.Errorf("рекуррентная Platega: только СБП, срок 1–730 дней")
		}
		payload["paymentMethod"] = 6
		payload["paymentDetails"] = map[string]any{"amount": float64(req.Amount) / 100, "currency": req.Currency, "interval": 1, "intervalCount": req.PlanDays}
		payload["metadata"] = map[string]any{"user_email": req.CustomerEmail}
	}
	data, err := p.api.do(ctx, http.MethodPost, "/transaction/process", payload, nil)
	if err != nil {
		return Link{}, err
	}
	url := jsonString(data["redirect"])
	if url == "" {
		return Link{}, fmt.Errorf("Platega не вернула ссылку на оплату")
	}
	expires := time.Now().UTC().Add(req.ExpiresIn)
	return Link{URL: url, ID: jsonString(data["transactionId"]), ExpiresAt: &expires}, nil
}

func (p *PlategaProvider) Verify(_ context.Context, body []byte, headers http.Header) (Event, error) {
	merchantID := headerValue(headers, "X-MerchantId")
	secret := headerValue(headers, "X-Secret")
	if merchantID == "" || secret == "" {
		return Event{}, fmt.Errorf("%w: нет заголовков кабинета", ErrVerification)
	}
	if merchantID != p.merchantID || secret != p.secret {
		return Event{}, fmt.Errorf("%w: чужие учётные данные", ErrVerification)
	}
	var raw map[string]any
	if err := decodeJSON(body, &raw); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrVerification, err)
	}
	var status Status
	switch jsonString(raw["status"]) {
	case "CONFIRMED":
		status = StatusPaid
	case "CANCELED":
		status = StatusFailed
	default:
		return Event{}, ErrNoEvent
	}
	event := Event{
		OrderID:       jsonString(raw["payload"]),
		Status:        status,
		TransactionID: jsonString(raw["id"]),
		Currency:      jsonString(raw["currency"]),
	}
	event.SubscriptionID = jsonString(raw["subscriptionId"])
	event.NextChargeAt = parseISOTime(raw["nextChargeAt"])
	if status == StatusPaid {
		now := time.Now().UTC()
		event.PaidAt = &now
	}
	if cents := moneyFromMajor(raw["amount"]); cents != nil {
		amount := domain.Money(*cents)
		event.Amount = &amount
	}
	if status == StatusPaid && (event.TransactionID == "" || event.Amount == nil || *event.Amount <= 0 || event.Currency == "") {
		return Event{}, fmt.Errorf("%w: нет подтверждённой суммы, валюты или транзакции", ErrVerification)
	}
	if event.OrderID == "" && event.SubscriptionID == "" {
		return Event{}, ErrNoEvent
	}
	return event, nil
}

func (p *PlategaProvider) CancelRecurring(ctx context.Context, id string) error {
	_, err := p.api.do(ctx, http.MethodPost, "/subscription/"+id+"/cancel", nil, nil)
	return err
}
