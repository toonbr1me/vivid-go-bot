package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/payments"
)

// paymentWebhook принимает уведомление шлюза. Ответ «200 OK» означает
// «событие принято»; ошибка проверки подписи — 400, временный сбой базы —
// 500, чтобы шлюз повторил доставку.
func (s *Server) paymentWebhook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	registry := s.pay.Registry()
	if registry == nil {
		http.Error(w, "платежи не настроены", http.StatusNotFound)
		return
	}
	provider, ok := registry.Get(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "не удалось прочитать уведомление", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	event, err := provider.Verify(ctx, body, r.Header)
	if errors.Is(err, payments.ErrNoEvent) {
		_, _ = w.Write([]byte("OK"))
		return
	}
	if err != nil {
		s.log.Warn("вебхук: проверка не прошла", "provider", name, "err", err)
		status := http.StatusServiceUnavailable
		if errors.Is(err, payments.ErrVerification) {
			status = http.StatusBadRequest
		}
		http.Error(w, "уведомление не прошло проверку", status)
		return
	}
	changed, err := s.pay.ReceiveEvent(ctx, name, event)
	if err != nil {
		s.log.Error("вебхук: обработка", "provider", name, "order", event.OrderID, "err", err)
		http.Error(w, "не удалось обработать уведомление", http.StatusInternalServerError)
		return
	}
	if changed {
		s.log.Info("вебхук: заказ обновлён", "provider", name, "order", event.OrderID, "status", event.Status)
	}
	_, _ = w.Write([]byte("OK"))
}
