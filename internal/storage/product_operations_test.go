package storage_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
	"testing"
	"time"
)

func TestStaffWorkflowKeepsNotesPrivateAndChecksHolder(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 901)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{100, 200} {
		if err = db.AddStaff(ctx, id, "support", "Agent", 999); err != nil {
			t.Fatal(err)
		}
	}
	ticket, err := db.CreateTicket(ctx, subject.ID, subject.TelegramID, "Problem", "Help", "human")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StaffTicketAction(ctx, ticket.ID, 100, "take", "", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.StaffTicketAction(ctx, ticket.ID, 200, "reply", "wrong agent", 0); !errors.Is(err, storage.ErrTicketBusy) {
		t.Fatalf("holder bypass: %v", err)
	}
	if err = db.StaffTicketAction(ctx, ticket.ID, 100, "note", "private diagnosis", 0); err != nil {
		t.Fatal(err)
	}
	public, err := db.TicketHistory(ctx, ticket.ID)
	if err != nil || len(public) != 1 {
		t.Fatalf("private note in public history: %+v %v", public, err)
	}
	staff, err := db.StaffTicketHistory(ctx, ticket.ID)
	if err != nil || len(staff) != 2 || !staff[1].Internal {
		t.Fatalf("staff lost note: %+v %v", staff, err)
	}
	if err = db.StaffTicketAction(ctx, ticket.ID, 100, "transfer", "", 200); err != nil {
		t.Fatal(err)
	}
	if err = db.StaffTicketReply(ctx, ticket.ID, 200, "response", "file-id", "photo"); err != nil {
		t.Fatal(err)
	}
	queue, err := db.StaffQueue(ctx, 200, "waiting", 0)
	if err != nil || queue.Total != 1 {
		t.Fatalf("waiting queue: %+v %v", queue, err)
	}
	if err = db.StaffTicketAction(ctx, ticket.ID, 200, "close", "", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.StaffTicketAction(ctx, ticket.ID, 200, "reopen", "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestOrderSearchFindsOldPaymentsAndKeepsAccountScope(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 902)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO orders(order_id,provider,account_id,plan_id,kind,amount,currency,status,created_at) SELECT 'history-'||n,'wata',$1,'30d','subscription',100,'RUB','pending',now()-n*interval '1 hour' FROM generate_series(1,190)n`, subject.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.SearchOrders(ctx, storage.OrderFilter{Query: "history-190"})
	if err != nil || page.Total != 1 || page.Orders[0].OrderID != "history-190" {
		t.Fatalf("old order: %+v %v", page, err)
	}
	foreign := subject.ID + 999
	page, err = db.SearchOrders(ctx, storage.OrderFilter{SubjectID: &foreign})
	if err != nil || page.Total != 0 {
		t.Fatalf("account leak: %+v %v", page, err)
	}
	page, err = db.SearchOrders(ctx, storage.OrderFilter{SubjectID: &subject.ID, Page: 3})
	if err != nil || page.Total != 190 || len(page.Orders) != 40 {
		t.Fatalf("pagination: %+v %v", page, err)
	}
}

func TestWithdrawalDecisionSpendsBalanceOnlyOnce(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.EnsureTelegramSubject(ctx, 903)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddBalance(ctx, s.ID, domain.FromRubles(200), nil); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateWithdrawal(ctx, s.ID, s.TelegramID, "", "sbp", "test", domain.FromRubles(100)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Withdrawals(ctx, "pending", 10)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = db.DecideWithdrawalAtomic(ctx, rows[0].ID, 100, "approved"); err != nil {
			t.Fatal(err)
		}
	}
	balance, err := db.Balance(ctx, s.ID)
	if err != nil || balance != domain.FromRubles(100) {
		t.Fatalf("double payout: %v %v", balance, err)
	}
}

func TestRefundRequiresProviderReferenceAndDoesNotResurrect(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.EnsureTelegramSubject(ctx, 904)
	if err != nil {
		t.Fatal(err)
	}
	order, err := db.CreateOrder(ctx, domain.Order{OrderID: "refunded-order", AccountID: &s.ID, TelegramID: s.TelegramID, Provider: "wata", PlanID: "30d", Kind: domain.KindSubscription, Amount: 10000, Currency: "RUB", Status: domain.OrderPaid})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateRefundCase(ctx, 100, order.OrderID, "customer request", "completed", ""); err == nil {
		t.Fatal("accepted unconfirmed refund")
	}
	if err = db.UpdateRefundCase(ctx, 100, order.OrderID, "customer request", "completed", "provider-ref-1"); err != nil {
		t.Fatal(err)
	}
	changed, err := db.MarkPaid(ctx, order.OrderID, "replayed-payment", order.CreatedAt)
	if err != nil || changed {
		t.Fatalf("refunded order resurrected: %v %v", changed, err)
	}
}

func TestBehavioralSegmentsUseVerifiedConnectionAndRespectPreferences(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	for _, id := range []int64{911, 912, 913, 914} {
		s, err := db.EnsureTelegramSubject(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		provider, status, amount := "wata", "provisioned", 100
		expiry := time.Now().Add(24 * time.Hour)
		if id == 911 {
			provider = "trial"
			amount = 0
		}
		if id == 912 {
			status = "pending"
		}
		if id == 913 {
			expiry = time.Now().Add(-4 * 24 * time.Hour)
		}
		_, err = db.Pool.Exec(ctx, `INSERT INTO orders(order_id,account_id,telegram_id,provider,plan_id,kind,amount,currency,status,created_at,expire_at) VALUES($1,$2,$3,$4,'30d','subscription',$5,'RUB',$6,now()-interval '2 hours',$7)`, fmt.Sprint(id), s.ID, id, provider, amount, status, expiry)
		if err != nil {
			t.Fatal(err)
		}
		if id == 914 {
			if err = db.RecordConnectionCheck(ctx, s.ID, true, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, segment := range []string{"trial_only", "abandoned", "expired", "not_connected"} {
		b, err := db.CreateBroadcast(ctx, 100, segment, "message", "")
		if err != nil || b.Total != 1 {
			t.Fatalf("%s: %+v %v", segment, b, err)
		}
	}
	if err := db.SaveNotificationSettings(ctx, 914, storage.NotificationSettings{Expiry: true, Activity: false}); err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateBroadcast(ctx, 100, "not_connected", "message", "")
	if err != nil || b.Total != 0 {
		t.Fatalf("ignored opt-out: %+v %v", b, err)
	}
}
