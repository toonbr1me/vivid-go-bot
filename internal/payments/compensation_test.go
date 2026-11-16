package payments

import (
	"context"
	"errors"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompensationExtendsOnceAndRejectsUnauthorizedActors(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	svc.cfg.SupportCompensationDailyDays = 3
	if err := db.AddStaff(ctx, 100, storage.RoleSupport, "Support", 1); err != nil {
		t.Fatal(err)
	}
	s, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Issue", "Help", "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompensateTicket(ctx, 501, ticket.ID); !errors.Is(err, ErrPermission) {
		t.Fatal("user self compensated", err)
	}
	var created atomic.Int32
	var wg sync.WaitGroup
	before := time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, yes, err := svc.CompensateTicket(ctx, 100, ticket.ID)
			if err != nil {
				t.Error(err)
				return
			}
			if yes {
				created.Add(1)
			}
			if o.PlanDays == nil || *o.PlanDays != 3 {
				t.Error("incorrect period")
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 || panel.expireUpdates() != 1 {
		t.Fatal("repeated compensation", created.Load(), panel.expireUpdates())
	}
	expiry := panel.lastExpiry()
	if expiry.Before(before.Add(72*time.Hour-time.Second)) || expiry.After(time.Now().Add(72*time.Hour+time.Second)) {
		t.Fatal("incorrect compensation expiry", expiry)
	}
	if _, err := db.RemoveStaff(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompensateTicket(ctx, 100, ticket.ID); !errors.Is(err, ErrPermission) {
		t.Fatal("revoked support accepted", err)
	}
}

func TestCompensationPanelFailureKeepsQuotaAndResumesSameOrder(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	svc.panel = nil
	svc.cfg.SupportCompensationDailyDays = 3
	if err := db.AddStaff(ctx, 100, storage.RoleSupport, "Support", 1); err != nil {
		t.Fatal(err)
	}
	s, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Issue", "Help", "human")
	if err != nil {
		t.Fatal(err)
	}
	o, created, err := svc.CompensateTicket(ctx, 100, ticket.ID)
	if err != nil || !created || o.Status != domain.OrderPaid {
		t.Fatal("failed to queue durable compensation", o, created, err)
	}
	other, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Issue", "Help", "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompensateTicket(ctx, 100, other.ID); !errors.Is(err, storage.ErrCompensationLimit) {
		t.Fatal("failure freed reserved quota", err)
	}
	repeated, created, err := svc.CompensateTicket(ctx, 100, ticket.ID)
	if err != nil || created || repeated.OrderID != o.OrderID {
		t.Fatal("retry created new order", err)
	}
	// A new Service models a restart with the panel working again.
	restarted := New(db, panel, svc.registry, svc.cfg)
	updated, err := restarted.CheckOrder(ctx, o.OrderID)
	if err != nil || updated.Status != domain.OrderProvisioned {
		t.Fatal("recovery failed", updated, err)
	}
	if _, _, err := restarted.CompensateTicket(ctx, 100, ticket.ID); err != nil {
		t.Fatal(err)
	}
	if panel.expireUpdates() != 1 {
		t.Fatal("recovery issued twice")
	}
}

func TestCompensationOwnerBypassesCapAndClosedTicketIsRejected(t *testing.T) {
	svc, db, _ := testProvisionService(t)
	ctx := context.Background()
	svc.cfg.AdminIDs = map[int64]bool{1: true}
	svc.cfg.SupportCompensationDailyDays = 0
	s, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ticket, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Issue", "Help", "human")
		if err != nil {
			t.Fatal(err)
		}
		if _, created, err := svc.CompensateTicket(ctx, 1, ticket.ID); err != nil || !created {
			t.Fatal("owner cap", err)
		}
	}
	ticket, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Issue", "Help", "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CloseTicket(ctx, ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompensateTicket(ctx, 1, ticket.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("closed ticket compensated", err)
	}
}
