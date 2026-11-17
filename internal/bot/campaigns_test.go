package bot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestCampaignWorkerSkipsChangedRecipientsAndSurvivesRestart(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subjects := map[int64]domain.Subject{}
	for _, id := range []int64{891, 892, 893} {
		s, err := db.EnsureTelegramSubject(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		subjects[id] = s
		if _, err = db.CreateOrder(ctx, domain.Order{OrderID: fmt.Sprintf("campaign-trial-%d", id), Provider: "trial", AccountID: &s.ID, TelegramID: &id, PlanID: "trial_bot", Kind: domain.KindSubscription, Status: domain.OrderProvisioned, Currency: "RUB"}); err != nil {
			t.Fatal(err)
		}
	}
	campaign, err := db.SaveCampaign(ctx, storage.Campaign{Title: "Trial conversion", Audience: "trial_only", Text: "Subscription offer", DailyLimit: 10, SendFrom: 0, SendTo: 24, StartsAt: time.Now().Add(-time.Hour)}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetCampaignEnabled(ctx, campaign.ID, campaign.Revision, 100, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.ProcessCampaigns(ctx, map[int64]bool{100: true}, time.Now()); err != nil || n != 3 {
		t.Fatalf("enrolled %d %v", n, err)
	}
	if err = db.SaveNotificationSettings(ctx, 891, storage.NotificationSettings{Activity: false}); err != nil {
		t.Fatal(err)
	}
	paid := subjects[892]
	if _, err = db.CreateOrder(ctx, domain.Order{OrderID: "campaign-paid", Provider: "wata", AccountID: &paid.ID, TelegramID: paid.TelegramID, PlanID: "30d", Kind: domain.KindSubscription, Status: domain.OrderPaid, Amount: 10000, Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{BotStyle: "legacy", AdminIDs: map[int64]bool{100: true}}
	for i := 0; i < 2; i++ {
		worker := New(api, db, cfg, nil)
		if err = worker.ProcessBroadcasts(ctx, 20); err != nil {
			t.Fatal(err)
		}
	}
	if len(caller.methods) != 1 || caller.methods[0] != "sendMessage" {
		t.Fatalf("ineligible recipient contacted or duplicate: %v", caller.methods)
	}
	preview, err := db.CampaignPreview(ctx, campaign.ID)
	if err != nil || preview.Sent != 1 || preview.Skipped != 2 || preview.Pending != 0 {
		t.Fatalf("delivery history %+v %v", preview, err)
	}
}
