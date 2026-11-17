package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

func verifyPanelSignature(body []byte, signature, secret string) bool {
	if secret == "" {
		return false
	}
	got, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func panelEvent(body []byte) (storage.PanelEvent, error) {
	var payload struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return storage.PanelEvent{}, err
	}
	digest := sha256.Sum256(body)
	e := storage.PanelEvent{ID: hex.EncodeToString(digest[:]), Event: payload.Event}
	var data map[string]json.RawMessage
	if json.Unmarshal(payload.Data, &data) != nil {
		return e, nil
	}
	user, device := data, data
	var nested map[string]json.RawMessage
	if json.Unmarshal(data["user"], &nested) == nil && nested != nil {
		user = nested
	}
	nested = nil
	if json.Unmarshal(data["device"], &nested) == nil && nested != nil {
		device = nested
	}
	raw := strings.TrimSpace(string(user["telegramId"]))
	if strings.HasPrefix(raw, `"`) {
		var s string
		if json.Unmarshal(user["telegramId"], &s) == nil {
			raw = strings.TrimSpace(s)
		}
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		e.TelegramID = id
	}
	parts := []string{}
	for _, key := range []string{"deviceModel", "platform", "osVersion"} {
		var value string
		if json.Unmarshal(device[key], &value) == nil {
			value = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return ' '
				}
				return r
			}, value)
			if value = strings.TrimSpace(value); value != "" {
				parts = append(parts, value)
			}
		}
	}
	label := []rune(strings.Join(parts, " · "))
	if len(label) > 200 {
		label = label[:200]
	}
	e.DeviceLabel = string(label)
	return e, nil
}

func (s *Server) panelWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.cfg.Current().RemnawaveWebhookSecret
	if secret == "" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid or oversized body", http.StatusRequestEntityTooLarge)
		return
	}
	if !verifyPanelSignature(body, r.Header.Get("X-Remnawave-Signature"), secret) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	e, err := panelEvent(body)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if e.Event == "user.first_connected" || e.Event == "user_hwid_devices.added" {
		if err = s.db.RecordPanelEvent(r.Context(), e); err != nil {
			s.log.Error("panel event persistence failed")
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
