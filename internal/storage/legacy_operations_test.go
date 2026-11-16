package storage_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStaffPermissionsFailClosed(t *testing.T) {
	for _, role := range []string{storage.RoleOwner, storage.RoleAdmin, storage.RoleSupport, "future-role", ""} {
		for _, action := range []string{"support.compensate", "subscription.grant", "broadcast.send", "tickets.view", "typo.action"} {
			want := action != "typo.action" && (role == storage.RoleOwner || role == storage.RoleAdmin || role == storage.RoleSupport && (action == "support.compensate" || action == "tickets.view"))
			if got := storage.RoleCan(role, action); got != want {
				t.Errorf("%s/%s: %v", role, action, got)
			}
		}
	}
	db := testdb.Open(t)
	ctx := context.Background()
	for i, role := range []string{"support", "future-role", "owner"} {
		if err := db.AddStaff(ctx, int64(i+10), role, "", 1); err != nil {
			t.Fatal(err)
		}
	}
	if db.Permission(ctx, 10, "subscription.grant", nil) || db.Permission(ctx, 11, "tickets.view", nil) || db.Permission(ctx, 12, "typo.action", nil) || db.Permission(ctx, 1, "typo.action", map[int64]bool{1: true}) {
		t.Fatal("unrecognized or forbidden permission granted")
	}
	if _, err := db.RemoveStaff(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if db.Permission(ctx, 10, "support.compensate", nil) {
		t.Fatal("revoked role accepted")
	}
}

func TestBroadcastAudienceSnapshotConfirmationAndRecovery(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		s, err := db.EnsureTelegramSubject(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if id == 1 {
			expire := time.Now().Add(time.Hour)
			if _, err := db.CreateOrder(ctx, domain.Order{OrderID: "active", Provider: "test", AccountID: &s.ID, TelegramID: &id, PlanID: "30d", Kind: domain.KindSubscription, Currency: "RUB", Status: domain.OrderProvisioned, ExpireAt: &expire}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE orders SET expire_at=now()+interval '1 hour' WHERE order_id='active'`); err != nil {
		t.Fatal(err)
	}
	active, err := db.PollAudience(ctx, "active", 100)
	if err != nil || len(active) != 1 || active[0] != 1 {
		t.Fatalf("active=%v err=%v", active, err)
	}
	inactive, err := db.PollAudience(ctx, "inactive", 100)
	if err != nil || len(inactive) != 2 {
		t.Fatalf("inactive=%v err=%v", inactive, err)
	}
	b, err := db.CreateBroadcast(ctx, 100, "all", "hello", "")
	if err != nil || b.Total != 3 {
		t.Fatalf("draft: %+v %v", b, err)
	}
	if _, err := db.ClaimBroadcast(ctx); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("draft was sent")
	}
	if _, err := db.EnsureTelegramSubject(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if yes, err := db.ConfirmBroadcast(ctx, b.ID, 101); yes || err != nil {
		t.Fatal("other actor confirmed draft")
	}
	if _, err := db.Broadcast(ctx, b.ID, 101); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("draft author isolation")
	}
	var confirmed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			yes, err := db.ConfirmBroadcast(ctx, b.ID, 100)
			if err != nil {
				t.Error(err)
			}
			if yes {
				confirmed.Add(1)
			}
		}()
	}
	wg.Wait()
	if confirmed.Load() != 1 {
		t.Fatal("duplicate confirmation", confirmed.Load())
	}
	if yes, err := db.CancelBroadcast(ctx, b.ID, 100); yes || err != nil {
		t.Fatal("cancelled already queued delivery")
	}
	first, err := db.ClaimBroadcast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBroadcastDelivery(ctx, first, "pending", time.Hour); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		d, err := db.ClaimBroadcast(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if d.TelegramID == first.TelegramID || d.TelegramID == 4 {
			t.Fatal("retry delay or snapshot ignored")
		}
		if err := db.FinishBroadcastDelivery(ctx, d, "sent", 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ClaimBroadcast(ctx); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("unexpected pending recipient")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE broadcast_deliveries SET available_at=now()-interval '1 second' WHERE status='pending'`); err != nil {
		t.Fatal(err)
	}
	lease, err := db.ClaimBroadcast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBroadcastDelivery(ctx, first, "sent", 0); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.Pool.QueryRow(ctx, `SELECT status FROM broadcast_deliveries WHERE broadcast_id=$1 AND telegram_id=$2`, lease.BroadcastID, lease.TelegramID).Scan(&status); err != nil || status != "sending" {
		t.Fatal("stale lease completed new attempt", status, err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE broadcast_deliveries SET claimed_at=now()-interval '6 minutes' WHERE status='sending'`); err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverBroadcastDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := db.Broadcast(ctx, b.ID, 100)
	if err != nil || final.Total != 3 || final.Sent != 2 || final.Uncertain != 1 || final.Status != "completed" {
		t.Fatalf("outcome=%+v %v", final, err)
	}
	if _, err := db.ClaimBroadcast(ctx); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("ambiguous delivery retried")
	}
}

func TestBroadcastExpiredEmptyCancelledAndConcurrentClaims(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	empty, err := db.CreateBroadcast(ctx, 1, "all", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if yes, err := db.ConfirmBroadcast(ctx, empty.ID, 1); yes || err != nil {
		t.Fatal("confirmed empty audience")
	}
	if _, err := db.CreateBroadcast(ctx, 1, "unknown", "hello", ""); err == nil {
		t.Fatal("unknown audience")
	}
	if _, err := db.CreateBroadcast(ctx, 1, "all", " ", ""); err == nil {
		t.Fatal("empty broadcast")
	}
	for id := int64(1); id <= 20; id++ {
		if _, err := db.EnsureTelegramSubject(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	expired, err := db.CreateBroadcast(ctx, 1, "all", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE broadcasts SET created_at=now()-interval '21 minutes' WHERE id=$1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if yes, err := db.ConfirmBroadcast(ctx, expired.ID, 1); yes || err != nil {
		t.Fatal("expired draft confirmed")
	}
	cancelled, err := db.CreateBroadcast(ctx, 1, "all", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if yes, err := db.CancelBroadcast(ctx, cancelled.ID, 1); !yes || err != nil {
		t.Fatal(err)
	}
	if yes, err := db.ConfirmBroadcast(ctx, cancelled.ID, 1); yes || err != nil {
		t.Fatal("cancelled draft confirmed")
	}
	b, err := db.CreateBroadcast(ctx, 1, "all", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmBroadcast(ctx, b.ID, 1); err != nil {
		t.Fatal(err)
	}
	claims := make(chan int64, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := db.ClaimBroadcast(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			claims <- d.TelegramID
		}()
	}
	wg.Wait()
	close(claims)
	seen := map[int64]bool{}
	for id := range claims {
		if seen[id] {
			t.Fatal("recipient claimed twice")
		}
		seen[id] = true
	}
	if len(seen) != 20 {
		t.Fatal("missing recipients", len(seen))
	}
}

func TestCompensationReservationSerializesQuotaAndTickets(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	tickets := []storage.Ticket{}
	for i := 0; i < 8; i++ {
		x, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "issue", "help", "human")
		if err != nil {
			t.Fatal(err)
		}
		tickets = append(tickets, x)
	}
	var created atomic.Int32
	var wg sync.WaitGroup
	for _, ticket := range tickets {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_, ok, err := db.ReserveCompensation(ctx, 100, id, s, 6, false, "RUB")
			if err != nil && !errors.Is(err, storage.ErrCompensationLimit) {
				t.Error(err)
			}
			if ok {
				created.Add(1)
			}
		}(ticket.ID)
	}
	wg.Wait()
	if created.Load() != 2 {
		t.Fatal("quota bypassed", created.Load())
	}
	var ticketID int64
	var orderID string
	if err := db.Pool.QueryRow(ctx, `SELECT ticket_id,order_id FROM support_compensations LIMIT 1`).Scan(&ticketID, &orderID); err != nil {
		t.Fatal(err)
	}
	for actor := int64(100); actor < 105; actor++ {
		o, ok, err := db.ReserveCompensation(ctx, actor, ticketID, s, 0, false, "RUB")
		if err != nil || ok || o.OrderID != orderID {
			t.Fatal("duplicate ticket charged", o, ok, err)
		}
	}
	var count int
	var days int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*),sum(amount)::int FROM staff_audit WHERE action='client.compensate'`).Scan(&count, &days); err != nil || count != 2 || days != 6 {
		t.Fatal("audit mismatch", count, days, err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE staff_audit SET created_at=now()-interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	for _, ticket := range tickets {
		if ticket.ID == ticketID {
			continue
		}
		if _, err := db.TicketCompensation(ctx, ticket.ID); err == nil {
			continue
		}
		if _, ok, err := db.ReserveCompensation(ctx, 100, ticket.ID, s, 3, false, "RUB"); err != nil || !ok {
			t.Fatal("old quota did not expire", err)
		}
		break
	}
	foreign, err := db.EnsureTelegramSubject(ctx, 999)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ReserveCompensation(ctx, 1, tickets[7].ID, foreign, 0, true, "RUB"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("foreign subject accepted", err)
	}
	if _, err := db.CloseTicket(ctx, tickets[7].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ReserveCompensation(ctx, 1, tickets[7].ID, s, 0, true, "RUB"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("closed ticket accepted", err)
	}
}

func TestPanelEventsDeduplicateAndKeepCooldownAcrossClaims(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.EnsureTelegramSubject(ctx, 501); err != nil {
		t.Fatal(err)
	}
	first := storage.PanelEvent{ID: "first", Event: "user.first_connected", TelegramID: 501}
	for i := 0; i < 3; i++ {
		if err := db.RecordPanelEvent(ctx, first); err != nil {
			t.Fatal(err)
		}
	}
	var hits int
	if err := db.Pool.QueryRow(ctx, `SELECT hits FROM funnel_daily WHERE step='first_connected'`).Scan(&hits); err != nil || hits != 1 {
		t.Fatal("duplicate funnel event", hits, err)
	}
	for i := 0; i < 5; i++ {
		if err := db.RecordPanelEvent(ctx, storage.PanelEvent{ID: fmt.Sprint(i), Event: "user_hwid_devices.added", TelegramID: 501, DeviceLabel: "Android"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.RecordPanelEvent(ctx, storage.PanelEvent{ID: "unknown", Event: "user_hwid_devices.added", TelegramID: 999}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var sends atomic.Int32
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := db.ClaimPanelEvent(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			if e.Status == "sending" {
				sends.Add(1)
			}
		}()
	}
	wg.Wait()
	if sends.Load() != 1 {
		t.Fatal("cooldown race", sends.Load())
	}
	if _, err := db.ClaimPanelEvent(ctx); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("unknown user enqueued", err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE panel_events SET claimed_at=now()-interval '6 minutes' WHERE status='sending'`); err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverPanelEvents(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimPanelEvent(ctx); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("ambiguous notification resent")
	}
}
