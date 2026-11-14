package storage_test

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
	"testing"
	"time"
)

func TestSupportReadResolutionAndStaleReplies(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	ticket, err := db.CreateTicketContent(ctx, owner.ID, owner.TelegramID, "Issue", "description", "", "Заказ: order-1", "photo-secret", "photo")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := db.AddTicketMessage(ctx, ticket.ID, owner.ID, nil, "answer", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ticket, _ = db.TicketByID(ctx, ticket.ID)
	if !ticket.Unread() || ticket.Context != "Заказ: order-1" {
		t.Fatalf("lost context/unread: %+v", ticket)
	}
	_ = db.MarkTicketRead(ctx, ticket.ID, other.ID, reply.ID)
	ticket, _ = db.TicketByID(ctx, ticket.ID)
	if !ticket.Unread() {
		t.Fatal("foreign user marked read")
	}
	_ = db.MarkTicketRead(ctx, ticket.ID, owner.ID, reply.ID)
	later, err := db.AddTicketMessage(ctx, ticket.ID, owner.ID, nil, "new answer", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = db.MarkTicketRead(ctx, ticket.ID, owner.ID, reply.ID)
	ticket, _ = db.TicketByID(ctx, ticket.ID)
	if !ticket.Unread() || ticket.LastStaffMessageID != later.ID {
		t.Fatal("old acknowledgement swallowed a new reply")
	}
	if ok, _ := db.ResolveOwnTicket(ctx, ticket.ID, other.ID); ok {
		t.Fatal("foreign resolution allowed")
	}
	if ok, err := db.ResolveOwnTicket(ctx, ticket.ID, owner.ID); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := db.AddOwnerTicketMessage(ctx, ticket.ID, owner.ID, owner.TelegramID, "stale reply", "", ""); err == nil {
		t.Fatal("stale reply reopened a resolved ticket")
	}
	messages, _ := db.TicketMessages(ctx, ticket.ID, 20)
	if len(messages) != 3 || messages[2].FileID == nil || *messages[2].FileID != "photo-secret" {
		t.Fatal("history/attachment lost")
	}
}

func TestRecurringStopReasonSurvivesCallbacksAndMigrations(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	a := domain.RecurringAgreement{ID: "agreement", SubjectID: owner.ID, Provider: "yookassa", Method: "card", Amount: 10000, Currency: "RUB", Days: 30}
	if err := db.CreateRecurring(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := db.StopRecurring(ctx, a.ID, "user"); err != nil {
		t.Fatal(err)
	}
	next := time.Now().Add(time.Hour)
	if err := db.ConfirmRecurringCharge(ctx, a.ID, "saved", &next); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := db.LatestRecurringForSubject(ctx, owner.ID)
	if err != nil || a.Enabled || a.StopReason != "user" || a.StoppedAt == nil {
		t.Fatalf("cancellation lost: %+v %v", a, err)
	}
	if _, err := db.RecurringForSubject(ctx, owner.ID); err == nil {
		t.Fatal("disabled agreement selected for charging")
	}
}

func TestOperationsFindsOldPaidOrdersOutsideRecentPage(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	tg := int64(7)
	for i := 0; i < 170; i++ {
		_, err := db.CreateOrder(ctx, domain.Order{OrderID: time.Unix(int64(i), 0).Format(time.RFC3339), TelegramID: &tg, Provider: "test", Status: domain.OrderProvisioned, Kind: domain.KindSubscription, Amount: 100, Currency: "RUB"})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := db.CreateOrder(ctx, domain.Order{OrderID: "old-paid", TelegramID: &tg, Provider: "test", Status: domain.OrderPaid, Kind: domain.KindSubscription, Amount: 100, Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `UPDATE orders SET created_at=now()-interval '7 days' WHERE order_id='old-paid'`)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := db.Operations(ctx)
	if err != nil || ops.AwaitingAccess != 1 {
		t.Fatalf("wrong queue: %+v %v", ops, err)
	}
	orders, err := db.AttentionOrders(ctx, 150)
	if err != nil || len(orders) != 1 || orders[0].OrderID != "old-paid" {
		t.Fatal("old paid order hidden by recent pagination")
	}
}

func TestLegacyTelegramTicketCanBeReadRepliedAndResolved(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Imported", "history", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE support_tickets SET subject_id=NULL WHERE id=$1`, ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddOwnerTicketMessage(ctx, ticket.ID, other.ID, other.TelegramID, "foreign", "", ""); err == nil {
		t.Fatal("foreign legacy reply allowed")
	}
	if _, err = db.AddOwnerTicketMessage(ctx, ticket.ID, owner.ID, owner.TelegramID, "owner", "", ""); err != nil {
		t.Fatal(err)
	}
	reply, err := db.AddTicketMessage(ctx, ticket.ID, owner.ID, nil, "staff", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.MarkTicketRead(ctx, ticket.ID, owner.ID, reply.ID); err != nil {
		t.Fatal(err)
	}
	ticket, _ = db.TicketByID(ctx, ticket.ID)
	if ticket.Unread() {
		t.Fatal("legacy ticket unread marker stuck")
	}
	if ok, err := db.ResolveOwnTicket(ctx, ticket.ID, owner.ID); err != nil || !ok {
		t.Fatal("legacy ticket cannot be resolved")
	}
}

func TestOperationsDropsSupersededRecurringWarning(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	a := domain.RecurringAgreement{ID: "old", SubjectID: owner.ID, Provider: "yookassa", Method: "card", Amount: 10000, Currency: "RUB", Days: 30}
	if err := db.CreateRecurring(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := db.StopRecurring(ctx, a.ID, "declined"); err != nil {
		t.Fatal(err)
	}
	ops, err := db.Operations(ctx)
	if err != nil || ops.RecurringErrors != 1 {
		t.Fatal("decline missing")
	}
	a.ID = "new"
	if err = db.CreateRecurring(ctx, a); err != nil {
		t.Fatal(err)
	}
	ops, err = db.Operations(ctx)
	issues, e := db.RecurringIssues(ctx, 100)
	if err != nil || e != nil || ops.RecurringErrors != 0 || len(issues) != 0 {
		t.Fatal("replaced agreement still raises warning")
	}
}
