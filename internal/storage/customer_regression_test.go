package storage_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestStaleAssistantTicketRemainsOpenAndTransferIsAtomic(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Unanswered", "Please help", storage.SupportModeAssistant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE support_tickets SET updated_at=now()-interval '3 days' WHERE id=$1`, ticket.ID); err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `CREATE FUNCTION fail_handoff_note() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'note failed'; END $$;
	CREATE TRIGGER fail_handoff_note BEFORE INSERT ON support_messages FOR EACH ROW EXECUTE FUNCTION fail_handoff_note()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CloseIdleAITickets(ctx, 48, "Transferred"); err == nil {
		t.Fatal("expected note failure")
	}
	var mode, status string
	if err := db.Pool.QueryRow(ctx, `SELECT support_mode,status FROM support_tickets WHERE id=$1`, ticket.ID).Scan(&mode, &status); err != nil {
		t.Fatal(err)
	}
	if mode != storage.SupportModeAssistant || status != "open" {
		t.Fatal("failed transaction changed ticket", mode, status)
	}
	if _, err := db.Pool.Exec(ctx, `DROP TRIGGER fail_handoff_note ON support_messages; DROP FUNCTION fail_handoff_note()`); err != nil {
		t.Fatal(err)
	}
	if n, err := db.CloseIdleAITickets(ctx, 48, "Transferred"); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if n, err := db.CloseIdleAITickets(ctx, 48, "Transferred"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT support_mode,status FROM support_tickets WHERE id=$1`, ticket.ID).Scan(&mode, &status); err != nil {
		t.Fatal(err)
	}
	if mode != storage.SupportModeHuman || status != "open" {
		t.Fatal("unanswered issue closed", mode, status)
	}
}

func TestPartnerInviteClaimRollbackAndConcurrentRepeat(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	partner, err := db.CreatePartner(ctx, "Partner", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := db.EnsureTelegramSubject(ctx, 502)
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureTelegramSubject(ctx, 503)
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.CreatePartnerInviteCode(ctx, partner.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `CREATE FUNCTION fail_partner_link() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'link failed'; END $$;
	CREATE TRIGGER fail_partner_link BEFORE UPDATE ON partners FOR EACH ROW EXECUTE FUNCTION fail_partner_link()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimPartnerInvite(ctx, code, s.ID); err == nil {
		t.Fatal("expected failure")
	}
	if invite, err := db.PartnerInvite(ctx, code); err != nil || invite == nil {
		t.Fatal("invitation burned", invite, err)
	}
	if _, err := db.Pool.Exec(ctx, `DROP TRIGGER fail_partner_link ON partners; DROP FUNCTION fail_partner_link()`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := db.ClaimPartnerInvite(ctx, code, s.ID); err == nil {
				successes.Add(1)
			} else {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 6 {
		t.Fatal("same-account repeat failed", successes.Load())
	}
	if _, err := db.ClaimPartnerInvite(ctx, code, other.ID); err == nil {
		t.Fatal("used invitation claimed by another account")
	}
}

func TestPartnerTotalsUseEntireHistoryAndKeepCancelledSeparate(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	partner, err := db.CreatePartner(ctx, "Partner", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 55; i++ {
		id := fmt.Sprintf("reward-%d", i)
		if _, err := db.CreateOrder(ctx, domain.Order{OrderID: id, Provider: "balance", PlanID: "30d", Kind: domain.KindSubscription, Status: domain.OrderProvisioned, Currency: "RUB", Amount: 10000}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreditPartnerReward(ctx, partner.ID, id, nil, 101, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE partner_rewards SET status='cancelled' WHERE order_id='reward-0'`); err != nil {
		t.Fatal(err)
	}
	totals, err := db.PartnerRewardTotals(ctx, partner.ID)
	if err != nil || totals.Accrued != 54*101 || totals.Cancelled != 101 {
		t.Fatal(totals, err)
	}
}

func TestExpiredTelegramLinkCannotBindOrConsumeAnotherAccount(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.UpsertGoogleSubject(ctx, "web", "web@example.com", "Web")
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.CreateLinkCode(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE telegram_link_codes SET expires_at=now()-interval '1 minute' WHERE code=$1`, code); err != nil {
		t.Fatal(err)
	}
	if err := db.AttachTelegramByCode(ctx, code, s.ID, 1234); err == nil {
		t.Fatal("expired code attached")
	}
	s, err = db.SubjectByID(ctx, s.ID)
	if err != nil || s.TelegramID != nil {
		t.Fatal(s, err)
	}
}

func TestPartnerClaimAndIncomingRewardsPreserveEveryCredit(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	p, err := db.CreatePartner(ctx, "Partner", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := db.EnsureTelegramSubject(ctx, 8007)
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.CreatePartnerInviteCode(ctx, p.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := db.ClaimPartnerInvite(ctx, code, s.ID); err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := db.CreditPartnerReward(ctx, p.ID, fmt.Sprintf("concurrent-reward-%d", i), nil, 101, nil); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if balance, err := db.Balance(ctx, s.ID); err != nil || balance != 20*101 {
		t.Fatal("reward lost while linking", balance, err)
	}
	if totals, err := db.PartnerRewardTotals(ctx, p.ID); err != nil || totals.Credited != 20*101 || totals.Accrued != 0 {
		t.Fatal(totals, err)
	}
}
