package storage_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func campaignFixture(t *testing.T, db *storage.DB, actor int64) (storage.Campaign, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	c, err := db.SaveCampaign(context.Background(), storage.Campaign{Title: "Trial conversion", Audience: "trial_only", Text: "Try a subscription", DailyLimit: 100, CooldownDays: 0, SendFrom: 9, SendTo: 21, StartsAt: time.Now().Add(-48 * time.Hour)}, actor)
	if err != nil {
		t.Fatal(err)
	}
	return c, now
}
func campaignTrial(t *testing.T, db *storage.DB, id int64) domain.Subject {
	t.Helper()
	ctx := context.Background()
	s, err := db.EnsureTelegramSubject(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateOrder(ctx, domain.Order{OrderID: "trial-campaign-" + time.Now().Format("150405.000000000"), Provider: "trial", AccountID: &s.ID, TelegramID: s.TelegramID, PlanID: "trial_bot", Kind: domain.KindSubscription, Currency: "RUB", Status: domain.OrderProvisioned}); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestCampaignRequiresReviewedVersionAndCapsConcurrentWorkers(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	for _, id := range []int64{801, 802, 803} {
		campaignTrial(t, db, id)
	}
	c, now := campaignFixture(t, db, 100)
	c.DailyLimit = 2
	var err error
	c, err = db.SaveCampaign(ctx, c, 100)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[int64]bool{100: true}
	if n, err := db.ProcessCampaigns(ctx, owners, now); err != nil || n != 0 {
		t.Fatalf("draft sent: %d %v", n, err)
	}
	p, err := db.CampaignPreview(ctx, c.ID)
	if err != nil || p.Eligible != 3 {
		t.Fatalf("preview %+v %v", p, err)
	}
	if err = db.SetCampaignEnabled(ctx, c.ID, c.Revision, 100, true); err != nil {
		t.Fatal(err)
	}
	var total atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Go(func() {
			n, e := db.ProcessCampaigns(ctx, owners, now)
			if e != nil {
				t.Error(e)
			}
			total.Add(int32(n))
		})
	}
	workers.Wait()
	if total.Load() != 2 {
		t.Fatalf("concurrent enrollment: %d", total.Load())
	}
	if n, err := db.ProcessCampaigns(ctx, owners, now); err != nil || n != 0 {
		t.Fatalf("daily cap ignored: %d %v", n, err)
	}
	p, err = db.CampaignPreview(ctx, c.ID)
	if err != nil || p.Pending != 2 || p.Enrolled != 2 {
		t.Fatalf("batch %+v %v", p, err)
	}
	if err = db.SetCampaignEnabled(ctx, c.ID, p.Campaign.Revision, 100, false); err != nil {
		t.Fatal(err)
	}
	p, err = db.CampaignPreview(ctx, c.ID)
	if err != nil || p.Pending != 0 || p.Skipped != 2 {
		t.Fatalf("pause %+v %v", p, err)
	}
	if _, err = db.ClaimBroadcast(ctx); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("paused delivery claimed", err)
	}
	if err = db.SetCampaignEnabled(ctx, c.ID, c.Revision, 100, true); !errors.Is(err, storage.ErrCampaignChanged) {
		t.Fatal("stale preview accepted", err)
	}
}

func TestCampaignsShareRecipientCooldown(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	campaignTrial(t, db, 811)
	a, now := campaignFixture(t, db, 100)
	b, _ := campaignFixture(t, db, 100)
	for _, c := range []storage.Campaign{a, b} {
		if err := db.SetCampaignEnabled(ctx, c.ID, c.Revision, 100, true); err != nil {
			t.Fatal(err)
		}
	}
	var total atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Go(func() {
			n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now)
			if err != nil {
				t.Error(err)
			}
			total.Add(int32(n))
		})
	}
	group.Wait()
	if total.Load() != 1 {
		t.Fatalf("global cap bypassed: %d", total.Load())
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE campaign_contacts SET next_allowed_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now)
	if err != nil || n != 1 {
		t.Fatalf("other campaign after cooldown: %d %v", n, err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE campaign_contacts SET next_allowed_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	n, err = db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now)
	if err != nil || n != 0 {
		t.Fatalf("once-per-campaign bypassed: %d %v", n, err)
	}
}

