package payments

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type safetyGateway struct {
	mu        sync.Mutex
	name      string
	requests  []CreateRequest
	fail      bool
	cancelErr error
	cancelled []string
	before    func(CreateRequest) error
}

func (p *safetyGateway) Name() string       { return p.name }
func (p *safetyGateway) Methods() []string  { return []string{"card", "sbp"} }
func (p *safetyGateway) TelegramOnly() bool { return false }
func (p *safetyGateway) Verify(context.Context, []byte, http.Header) (Event, error) {
	return Event{}, ErrNoEvent
}
func (p *safetyGateway) CreateLink(_ context.Context, r CreateRequest) (Link, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, r)
	if p.before != nil {
		if err := p.before(r); err != nil {
			return Link{}, err
		}
	}
	if p.fail {
		return Link{}, errors.New("simulated provider timeout")
	}
	return Link{URL: "https://pay.example/" + r.OrderID, ID: "gateway-" + r.OrderID}, nil
}
func (p *safetyGateway) CancelRecurring(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, id)
	return p.cancelErr
}
func (p *safetyGateway) calls() []CreateRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]CreateRequest(nil), p.requests...)
}

func recurringSafetyService(t *testing.T, provider string) (*Service, *storage.DB, *safetyGateway, Purchase) {
	t.Helper()
	s, db, _ := testProvisionService(t)
	ctx := context.Background()
	s.cfg.RecurringEnabled = true
	s.cfg.RecurringProviders = []string{"yookassa", "platega"}
	s.cfg.LinkTTLMinutes = 30
	if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, Devices: 3, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	p := &safetyGateway{name: provider}
	s.registry.add(p)
	method := "card"
	if provider == "platega" {
		method = "sbp"
	}
	return s, db, p, Purchase{TelegramID: ptr(int64(91001)), PlanID: "30d", Option: provider + "." + method, Recurring: true}
}

func dueRecurring(t *testing.T) (*Service, *storage.DB, *safetyGateway, domain.RecurringAgreement) {
	t.Helper()
	s, db, p, purchase := recurringSafetyService(t, "yookassa")
	ctx := context.Background()
	checkout, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Minute)
	if err = db.MarkProvisioned(ctx, checkout.OrderID, &due, ptr(30)); err != nil {
		t.Fatal(err)
	}
	if err = db.SetRecurringExternal(ctx, checkout.OrderID, "saved-method", true); err != nil {
		t.Fatal(err)
	}
	if err = db.SetRecurringSchedule(ctx, checkout.OrderID, &due, nil); err != nil {
		t.Fatal(err)
	}
	a, err := db.RecurringByID(ctx, checkout.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	return s, db, p, a
}

func TestRecurringCheckoutStoresConsentBeforeProviderAndReusesOnlySameMode(t *testing.T) {
	s, db, p, purchase := recurringSafetyService(t, "yookassa")
	ctx := context.Background()
	purchase.Recurring = false
	single, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	purchase.Recurring = true
	p.before = func(r CreateRequest) error {
		if !r.Recurring {
			return nil
		}
		a, err := db.RecurringByID(ctx, r.OrderID)
		if err != nil {
			return err
		}
		if a.Amount != r.Amount || a.Days != r.PlanDays {
			return errors.New("consent terms differ from payment")
		}
		return nil
	}
	recurring, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	if single.OrderID == recurring.OrderID || !repeated.Reused || repeated.OrderID != recurring.OrderID || len(p.calls()) != 2 {
		t.Fatal("one-time and recurring invoices were confused or duplicated")
	}
}

