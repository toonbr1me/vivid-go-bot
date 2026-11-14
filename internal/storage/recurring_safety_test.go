package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestMigrationsPreserveInterfaceAndRecurringState(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveBotCard(ctx, 7, 42, map[string]any{"private": "preserved"}); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveNativeMessage(ctx, 7, 43); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveCheckoutPreference(ctx, 7, "30d", true); err != nil {
		t.Fatal(err)
	}
	agreement := domain.RecurringAgreement{ID: "agreement-1", SubjectID: subject.ID, Provider: "yookassa", Method: "card", PlanID: "30d", Amount: 10001, Currency: "RUB", Days: 30}
	if err = db.CreateRecurring(ctx, agreement); err != nil {
		t.Fatal(err)
	}
	when := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	if err = db.SetRecurringExternal(ctx, agreement.ID, "saved-card", true); err != nil {
		t.Fatal(err)
	}
	if err = db.SetRecurringSchedule(ctx, agreement.ID, &when, nil); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	plan, recurring, err := db.CheckoutPreference(ctx, 7)
	if err != nil || plan != "30d" || !recurring || db.NativeMessageID(ctx, 7) != 43 {
		t.Fatal("migration reset preferences")
	}
	id, raw, err := db.BotCard(ctx, 7)
	if err != nil || id != 42 || !strings.Contains(string(raw), "preserved") {
		t.Fatal("migration lost active card")
	}
	a, err := db.RecurringByID(ctx, agreement.ID)
	if err != nil || a.Amount != 10001 || !a.Activated || a.ExternalID != "saved-card" || a.NextChargeAt == nil || !a.NextChargeAt.Equal(when) {
		t.Fatal("migration changed authorised terms")
	}
}

func TestOnlyOneEnabledRecurringAgreementPerSubject(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	a := domain.RecurringAgreement{ID: "first", SubjectID: subject.ID, Provider: "yookassa", Method: "card", PlanID: "30d", Amount: 10000, Currency: "RUB", Days: 30}
	if err = db.CreateRecurring(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.ID = "second"
	if err = db.CreateRecurring(ctx, a); err == nil {
		t.Fatal("two active agreements may double-charge a user")
	}
	if err = db.DisableRecurring(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateRecurring(ctx, a); err != nil {
		t.Fatal(err)
	}
	selected, err := db.RecurringForSubject(ctx, subject.ID)
	if err != nil || selected.ID != "second" {
		t.Fatal("disabled agreement still selected")
	}
}

func TestRecurringTransactionCannotBeReboundToAnotherOrder(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		if _, err := db.CreateOrder(ctx, domain.Order{OrderID: id, Provider: "platega", PlanID: "30d", Kind: domain.KindSubscription, Amount: 10000, Currency: "RUB", Status: domain.OrderPending}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.LinkRecurringCharge(ctx, "platega", "tx-1", "first"); err != nil {
		t.Fatal(err)
	}
	if err := db.LinkRecurringCharge(ctx, "platega", "tx-1", "first"); err != nil {
		t.Fatal("identical replay must remain idempotent")
	}
	if err := db.LinkRecurringCharge(ctx, "platega", "tx-1", "second"); err == nil {
		t.Fatal("transaction silently rebound or conflicting callback silently accepted")
	}
	order, err := db.RecurringChargeOrder(ctx, "platega", "tx-1")
	if err != nil || order != "first" {
		t.Fatal("transaction ownership changed")
	}
}

func TestCreateTicketRollsBackIfFirstMessageCannotBeSaved(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `CREATE FUNCTION fail_initial_message() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated storage failure'; END $$; CREATE TRIGGER fail_initial_message BEFORE INSERT ON support_messages FOR EACH ROW EXECUTE FUNCTION fail_initial_message()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_initial_message ON support_messages; DROP FUNCTION IF EXISTS fail_initial_message()`)
	})
	if _, err = db.CreateTicket(ctx, subject.ID, subject.TelegramID, "Тема", "Исходный текст", storage.SupportModeHuman); err == nil {
		t.Fatal("write failure hidden")
	}
	tickets, err := db.TicketsForSubject(ctx, subject.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Fatal("half-created ticket left behind after initial message failure")
	}
}

func TestAppendTicketMessageRollsBackIfTicketUpdateFails(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, subject.ID, subject.TelegramID, "Тема", "Первое сообщение", storage.SupportModeHuman)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `CREATE FUNCTION fail_ticket_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated ticket update failure'; END $$; CREATE TRIGGER fail_ticket_update BEFORE UPDATE ON support_tickets FOR EACH ROW EXECUTE FUNCTION fail_ticket_update()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_ticket_update ON support_tickets; DROP FUNCTION IF EXISTS fail_ticket_update()`)
	})
	if _, err = db.AddTicketMessage(ctx, ticket.ID, subject.ID, subject.TelegramID, "Повторяемое сообщение", false, "", ""); err == nil {
		t.Fatal("ticket update failure hidden")
	}
	messages, err := db.TicketMessages(ctx, ticket.ID, 10)
	if err != nil || len(messages) != 1 {
		t.Fatal("failed append left a message that a retry would duplicate")
	}
}
