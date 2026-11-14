package payments

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// YooKassa не подписывает уведомления. Тело считаем только сигналом «сходи и
// посмотри»: идентификатор платежа берём из него, а статус, сумму и наш номер
// заказа — из ответа API. Подделка стоит нам одного лишнего запроса.
type YooKassaProvider struct {
	api     *apiClient
	methods []string
	shopID  string
}

var yookassaMethodTypes = map[string]string{
	"card":     "bank_card",
	"sbp":      "sbp",
	"sberpay":  "sberbank",
	"tpay":     "tinkoff_bank",
	"yoomoney": "yoo_money",
}

func NewYooKassaProvider(baseURL, shopID, secret string, methods []string, timeout time.Duration) *YooKassaProvider {
	client := newAPIClient(baseURL, timeout)
	token := base64.StdEncoding.EncodeToString([]byte(shopID + ":" + secret))
	client.headers["Authorization"] = "Basic " + token
	supported := make([]string, 0, len(methods))
	for _, method := range methods {
		if _, ok := yookassaMethodTypes[method]; ok {
			supported = append(supported, method)
		}
	}
	return &YooKassaProvider{api: client, methods: supported, shopID: shopID}
}

func (p *YooKassaProvider) Name() string       { return "yookassa" }
func (p *YooKassaProvider) Methods() []string  { return append([]string(nil), p.methods...) }
func (p *YooKassaProvider) TelegramOnly() bool { return false }

func (p *YooKassaProvider) CreateLink(ctx context.Context, req CreateRequest) (Link, error) {
	description := req.Description
	if runes := []rune(description); len(runes) > 128 {
		description = string(runes[:128])
	}
	payload := map[string]any{
		"amount": map[string]any{
			"value":    majorString(int64(req.Amount)),
			"currency": req.Currency,
		},
		"capture": true,
		"confirmation": map[string]any{
			"type":       "redirect",
			"return_url": req.SuccessURL,
		},
		"description": description,
		"metadata":    map[string]any{"order_id": req.OrderID},
	}
	if methodType, ok := yookassaMethodTypes[req.Method]; ok {
		payload["payment_method_data"] = map[string]any{"type": methodType}
	}
	if req.Recurring {
		payload["save_payment_method"] = true
	}
	if req.SavedMethodID != "" {
		delete(payload, "confirmation")
		delete(payload, "payment_method_data")
		delete(payload, "save_payment_method")
		payload["payment_method_id"] = req.SavedMethodID
	}
	data, err := p.api.do(ctx, http.MethodPost, "/payments", payload, map[string]string{
		"Idempotence-Key": req.OrderID,
	})
	if err != nil {
		return Link{}, err
	}
	confirmation, _ := data["confirmation"].(map[string]any)
	url := jsonString(confirmation["confirmation_url"])
	if url == "" && req.SavedMethodID == "" {
		return Link{}, fmt.Errorf("ЮKassa не вернула ссылку на оплату")
	}
	expires := parseISOTime(data["expires_at"])
	if expires == nil {
		fallback := time.Now().UTC().Add(req.ExpiresIn)
		expires = &fallback
	}
	return Link{URL: url, ID: jsonString(data["id"]), ExpiresAt: expires}, nil
}

func (p *YooKassaProvider) Verify(ctx context.Context, body []byte, _ http.Header) (Event, error) {
	var raw map[string]any
	if err := decodeJSON(body, &raw); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrVerification, err)
	}
	if jsonString(raw["type"]) != "notification" {
		return Event{}, fmt.Errorf("%w: это не уведомление ЮKassa", ErrVerification)
	}
	object, _ := raw["object"].(map[string]any)
	paymentID := jsonString(object["id"])
	if paymentID == "" {
		return Event{}, fmt.Errorf("%w: в уведомлении нет идентификатора платежа", ErrVerification)
	}
	event := jsonString(raw["event"])
	if !strings.HasPrefix(event, "payment.") {
		return Event{}, ErrNoEvent
	}
	payment, err := p.api.do(ctx, http.MethodGet, "/payments/"+paymentID, nil, nil)
	if err != nil {
		return Event{}, err
	}
	if jsonString(payment["id"]) != paymentID {
		return Event{}, fmt.Errorf("%w: API вернул другой платёж", ErrVerification)
	}
	var status Status
	switch jsonString(payment["status"]) {
	case "succeeded":
		status = StatusPaid
	case "canceled":
		status = StatusFailed
	default:
		// pending и waiting_for_capture — «ещё ничего не решено».
		return Event{}, ErrNoEvent
	}
	money, _ := payment["amount"].(map[string]any)
	metadata, _ := payment["metadata"].(map[string]any)
	out := Event{
		OrderID:       jsonString(metadata["order_id"]),
		Status:        status,
		TransactionID: jsonString(payment["id"]),
		Currency:      jsonString(money["currency"]),
	}
	if status == StatusPaid {
		out.PaidAt = parseISOTime(payment["captured_at"])
		if out.PaidAt == nil {
			now := time.Now().UTC()
			out.PaidAt = &now
		}
	}
	if cents := moneyFromMajor(money["value"]); cents != nil {
		amount := domain.Money(*cents)
		out.Amount = &amount
	}
	if out.OrderID == "" {
		return Event{}, ErrNoEvent
	}
	if out.Amount == nil || out.Currency == "" {
		return Event{}, fmt.Errorf("%w: нет корректной суммы или валюты платежа", ErrVerification)
	}
	method, _ := payment["payment_method"].(map[string]any)
	if saved, _ := method["saved"].(bool); saved {
		out.SavedMethodID = jsonString(method["id"])
	}
	return out, nil
}