func TestCampaignRechecksOptOutAndPaidOrdersBeforeDelivery(t *testing.T) {
	for _, change := range []string{"optout", "paid"} {
		t.Run(change, func(t *testing.T) {
			db := testdb.Open(t)
			ctx := context.Background()
			s := campaignTrial(t, db, 821)
			c, now := campaignFixture(t, db, 100)
			if err := db.SetCampaignEnabled(ctx, c.ID, c.Revision, 100, true); err != nil {
				t.Fatal(err)
			}
			if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
				t.Fatalf("enrollment %d %v", n, err)
			}
			d, err := db.ClaimBroadcast(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if yes, err := db.BroadcastDeliveryAllowed(ctx, d); err != nil || !yes {
				t.Fatalf("initial eligibility %v %v", yes, err)
			}
			if change == "optout" {
				err = db.SaveNotificationSettings(ctx, 821, storage.NotificationSettings{Activity: false})
			} else {
				_, err = db.CreateOrder(ctx, domain.Order{OrderID: "paid-after-enrollment", AccountID: &s.ID, TelegramID: s.TelegramID, Kind: domain.KindSubscription, Provider: "wata", PlanID: "30d", Amount: 10000, Currency: "RUB", Status: domain.OrderPaid})
			}
			if err != nil {
				t.Fatal(err)
			}
			if yes, err := db.BroadcastDeliveryAllowed(ctx, d); err != nil || yes {
				t.Fatalf("stale eligibility %v %v", yes, err)
			}
			if err = db.FinishBroadcastDelivery(ctx, d, "skipped", 0); err != nil {
				t.Fatal(err)
			}
			p, err := db.CampaignPreview(ctx, c.ID)
			if err != nil || p.Skipped != 1 {
				t.Fatalf("skip history %+v %v", p, err)
			}
		})
	}
}

func TestCampaignQuietHoursAndRevokedAuthor(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	campaignTrial(t, db, 831)
	if err := db.AddStaff(ctx, 201, "admin", "Author", 100); err != nil {
		t.Fatal(err)
	}
	c, now := campaignFixture(t, db, 201)
	if err := db.SetCampaignEnabled(ctx, c.ID, c.Revision, 201, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, nil, now.Add(-8*time.Hour)); err != nil || n != 0 {
		t.Fatalf("quiet hours ignored: %d %v", n, err)
	}
	if n, err := db.ProcessCampaigns(ctx, nil, now); err != nil || n != 1 {
		t.Fatalf("send hours %d %v", n, err)
	}
	if _, err := db.RemoveStaff(ctx, 201); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ProcessCampaigns(ctx, nil, now); err != nil {
		t.Fatal(err)
	}
	p, err := db.CampaignPreview(ctx, c.ID)
	if err != nil || p.Campaign.Enabled || p.Pending != 0 || p.Skipped != 1 || p.Campaign.LastError == "" {
		t.Fatalf("revocation %+v %v", p, err)
	}
}

func TestCampaignInvalidPromoPausesWithoutBlockingOthers(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	campaignTrial(t, db, 841)
	c, now := campaignFixture(t, db, 100)
	c.PromoCode = "MISSING"
	c, err := db.SaveCampaign(ctx, c, 100)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := campaignFixture(t, db, 100)
	for _, item := range []storage.Campaign{c, other} {
		if err = db.SetCampaignEnabled(ctx, item.ID, item.Revision, 100, true); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("unrelated campaign blocked: %d %v", n, err)
	}
	p, err := db.CampaignPreview(ctx, c.ID)
	if err != nil || p.Campaign.Enabled || p.Campaign.LastError == "" {
		t.Fatalf("invalid promo not paused %+v %v", p, err)
	}
}

