package payments

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
)

func TestLinkedWebSubscriptionKeepsAccessAndRenewal(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()
	if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	subject, err := db.UpsertGoogleSubject(ctx, "linked-web", "web@example.com", "Web")
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("web_%d", subject.ID)
	if err := db.SetRemnawaveUsername(ctx, subject.ID, name); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().UTC().Add(10 * 24 * time.Hour)
	panel.users[-42] = remnawave.User{ID: 42, Username: name, Status: "ACTIVE", ExpireAt: remnawave.Timestamp{Time: expiry}, SubscriptionURL: "https://example.com/original"}
	code, err := db.CreateLinkCode(ctx, subject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AttachTelegramByCode(ctx, code, subject.ID, 7654); err != nil {
		t.Fatal(err)
	}
	subject, err = db.SubjectByTelegramID(ctx, 7654)
	if err != nil {
		t.Fatal(err)
	}
	access, err := svc.Access(ctx, subject, "tg_7654")
	if err != nil || !access.Active || access.PanelUserID != 42 || access.URL != "https://example.com/original" {
		t.Fatalf("access=%+v err=%v", access, err)
	}
	days := 30
	order, err := db.CreateOrder(ctx, domain.Order{OrderID: "linked-renewal", Provider: "balance", AccountID: &subject.ID, TelegramID: subject.TelegramID, Username: ptrOrNil("tg_7654"), PlanID: "30d", PlanDays: &days, Kind: domain.KindSubscription, Status: domain.OrderPaid, Currency: "RUB", Amount: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Provision(ctx, order); err != nil {
		t.Fatal(err)
	}
	if panel.created != 0 || panel.lastExpiry().Sub(expiry) != 30*24*time.Hour {
		t.Fatalf("renewed another profile: created=%d expiry=%s", panel.created, panel.lastExpiry())
	}
	if id, err := db.PeekLinkCode(ctx, code); err != nil || id != nil {
		t.Fatal("link code not consumed", id, err)
	}
}

func TestPanelIdentityConflictIsNotSilentlyMerged(t *testing.T) {
	panel := newFakePanel()
	panel.users[-42] = remnawave.User{ID: 42, Username: "web_42"}
	panel.users[7] = remnawave.User{ID: 7, Username: "tg_7"}
	svc := New(nil, panel, &Registry{}, &config.Settings{})
	tg := int64(7)
	if _, err := svc.findPanelUsers(context.Background(), &tg, "web_42"); err == nil {
		t.Fatal("different subscriptions silently merged")
	}
}

func TestTrialUpgradeAppliesPaidProfileAndResetsOnlyOnce(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprint(reset), func(t *testing.T) {
			svc, db, panel := testProvisionService(t)
			ctx := context.Background()
			if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true}); err != nil {
				t.Fatal(err)
			}
			svc.cfg.TrialBotDays = 1
			svc.cfg.TrialTrafficBytes = new(int64)
			*svc.cfg.TrialTrafficBytes = 1024
			svc.cfg.TrialResetOnPaid = reset
			subject, err := db.EnsureTelegramSubject(ctx, 123456)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := svc.Plan(ctx, "trial_bot")
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.GrantTrial(ctx, subject, "tg_123456", plan); err != nil {
				t.Fatal(err)
			}
			user := panel.users[123456]
			user.TrafficUsedBytes = 1024
			panel.users[123456] = user
			days := 30
			order, err := db.CreateOrder(ctx, domain.Order{OrderID: "paid-upgrade", Provider: "balance", TelegramID: subject.TelegramID, AccountID: &subject.ID, PlanID: "30d", PlanDays: &days, Kind: domain.KindSubscription, Status: domain.OrderPaid, Amount: 10000, Currency: "RUB"})
			if err != nil {
				t.Fatal(err)
			}
			// Fail after the panel write, before finalization, then resume the same order.
			_, err = db.Pool.Exec(ctx, `CREATE FUNCTION fail_upgrade_finalize() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.order_id='paid-upgrade' AND NEW.provisioned_at IS NOT NULL THEN RAISE EXCEPTION 'finalize unavailable'; END IF; RETURN NEW; END $$;
			CREATE TRIGGER fail_upgrade_finalize BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION fail_upgrade_finalize()`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Provision(ctx, order); err == nil {
				t.Fatal("expected finalization failure")
			}
			// Another paid order can run before the first one is finalized.
			// Reset ownership must therefore be per subject, not per order.
			sibling := order
			sibling.OrderID = "paid-sibling"
			sibling, err = db.CreateOrder(ctx, sibling)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Provision(ctx, sibling); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool.Exec(ctx, `DROP TRIGGER fail_upgrade_finalize ON orders; DROP FUNCTION fail_upgrade_finalize()`); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Provision(ctx, order); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Provision(ctx, order); err != nil {
				t.Fatal(err)
			}
			user = panel.users[123456]
			if user.TrafficLimitBytes == nil || *user.TrafficLimitBytes != 0 {
				t.Fatal("trial limit survived upgrade", user.TrafficLimitBytes)
			}
			want := 0
			if reset {
				want = 1
			}
			if panel.resetCalls != want {
				t.Fatalf("reset calls=%d want=%d", panel.resetCalls, want)
			}
		})
	}
}

func TestExistingUserReceivesConfiguredTrafficLimitEvenWithSameExpiry(t *testing.T) {
	panel := newFakePanel()
	tg := int64(7)
	expiry := time.Now().UTC().AddDate(0, 0, 30)
	trial, paid := int64(1), int64(100)
	panel.users[tg] = remnawave.User{ID: 7, Username: "tg_7", ExpireAt: remnawave.Timestamp{Time: expiry}, TrafficLimitBytes: &trial}
	svc := New(nil, panel, &Registry{}, &config.Settings{})
	if _, err := svc.ensureUserExpireAt(context.Background(), &tg, "tg_7", expiry, domain.ProvisioningProfile{TrafficLimitBytes: &paid}); err != nil {
		t.Fatal(err)
	}
	if *panel.users[tg].TrafficLimitBytes != paid {
		t.Fatal("profile skipped because expiry already matched")
	}
}
