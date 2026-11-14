package payments

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Wata — ссылка на оплату с подписанными уведомлениями.
//
// Каждый способ оплаты может жить под своим токеном: у Wata это отдельные
// кабинеты, и общий токен открыл бы страницу не того эквайринга.
type WataProvider struct {
	api          *apiClient
	token        string
	methodTokens map[string]string
	methods      []string
	publicKeyURL string
	keyMu        sync.Mutex
	publicKeyPEM string
}

func NewWataProvider(baseURL, token, publicKeyURL string, methods []string, methodTokens map[string]string, timeout time.Duration) *WataProvider {
	return &WataProvider{
		api:          newAPIClient(baseURL, timeout),
		token:        token,
		methodTokens: methodTokens,
		methods:      methods,
		publicKeyURL: publicKeyURL,
	}
}

func (p *WataProvider) Name() string       { return "wata" }
func (p *WataProvider) Methods() []string  { return append([]string(nil), p.methods...) }
func (p *WataProvider) TelegramOnly() bool { return false }

func (p *WataProvider) CreateLink(ctx context.Context, req CreateRequest) (Link, error) {
	token := p.token
	if methodToken, ok := p.methodTokens[req.Method]; ok && methodToken != "" {
		token = methodToken
	}
	expires := time.Now().UTC().Add(req.ExpiresIn)
	payload := map[string]any{
		"type":               "OneTime",
		"amount":             float64(req.Amount) / 100,
		"currency":           req.Currency,
		"description":        req.Description,
		"orderId":            req.OrderID,
		"expirationDateTime": expires.Format(time.RFC3339),
	}
	if req.SuccessURL != "" {
		payload["successRedirectUrl"] = req.SuccessURL
	}
	if req.FailURL != "" {
		payload["failRedirectUrl"] = req.FailURL
	}
	data, err := p.api.do(ctx, http.MethodPost, "/links", payload, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if err != nil {
		return Link{}, err
	}
	url := jsonString(data["url"])
	if url == "" {
		return Link{}, fmt.Errorf("WATA не вернула ссылку на оплату")
	}
	expireAt := parseISOTime(data["expiresAt"])
	if expireAt == nil {
		expireAt = &expires
	}
	return Link{URL: url, ID: jsonString(data["id"]), ExpiresAt: expireAt}, nil
}

func (p *WataProvider) Verify(ctx context.Context, body []byte, headers http.Header) (Event, error) {
	signature := headerValue(headers, "X-Signature")
	if signature == "" {
		return Event{}, fmt.Errorf("%w: нет заголовка X-Signature", ErrVerification)
	}
	key, err := p.publicKey(ctx, false)
	if err != nil {
		return Event{}, err
	}
	if !verifyWataSignature(body, signature, key) {
		key, err = p.publicKey(ctx, true)
		if err != nil {
			return Event{}, err
		}
		if !verifyWataSignature(body, signature, key) {
			return Event{}, fmt.Errorf("%w: подпись не сошлась", ErrVerification)
		}
	}
	var raw map[string]any
	if err := decodeJSON(body, &raw); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrVerification, err)
	}
	if jsonString(raw["kind"]) != "Payment" {
		return Event{}, ErrNoEvent
	}
	var status Status
	if jsonString(raw["transactionStatus"]) == "Paid" {
		status = StatusPaid
	} else if jsonString(raw["transactionStatus"]) == "Declined" {
		status = StatusFailed
	} else {
		return Event{}, ErrNoEvent
	}
	event := Event{
		OrderID:       jsonString(raw["orderId"]),
		Status:        status,
		TransactionID: jsonString(raw["transactionId"]),
		Currency:      jsonString(raw["currency"]),
	}
	if status == StatusPaid {
		event.PaidAt = parseISOTime(raw["paymentTime"])
	}
	if cents := moneyFromMajor(raw["amount"]); cents != nil {
		amount := domain.Money(*cents)
		event.Amount = &amount
	}
	if event.OrderID == "" {
		return Event{}, ErrNoEvent
	}
	return event, nil
}

func (p *WataProvider) publicKey(ctx context.Context, refresh bool) (string, error) {
	p.keyMu.Lock()
	defer p.keyMu.Unlock()
	if p.publicKeyPEM != "" && !refresh {
		return p.publicKeyPEM, nil
	}
	data, err := p.api.do(ctx, http.MethodGet, p.publicKeyURL, nil, nil)
	if err != nil {
		return "", err
	}
	value := jsonString(data["value"])
	if value == "" {
		return "", fmt.Errorf("WATA не вернула публичный ключ")
	}
	p.publicKeyPEM = value
	return value, nil
}

func verifyWataSignature(body []byte, signature string, publicKeyPEM string) bool {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return false
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		// Некоторые кабинеты отдают ключ в PKCS#1, а не в PKIX.
		if pkcs1, pkcs1Err := x509.ParsePKCS1PublicKey(block.Bytes); pkcs1Err == nil {
			parsed = pkcs1
		} else if certificate, certErr := x509.ParseCertificate(block.Bytes); certErr == nil {
			parsed = certificate.PublicKey
		} else {
			return false
		}
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	digest := sha512.Sum512(body)
	return rsa.VerifyPKCS1v15(key, crypto.SHA512, digest[:], raw) == nil
}
