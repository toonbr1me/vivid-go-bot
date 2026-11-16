package payments

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWataVerifiesSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("ключ: %v", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"value": string(publicPEM)})
	}))
	defer server.Close()

	provider := NewWataProvider(server.URL, "token", server.URL+"/key", nil, nil, 5*time.Second)

	body := []byte(`{"transactionStatus":"Paid","kind":"Payment","orderId":"tg-1-abc","amount":100.00,"currency":"RUB","transactionId":"t-1"}`)
	digest := sha512.Sum512(body)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA512, digest[:])
	if err != nil {
		t.Fatalf("подпись: %v", err)
	}
	headers := http.Header{}
	headers.Set("X-Signature", base64.StdEncoding.EncodeToString(signature))

	event, err := provider.Verify(context.Background(), body, headers)
	if err != nil {
		t.Fatalf("проверка подписи: %v", err)
	}
	if event.OrderID != "tg-1-abc" {
		t.Errorf("заказ = %q, ждали tg-1-abc", event.OrderID)
	}
	if event.Status != StatusPaid {
		t.Errorf("статус = %v, ждали «оплачен»", event.Status)
	}
	if event.Amount == nil || *event.Amount != 10000 {
		t.Errorf("сумма = %v, ждали 10000", event.Amount)
	}
}

func TestWataRejectsForeignSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("ключ: %v", err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("чужой ключ: %v", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"value": string(publicPEM)})
	}))
	defer server.Close()

	provider := NewWataProvider(server.URL, "token", server.URL+"/key", nil, nil, 5*time.Second)

	body := []byte(`{"transactionStatus":"Paid","kind":"Payment","orderId":"tg-1-abc"}`)
	digest := sha512.Sum512(body)
	signature, err := rsa.SignPKCS1v15(rand.Reader, other, crypto.SHA512, digest[:])
	if err != nil {
		t.Fatalf("подпись: %v", err)
	}
	headers := http.Header{}
	headers.Set("X-Signature", base64.StdEncoding.EncodeToString(signature))

	if _, err := provider.Verify(context.Background(), body, headers); err == nil {
		t.Fatal("чужая подпись не должна приниматься")
	}
}

func TestPlategaChecksCredentials(t *testing.T) {
	provider := NewPlategaProvider("https://api.example", "merchant", "secret", nil, time.Second)

	headers := http.Header{}
	headers.Set("X-MerchantId", "merchant")
	headers.Set("X-Secret", "wrong")
	body := []byte(`{"status":"CONFIRMED","payload":"order-1","id":"tx-1","amount":100,"currency":"RUB"}`)
	if _, err := provider.Verify(context.Background(), body, headers); err == nil {
		t.Fatal("чужие учётные данные не должны приниматься")
	}

	headers.Set("X-Secret", "secret")
	event, err := provider.Verify(context.Background(), body, headers)
	if err != nil {
		t.Fatalf("проверка: %v", err)
	}
	if event.OrderID != "order-1" || event.Status != StatusPaid {
		t.Errorf("событие = %+v, ждали заказ order-1 со статусом «оплачен»", event)
	}
	if event.Amount == nil || *event.Amount != 10000 {
		t.Errorf("сумма = %v, ждали 10000", event.Amount)
	}
}

func TestPlategaWithoutHeadersFails(t *testing.T) {
	provider := NewPlategaProvider("https://api.example", "merchant", "secret", nil, time.Second)
	if _, err := provider.Verify(context.Background(), []byte(`{}`), http.Header{}); err == nil {
		t.Fatal("без заголовков проверка не должна проходить")
	}
}
