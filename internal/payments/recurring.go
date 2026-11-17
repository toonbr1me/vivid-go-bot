package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// Merchant capabilities are opt-in: the provider must have enabled recurring
// for this merchant. A supported API does not imply account eligibility.
func (s *Service) RecurringSupported(o domain.PaymentOption, days int) bool {
	if s.cfg == nil || !s.cfg.Current().RecurringEnabled || days <= 0 {
		return false
	}
	enabled := false
	for _, p := range s.cfg.Current().RecurringProviders {
		if p == o.Provider {
			enabled = true
		}
	}
	if !enabled {
		return false
	}
	switch o.Provider {
	case "yookassa":
		return o.Method == "card"
	case "platega":
		return o.Method == "sbp" && days <= 730
	}
	return false
}

func (s *Service) RecurringForSubject(ctx context.Context, id int64) (domain.RecurringAgreement, error) {
	return s.db.RecurringForSubject(ctx, id)
}

func (s *Service) CancelRecurring(ctx context.Context, subjectID int64) error {
	a, err := s.db.RecurringForSubject(ctx, subjectID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	unlock, err := s.db.LockRecurring(ctx, a.ID)
	if err != nil {
		return err
	}
	defer unlock()
	// Another request may have cancelled while this one waited for the lock.
	a, err = s.db.RecurringByID(ctx, a.ID)
	if err != nil {
		return err
	}
	if !a.Enabled {
		return nil
	}
	if a.Provider == "platega" {
		if a.ExternalID == "" {
			return errors.New("подписка ещё создаётся; повторите отмену после получения её номера")
		}
		p, ok := s.registry.Get(a.Provider)
		if !ok {
			return ErrNoProviders
		}
		canceller, ok := p.(interface {
			CancelRecurring(context.Context, string) error
		})
		if !ok {
			return ErrNoProviders
		}
		if err = canceller.CancelRecurring(ctx, a.ExternalID); err != nil {
			return err
		}
	}
	return s.db.StopRecurring(ctx, a.ID, "user")
}

// Platega assigns a new transaction ID to every scheduled debit. Convert each
// verified charge into its own immutable order; replaying a callback reuses it.
func (s *Service) recurringEventOrder(ctx context.Context, provider string, e Event) (Event, error) {
	if e.SubscriptionID == "" {
		return e, nil
	}
	if provider != "platega" || e.TransactionID == "" || e.Amount == nil || e.Currency == "" {
		return e, ErrVerification
	}
	a, err := s.db.RecurringByExternal(ctx, provider, e.SubscriptionID)
	if errors.Is(err, storage.ErrNotFound) && e.OrderID != "" {
		a, err = s.db.RecurringByID(ctx, e.OrderID)
		if err == nil && a.Provider == provider && (a.ExternalID == "" || a.ExternalID == e.SubscriptionID) {
			err = s.db.SetRecurringExternal(ctx, a.ID, e.SubscriptionID, false)
		} else if err == nil {
			err = ErrVerification
		}
	}
	if err != nil {
		return e, err
	}
	if a.Provider != provider || a.Amount != *e.Amount || a.Currency != e.Currency {
		return e, ErrVerification
	}
	unlock, err := s.db.LockRecurring(ctx, a.ID)
	if err != nil {
		return e, err
	}
	defer unlock()
	// Even after cancellation, an already completed charge must receive access.
	digest := sha256.Sum256([]byte(provider + ":" + e.TransactionID))
	id := "rec-" + hex.EncodeToString(digest[:16])
	if linked, err := s.db.RecurringChargeOrder(ctx, provider, e.TransactionID); err == nil {
		order, err := s.db.OrderByID(ctx, linked)
		if err != nil {
			return e, err
		}
		if order.RecurringAgreementID == nil || *order.RecurringAgreementID != a.ID {
			return e, ErrVerification
		}
		if e.Status == StatusPaid {
			if err = s.db.ConfirmRecurringCharge(ctx, a.ID, e.SubscriptionID, e.NextChargeAt); err != nil {
				return e, err
			}
		}
		e.OrderID = linked
		if e.Status == StatusFailed {
			if err = s.db.NoteRecurringDecline(ctx, a.ID, linked); err != nil {
				return e, err
			}
		}
		return e, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return e, err
	}
	initial, err := s.db.OrderByID(ctx, a.ID)
	if err != nil {
		return e, err
	}
	reserved, err := s.db.RecurringOrderReserved(ctx, a.ID)
	if err != nil {
		return e, err
	}
	if initial.TransactionID == nil && initial.Status == domain.OrderPending && !reserved {
		id = a.ID
	} else {
		// The order may already exist after a crash between insert and linking.
		_, err = s.db.OrderByID(ctx, id)
		if errors.Is(err, storage.ErrNotFound) {
			_, err = s.db.CreateOrder(ctx, domain.Order{OrderID: id, Provider: provider, TelegramID: initial.TelegramID, AccountID: initial.AccountID, PlanID: a.PlanID, Kind: domain.KindSubscription, Amount: a.Amount, OriginalAmount: a.Amount, Currency: a.Currency, PaymentMethod: &a.Method, Status: domain.OrderPending, PlanDays: &a.Days, DeviceCount: a.DeviceCount, RecurringAgreementID: &a.ID, PlanProfile: initial.PlanProfile})
		}
		if err != nil {
			return e, err
		}
	}
	e.OrderID = id
	if e.Status == StatusPaid {
		if err = s.db.ConfirmRecurringCharge(ctx, a.ID, e.SubscriptionID, e.NextChargeAt); err != nil {
			return e, err
		}
	}
	if err = s.db.LinkRecurringCharge(ctx, provider, e.TransactionID, id); err != nil {
		return e, err
	}
	if e.Status == StatusFailed {
		if err = s.db.NoteRecurringDecline(ctx, a.ID, id); err != nil {
			return e, err
		}
	}
	return e, nil
}

// YooKassa debits are scheduled here; Platega owns its own schedule. A durable
// pending order and stable idempotence key survive process/API timeouts.
func (s *Service) ProcessRecurring(ctx context.Context, limit int) error {
	if s.cfg == nil || !s.cfg.Current().RecurringEnabled {
		return nil
	}
	agreements, err := s.db.ActiveRecurring(ctx, limit)
	if err != nil {
		return err
	}
	for _, a := range agreements {
		if err = s.processRecurring(ctx, a.ID); err != nil {
			_ = s.db.RecurringError(ctx, a.ID, err.Error())
		}
	}
	return nil
}
func (s *Service) processRecurring(ctx context.Context, id string) error {
	defer s.db.TouchRecurring(ctx, id)
	unlock, err := s.db.LockRecurring(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	a, err := s.db.RecurringByID(ctx, id)
	if err != nil || !a.Enabled {
		return err
	}
	if !s.RecurringSupported(domain.PaymentOption{Provider: a.Provider, Method: a.Method}, a.Days) {
		return nil
	}
	provider, ok := s.registry.Get(a.Provider)
	if !ok {
		return ErrNoProviders
	}
	initial, err := s.db.OrderByID(ctx, a.ID)
	if err != nil {
		return err
	}
	if a.PendingOrderID != nil {
		order, err := s.db.OrderByID(ctx, *a.PendingOrderID)
		if err != nil {
			return err
		}
		if order.Status == domain.OrderFailed || order.Status == domain.OrderCancelled {
			_ = s.db.StopRecurring(ctx, a.ID, "declined")
			return errors.New("автопродление остановлено: платёж отклонён")
		}
		if order.ProvisionedAt != nil && order.ExpireAt != nil && a.ExternalID != "" {
			return s.db.SetRecurringSchedule(ctx, a.ID, order.ExpireAt, nil)
		}
		// Initial checkout is completed by the payer, never by this scheduler.
		if order.OrderID == a.ID {
			return nil
		}
		return s.sendRecurringDebit(ctx, provider, a, order)
	}
	if !a.Activated || a.ExternalID == "" || a.NextChargeAt == nil || a.NextChargeAt.After(time.Now()) {
		return nil
	}
	// Pause if the plan disappeared or its terms changed. Never silently raise
	// an authorised amount or replace the period of an existing agreement.
	plan, err := s.Plan(ctx, a.PlanID)
	if err != nil || !plan.IsActive || plan.Days != a.Days || plan.Price != initial.OriginalAmount {
		_ = s.db.StopRecurring(ctx, a.ID, "terms")
		return errors.New("условия тарифа изменились: требуется новое согласие")
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", a.ID, a.NextChargeAt.Unix())))
	orderID := "renew-" + hex.EncodeToString(digest[:16])
	order, err := s.db.OrderByID(ctx, orderID)
	if errors.Is(err, storage.ErrNotFound) {
		order, err = s.db.CreateOrder(ctx, domain.Order{OrderID: orderID, Provider: a.Provider, TelegramID: initial.TelegramID, AccountID: initial.AccountID, PlanID: a.PlanID, Kind: domain.KindSubscription, Amount: a.Amount, OriginalAmount: a.Amount, Currency: a.Currency, PaymentMethod: &a.Method, Status: domain.OrderPending, PlanDays: &a.Days, DeviceCount: a.DeviceCount, RecurringAgreementID: &a.ID, PlanProfile: initial.PlanProfile})
	}
	if err != nil {
		return err
	}
	if err = s.db.SetRecurringSchedule(ctx, a.ID, a.NextChargeAt, &orderID); err != nil {
		return err
	}
	return s.sendRecurringDebit(ctx, provider, a, order)
}
func (s *Service) sendRecurringDebit(ctx context.Context, p Provider, a domain.RecurringAgreement, order domain.Order) error {
	if order.PaymentLinkID != nil {
		return nil
	} // reconciliation resolves its status
	// YooKassa retains idempotence keys for 24h. Do not risk a second debit
	// after that window if the first API response was lost.
	if time.Since(order.CreatedAt) > 23*time.Hour {
		_ = s.db.DisableRecurring(ctx, a.ID)
		return errors.New("неизвестен результат автоплатежа; нужна ручная сверка")
	}
	link, err := p.CreateLink(ctx, CreateRequest{OrderID: order.OrderID, Amount: a.Amount, Currency: a.Currency, Description: "Автопродление подписки", SavedMethodID: a.ExternalID, ExpiresIn: time.Hour})
	if err != nil {
		return err
	}
	if link.ID == "" {
		return errors.New("нет идентификатора автоплатежа")
	}
	return s.db.SetPaymentLink(ctx, order.OrderID, "", link.ID, link.ExpiresAt)
}
