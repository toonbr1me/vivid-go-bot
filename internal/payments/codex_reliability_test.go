package payments

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

func TestReliabilityJournalConcurrentCredit(t *testing.T) {
	_, db, _ := testProvisionService(t)
	ctx := context.Background()
	order := paidOrder(t, db, 44001, domain.FromRubles(100))
	subject, err := db.EnsureTelegramSubject(ctx, 44001)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := db.Journal(order.OrderID).Once(ctx, "concurrent_credit", func(ctx context.Context, tx storage.Tx) (any, error) {
				balance, err := tx.AddBalance(ctx, subject.ID, domain.FromRubles(10))
				time.Sleep(5 * time.Millisecond)
				return balance, err
			})
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if balance, err := db.Balance(ctx, subject.ID); err != nil || balance != domain.FromRubles(10) {
		t.Fatalf("повторное начисление: balance=%v err=%v", balance, err)
	}
}

func TestReliabilityConcurrentOrdersAddPeriods(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true}); err != nil {
		t.Fatal(err)
	}
	first := paidOrder(t, db, 44002, domain.FromRubles(100))
	second := first
	second.OrderID += "-second"
	second, err := db.CreateOrder(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	failures := make(chan error, 2)
	for _, order := range []domain.Order{first, second} {
		go func() { _, err := svc.Provision(ctx, order); failures <- err }()
	}
	for i := 0; i < 2; i++ {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	if remaining := panel.lastExpiry().Sub(start); remaining < 60*24*time.Hour || remaining > 60*24*time.Hour+time.Minute {
		t.Fatalf("два заказа должны дать 60 дней, получили %v", remaining)
	}
	before := panel.lastExpiry()
	if _, err := svc.Provision(ctx, first); err != nil {
		t.Fatal(err)
	}
	if !panel.lastExpiry().Equal(before) {
		t.Fatal("повтор старого заказа изменил срок")
	}
}

func TestReliabilityBalanceRollbackOnOrderConflict(t *testing.T) {
	_, db, _ := testProvisionService(t)
	ctx := context.Background()
	order := paidOrder(t, db, 44003, domain.FromRubles(100))
	subject, err := db.EnsureTelegramSubject(ctx, 44003)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddBalance(ctx, subject.ID, domain.FromRubles(100), nil); err != nil {
		t.Fatal(err)
	}
	order.Provider = "balance"
	if _, err := db.CreateBalanceOrder(ctx, subject.ID, order); err == nil {
		t.Fatal("ожидался конфликт номера заказа")
	}
	if balance, err := db.Balance(ctx, subject.ID); err != nil || balance != domain.FromRubles(100) {
		t.Fatalf("списание не откатилось: %v %v", balance, err)
	}
	order.OrderID += "-too-much"
	order.Amount = domain.FromRubles(101)
	if _, err := db.CreateBalanceOrder(ctx, subject.ID, order); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("не отвергнута неполная оплата: %v", err)
	}
	if _, err := db.OrderByID(ctx, order.OrderID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("сохранился неоплаченный заказ: %v", err)
	}
}

type unavailableGrantPanel struct{ Panel }

func (p unavailableGrantPanel) CreateUser(context.Context, remnawave.CreateUserParams) (remnawave.User, error) {
	return remnawave.User{}, errors.New("panel offline")
}

func TestReliabilityTrialResumesReservedBonus(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 44004)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddPendingBonus(ctx, subject.ID, 7); err != nil {
		t.Fatal(err)
	}
	plan := domain.Plan{ID: "trial_bot", IsTrial: true, TrialKind: "bot", Days: 3}
	svc.panel = unavailableGrantPanel{Panel: panel}
	if err := svc.GrantTrial(ctx, subject, "tg_44004", plan); err == nil {
		t.Fatal("ожидался сбой панели")
	}
	svc.panel = panel
	start := time.Now()
	if err := svc.GrantTrial(ctx, subject, "tg_44004", plan); err != nil {
		t.Fatal(err)
	}
	if remaining := panel.lastExpiry().Sub(start); remaining < 10*24*time.Hour-time.Minute || remaining > 10*24*time.Hour+time.Minute {
		t.Fatalf("триал потерял или удвоил бонус: %v", remaining)
	}
	if err := svc.GrantTrial(ctx, subject, "tg_44004", plan); !errors.Is(err, ErrTrialUsed) {
		t.Fatalf("повторный триал: %v", err)
	}
}

func TestReliabilityGiftReservationSurvivesPanelFailure(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 44005)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreatePromo(ctx, domain.PromoCode{Code: "GIFT7", GrantDays: 7, MaxUses: 1, PerUserLimit: 1, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	svc.panel = unavailableGrantPanel{Panel: panel}
	if _, err := svc.RedeemGiftPromo(ctx, subject, "GIFT7"); err == nil {
		t.Fatal("ожидался сбой панели")
	}
	svc.panel = panel
	if days, err := svc.RedeemGiftPromo(ctx, subject, "GIFT7"); err != nil || days != 7 {
		t.Fatalf("повтор подарка: %d %v", days, err)
	}
	before := panel.lastExpiry()
	if _, err := svc.RedeemGiftPromo(ctx, subject, "GIFT7"); !errors.Is(err, ErrPromoRejected) {
		t.Fatalf("код выдан повторно: %v", err)
	}
	if !panel.lastExpiry().Equal(before) {
		t.Fatal("повторное применение продлило доступ")
	}
}