func TestEnrolledCampaignWaitsForNextSendingWindow(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	campaignTrial(t, db, 851)
	c, now := campaignFixture(t, db, 100)
	if err := db.SetCampaignEnabled(ctx, c.ID, c.Revision, 100, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("enroll %d %v", n, err)
	}
	d, err := db.ClaimBroadcast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	late := now.Add(10 * time.Hour)
	allowed, delay, err := db.BroadcastDeliveryReady(ctx, d, late)
	if err != nil || allowed || delay != 11*time.Hour {
		t.Fatalf("quiet window: %v %v %v", allowed, delay, err)
	}
	allowed, delay, err = db.BroadcastDeliveryReady(ctx, d, now)
	if err != nil || !allowed || delay != 0 {
		t.Fatalf("open window: %v %v %v", allowed, delay, err)
	}
	if err = db.FinishBroadcastDelivery(ctx, d, "pending", time.Hour); err != nil {
		t.Fatal(err)
	}
	p, err := db.CampaignPreview(ctx, c.ID)
	if err != nil || p.Pending != 1 || p.Skipped != 0 {
		t.Fatalf("delayed message lost %+v %v", p, err)
	}
}

func TestCampaignRepeatRequiresItsCooldownAndRespectsFutureStart(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	campaignTrial(t, db, 861)
	c, now := campaignFixture(t, db, 100)
	c.CooldownDays = 7
	c.StartsAt = now.Add(24 * time.Hour)
	var err error
	c, err = db.SaveCampaign(ctx, c, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetCampaignEnabled(ctx, c.ID, c.Revision, 100, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 0 {
		t.Fatalf("future start ignored: %d %v", n, err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE campaigns SET starts_at=now()-interval '3 days' WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("first send %d %v", n, err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE campaign_contacts SET next_allowed_at=now()-interval '1 second'; UPDATE broadcasts SET created_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 0 {
		t.Fatalf("cooldown bypass: %d %v", n, err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE campaign_enrollments SET last_enrolled_at=now()-interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("repeat after cooldown %d %v", n, err)
	}
}

func TestDelayedCampaignQueueCannotBypassDeliveryLimit(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	campaignTrial(t, db, 871)
	campaignTrial(t, db, 872)
	c, now := campaignFixture(t, db, 100)
	c.DailyLimit = 1
	var err error
	c, err = db.SaveCampaign(ctx, c, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetCampaignEnabled(ctx, c.ID, c.Revision, 100, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("first enrollment %d %v", n, err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE broadcasts SET created_at=now()-interval '2 days' WHERE campaign_id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("next enrollment %d %v", n, err)
	}
	first, err := db.ClaimBroadcast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, wait, err := db.BroadcastDeliveryReady(ctx, first, now); err != nil || !allowed || wait != 0 {
		t.Fatalf("first delivery %v %v %v", allowed, wait, err)
	}
	if err = db.FinishBroadcastDelivery(ctx, first, "sent", 0); err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimBroadcast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, wait, err := db.BroadcastDeliveryReady(ctx, second, now); err != nil || allowed || wait <= 0 {
		t.Fatalf("delivery cap bypass %v %v %v", allowed, wait, err)
	}
}

func TestDifferentCampaignsCannotDeliverTogetherAfterQueueDelay(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	campaignTrial(t, db, 881)
	a, now := campaignFixture(t, db, 100)
	if err := db.SetCampaignEnabled(ctx, a.ID, a.Revision, 100, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("first campaign %d %v", n, err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE campaign_contacts SET next_allowed_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	b, _ := campaignFixture(t, db, 100)
	if err := db.SetCampaignEnabled(ctx, b.ID, b.Revision, 100, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, now); err != nil || n != 1 {
		t.Fatalf("second campaign %d %v", n, err)
	}
	first, err := db.ClaimBroadcast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, wait, err := db.BroadcastDeliveryReady(ctx, first, now); err != nil || !allowed || wait != 0 {
		t.Fatalf("first delivery %v %v %v", allowed, wait, err)
	}
	if err = db.FinishBroadcastDelivery(ctx, first, "sent", 0); err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimBroadcast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, wait, err := db.BroadcastDeliveryReady(ctx, second, now); err != nil || allowed || wait <= 0 {
		t.Fatalf("recipient daily cap bypass %v %v %v", allowed, wait, err)
	}
}
