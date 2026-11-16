package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func signPanel(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}
func TestPanelSignatureUsesRawBody(t *testing.T) {
	body := []byte(`{ "event": "user.first_connected", "data": {"telegramId":501} }`)
	sig := signPanel(string(body), "test-secret")
	if !verifyPanelSignature(body, " "+strings.ToUpper(sig)+" ", "test-secret") {
		t.Fatal("valid signature rejected")
	}
	for _, s := range []string{"", sig[:20], sig + "ff", "bad"} {
		if verifyPanelSignature(body, s, "test-secret") {
			t.Fatal("invalid signature accepted")
		}
	}
	if verifyPanelSignature(append(body, ' '), sig, "test-secret") || verifyPanelSignature(body, sig, "") {
		t.Fatal("unsigned or modified body accepted")
	}
}
func TestPanelEventParsingIsDefensive(t *testing.T) {
	for _, tc := range []struct {
		body  string
		id    int64
		label string
	}{
		{`{"event":"user_hwid_devices.added","data":{"user":{"telegramId":"501"},"device":{"deviceModel":"Pixel","platform":"Android"}}}`, 501, "Pixel · Android"},
		{`{"data":{"telegramId":501,"platform":"iOS"}}`, 501, "iOS"},
		{`{"data":{"telegramId":true}}`, 0, ""}, {`{"data":{"telegramId":501.2}}`, 0, ""}, {`{"data":{"telegramId":-100}}`, 0, ""},
		{`{"data":{"telegramId":"9223372036854775808"}}`, 0, ""}, {`{"data":[]}`, 0, ""}, {`{"data":null}`, 0, ""},
	} {
		e, err := panelEvent([]byte(tc.body))
		if err != nil || e.TelegramID != tc.id || e.DeviceLabel != tc.label {
			t.Fatalf("%s: %+v %v", tc.body, e, err)
		}
	}
	if _, err := panelEvent([]byte(`{`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}
func TestPanelWebhookAuthenticationAndDurableReplay(t *testing.T) {
	server, db := testServer(t)
	ctx := context.Background()
	if _, err := db.EnsureTelegramSubject(ctx, 501); err != nil {
		t.Fatal(err)
	}
	body := `{"event":"user.first_connected","data":{"telegramId":501}}`
	send := func(body, sig string) int {
		r := httptest.NewRequest(http.MethodPost, "/webhooks/remnawave", strings.NewReader(body))
		r.Header.Set("X-Remnawave-Signature", sig)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := send(body, signPanel(body, "secret")); code != 404 {
		t.Fatal("disabled route", code)
	}
	server.cfg.RemnawaveWebhookSecret = "secret"
	if code := send(body, ""); code != 401 {
		t.Fatal("unsigned webhook", code)
	}
	if code := send(body+" ", signPanel(body, "secret")); code != 401 {
		t.Fatal("modified body", code)
	}
	if code := send("{", signPanel("{", "secret")); code != 400 {
		t.Fatal("broken json", code)
	}
	oversized := strings.Repeat("x", (1<<20)+1)
	if code := send(oversized, signPanel(oversized, "secret")); code != 413 {
		t.Fatal("oversized body", code)
	}
	for i := 0; i < 3; i++ {
		if code := send(body, signPanel(body, "secret")); code != 200 {
			t.Fatal("signed webhook rejected", code)
		}
	}
	var hits int
	if err := db.Pool.QueryRow(ctx, `SELECT hits FROM funnel_daily WHERE step='first_connected'`).Scan(&hits); err != nil || hits != 1 {
		t.Fatal("webhook replay", hits, err)
	}
	device := `{"event":"user_hwid_devices.added","data":{"user":{"telegramId":501},"device":{"platform":"Android"}}}`
	for i := 0; i < 2; i++ {
		if code := send(device, signPanel(device, "secret")); code != 200 {
			t.Fatal(code)
		}
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM panel_events WHERE status='pending'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("device event queue", count, err)
	}
	unknown := `{"event":"other"}`
	if code := send(unknown, signPanel(unknown, "secret")); code != 200 {
		t.Fatal("unknown signed event", code)
	}
	if _, err := db.Pool.Exec(ctx, `ALTER TABLE panel_events RENAME TO panel_events_unavailable`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `ALTER TABLE panel_events_unavailable RENAME TO panel_events`)
	})
	if code := send(device, signPanel(device, "secret")); code != 503 {
		t.Fatal("failed persistence acknowledged", code)
	}
}