func TestRecurringCannotBypassMerchantAndMethodRestrictions(t *testing.T) {
	for _, tc := range []string{"env_disabled", "merchant_disabled", "unsupported_method", "wrong_provider", "trial", "free_purchase"} {
		t.Run(tc, func(t *testing.T) {
			s, db, p, purchase := recurringSafetyService(t, "yookassa")
			ctx := context.Background()
			switch tc {
			case "env_disabled":
				s.cfg.RecurringEnabled = false
			case "merchant_disabled":
				s.cfg.RecurringProviders = nil
			case "unsupported_method":
				purchase.Option = "yookassa.sbp"
			case "wrong_provider":
				purchase.Option = "wata.card"
			case "trial":
				s.cfg.TrialBotDays = 3
				purchase.PlanID = "trial_bot"
			case "free_purchase":
				s.cfg.MinPaymentAmount = 0
				purchase.PromoCode = "FREE"
				if err := db.CreatePromo(ctx, domain.PromoCode{Code: "FREE", DiscountPercent: 100, IsActive: true}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Checkout(ctx, purchase); err == nil {
				t.Fatal("invalid recurring checkout accepted")
			}
			if len(p.calls()) != 0 {
				t.Fatal("provider called without usable consent")
			}
		})
	}
}

func TestRecurringSchedulerNeverChargesBeforeConsentOrDueDate(t *testing.T) {
	for _, tc := range []string{"future", "unactivated", "no_saved_method", "disabled", "env_disabled", "merchant_disabled"} {
		t.Run(tc, func(t *testing.T) {
			s, db, p, a := dueRecurring(t)
			ctx := context.Background()
			switch tc {
			case "future":
				future := time.Now().Add(time.Hour)
				_ = db.SetRecurringSchedule(ctx, a.ID, &future, nil)
			case "unactivated":
				_, err := db.Pool.Exec(ctx, `UPDATE recurring_agreements SET activated=FALSE WHERE id=$1`, a.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "no_saved_method":
				_ = db.SetRecurringExternal(ctx, a.ID, "", false)
			case "disabled":
				_ = db.DisableRecurring(ctx, a.ID)
			case "env_disabled":
				s.cfg.RecurringEnabled = false
			case "merchant_disabled":
				s.cfg.RecurringProviders = nil
			}
			if err := s.ProcessRecurring(ctx, 20); err != nil {
				t.Fatal(err)
			}
			if len(p.calls()) != 1 {
				t.Fatal("unexpected automatic debit")
			}
		})
	}
}

func TestRecurringChangedTermsRequireNewConsent(t *testing.T) {
	for _, tc := range []string{"price", "period", "removed"} {
		t.Run(tc, func(t *testing.T) {
			s, db, p, a := dueRecurring(t)
			ctx := context.Background()
			plan := domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true}
			switch tc {
			case "price":
				plan.Price = 20000
			case "period":
				plan.Days = 60
			case "removed":
				plan.IsActive = false
			}
			if err := db.SavePlan(ctx, plan); err != nil {
				t.Fatal(err)
			}
			_ = s.ProcessRecurring(ctx, 20)
			stored, err := db.RecurringByID(ctx, a.ID)
			if err != nil || stored.Enabled || stored.LastError == "" || len(p.calls()) != 1 {
				t.Fatal("changed terms were silently charged")
			}
		})
	}
}

func TestRecurringRetryKeepsSameOrderAndStopsOutsideIdempotencyWindow(t *testing.T) {
	s, db, p, a := dueRecurring(t)
	ctx := context.Background()
	p.fail = true
	_ = s.ProcessRecurring(ctx, 20)
	_ = s.ProcessRecurring(ctx, 20)
	calls := p.calls()
	if len(calls) != 3 || calls[1].OrderID != calls[2].OrderID || calls[1].SavedMethodID != "saved-method" {
		t.Fatal("retry changed the debit identity")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE orders SET created_at=now()-interval '24 hours' WHERE order_id=$1`, calls[1].OrderID); err != nil {
		t.Fatal(err)
	}
	_ = s.ProcessRecurring(ctx, 20)
	stored, err := db.RecurringByID(ctx, a.ID)
	if err != nil || stored.Enabled || len(p.calls()) != 3 {
		t.Fatal("an ambiguous old debit was retried")
	}
}

func TestConcurrentRecurringWorkersCreateOneDebit(t *testing.T) {
	s, db, p, _ := dueRecurring(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := New(db, s.panel, s.registry, s.cfg)
			errs <- worker.ProcessRecurring(ctx, 20)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(p.calls()) != 2 {
		t.Fatalf("parallel workers duplicated debit: %d", len(p.calls())-1)
	}
}

func TestPlategaCancellationMustBeConfirmedRemotely(t *testing.T) {
	s, db, p, purchase := recurringSafetyService(t, "platega")
	ctx := context.Background()
	checkout, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.RecurringByID(ctx, checkout.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	p.cancelErr = errors.New("provider unavailable")
	if err = s.CancelRecurring(ctx, a.SubjectID); err == nil {
		t.Fatal("remote cancellation failure hidden")
	}
	a, _ = db.RecurringByID(ctx, a.ID)
	if !a.Enabled {
		t.Fatal("local UI claimed cancellation despite provider failure")
	}
	p.cancelErr = nil
	if err = s.CancelRecurring(ctx, a.SubjectID); err != nil {
		t.Fatal(err)
	}
	if err = s.CancelRecurring(ctx, a.SubjectID); err != nil {
		t.Fatal(err)
	}
	a, _ = db.RecurringByID(ctx, a.ID)
	if a.Enabled || len(p.cancelled) != 2 {
		t.Fatal("cancellation not persisted or was sent again")
	}
}

func TestRecurringCancellationCannotCancelAnotherSubject(t *testing.T) {
	s, db, p, a := dueRecurring(t)
	ctx := context.Background()
	other, err := db.EnsureTelegramSubject(ctx, 91002)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CancelRecurring(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	a, _ = db.RecurringByID(ctx, a.ID)
	if !a.Enabled || len(p.cancelled) != 0 {
		t.Fatal("another account cancelled an agreement")
	}
}

func TestConcurrentPlategaCancellationSendsOneRemoteRequest(t *testing.T) {
	s, db, p, purchase := recurringSafetyService(t, "platega")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	checkout, err := s.Checkout(ctx, purchase)
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.RecurringByID(ctx, checkout.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetRecurringExternal(ctx, a.ID, "subscription-to-cancel", true); err != nil {
		t.Fatal(err)
	}
	unlock, err := db.LockRecurring(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- s.CancelRecurring(ctx, a.SubjectID) }()
	}
	// Both requests must read the enabled agreement and wait on the same lock.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted
 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND classid=((hashtextextended($1,0)>>32)&4294967295)::oid
 AND objid=(hashtextextended($1,0)&4294967295)::oid`, "recurring:"+a.ID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("cancellation requests did not reach the lock")
		case <-ticker.C:
		}
	}
	unlock()
	for range 2 {
		if err = <-errs; err != nil {
			t.Fatal(err)
		}
	}
	p.mu.Lock()
	calls := len(p.cancelled)
	p.mu.Unlock()
	if calls != 1 {
		t.Fatalf("remote cancellation sent %d times", calls)
	}
	a, err = db.RecurringByID(ctx, a.ID)
	if err != nil || a.Enabled || a.StopReason != "user" {
		t.Fatal("concurrent cancellation was not persisted")
	}
}
