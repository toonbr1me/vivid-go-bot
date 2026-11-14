package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
)

// ReceiveEvent подтверждает только сохранённое проверенное событие.
// Выдача может повторяться независимо от повторов вебхука провайдером.
func (s *Service) ReceiveEvent(ctx context.Context, provider string, event Event) (bool, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	canonical := event
	canonical.PaidAt = nil // шлюзы без даты подставляют время каждого получения
	identity, err := json.Marshal(canonical)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(append([]byte(provider+":"), identity...))
	key := hex.EncodeToString(digest[:])
	if err := s.db.StorePaymentEvent(ctx, key, provider, payload); err != nil {
		return false, err
	}
	changed, processErr := s.applyInboxEvent(ctx, provider, event)
	if processErr != nil {
		slog.Warn("платёж сохранён, обработка отложена", "order", event.OrderID, "err", processErr)
	}
	if err := s.db.FinishPaymentEvent(ctx, key, processErr); err != nil {
		slog.Error("не удалось обновить входящее платёжное событие", "order", event.OrderID, "err", err)
	}
	return changed, nil
}

func (s *Service) ProcessPaymentInbox(ctx context.Context, limit int) error {
	items, err := s.db.PendingPaymentEvents(ctx, limit)
	if err != nil {
		return err
	}
	for _, item := range items {
		var event Event
		processErr := json.Unmarshal(item.Payload, &event)
		if processErr == nil {
			_, processErr = s.applyInboxEvent(ctx, item.Provider, event)
		}
		if err := s.db.FinishPaymentEvent(ctx, item.Key, processErr); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyInboxEvent(ctx context.Context, provider string, event Event) (bool, error) {
	var err error
	event, err = s.recurringEventOrder(ctx, provider, event)
	if err != nil {
		return false, err
	}
	// Старый HandleEvent допускает уведомления о неизвестных заказах;
	// в надёжном inbox они остаются до появления соответствующего заказа.
	if _, err := s.db.OrderByID(ctx, event.OrderID); err != nil {
		return false, err
	}
	return s.HandleEvent(ctx, provider, event)
}
