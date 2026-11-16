package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
	"strings"
	"testing"
	"time"
)

func TestBroadcastTextAndDeliveryClassification(t *testing.T) {
	for _, tc := range []struct {
		raw, body, code string
		bad             bool
	}{
		{"hello promo=sale_20", "hello", "SALE_20", false}, {"promo=SALE\nhello", "hello", "SALE", false},
		{"<b>hello</b>", "<b>hello</b>", "", false}, {"promo=X", "", "", true}, {"promo=SALE", "", "", true},
		{"hi promo=ONE promo=TWO", "", "", true}, {strings.Repeat("🙂", 1501), "", "", true},
	} {
		body, code, err := parseBroadcast(tc.raw)
		if (err != nil) != tc.bad || !tc.bad && (body != tc.body || code != tc.code) {
			t.Fatalf("parse: %q %q %q %v", tc.raw, body, code, err)
		}
	}
	for _, tc := range []struct {
		err    error
		status string
		delay  time.Duration
	}{
		{nil, "sent", 0}, {errors.New("connection reset after write"), "uncertain", 0},
		{&telegoapi.Error{ErrorCode: 500}, "uncertain", 0}, {&telegoapi.Error{ErrorCode: 403}, "failed", 0},
		{&telegoapi.Error{ErrorCode: 429, Parameters: &telegoapi.ResponseParameters{RetryAfter: 60}}, "pending", time.Minute},
	} {
		status, delay := deliveryResult(tc.err)
		if status != tc.status || delay != tc.delay {
			t.Fatal("unsafe retry decision", status, delay)
		}
	}
}

func TestLegacyBroadcastWizardAndRestartWorker(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{BotStyle: "legacy", AdminIDs: map[int64]bool{100: true}, Currency: "RUB"}
	b := New(api, db, cfg, nil)
	for _, id := range []int64{501, 502} {
		if _, err := db.EnsureTelegramSubject(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	b.broadcastCallback(ctx, "query", telego.ChatID{ID: 100}, 0, 100, "audience", "all")
	if kind, ref := b.input(100); kind != inputBroadcast || ref != "all" {
		t.Fatal("wizard input missing", kind, ref)
	}
	b.handleBroadcastInput(ctx, telego.ChatID{ID: 100}, 100, "all", "Hello <friends>")
	drafts, err := db.RecentBroadcasts(ctx, 100)
	if err != nil || len(drafts) != 1 {
		t.Fatal("draft not saved", drafts, err)
	}
	id := drafts[0].ID
	if err := b.ProcessBroadcasts(ctx, 20); err != nil {
		t.Fatal(err)
	}
	x, err := db.Broadcast(ctx, id, 100)
	if err != nil || x.Status != "draft" || x.Sent != 0 {
		t.Fatal("draft delivered without confirmation", x, err)
	}
	b.broadcastCallback(ctx, "foreign", telego.ChatID{ID: 501}, 0, 501, "send", id)
	x, _ = db.Broadcast(ctx, id, 100)
	if x.Status != "draft" {
		t.Fatal("unauthorized confirmation")
	}
	b.broadcastCallback(ctx, "group", telego.ChatID{ID: -100}, 0, 100, "send", id)
	x, _ = db.Broadcast(ctx, id, 100)
	if x.Status != "draft" {
		t.Fatal("group confirmation")
	}
	b.broadcastCallback(ctx, "confirm", telego.ChatID{ID: 100}, 0, 100, "send", id)
	b.broadcastCallback(ctx, "replay", telego.ChatID{ID: 100}, 0, 100, "send", id)
	before := len(caller.bodies)
	restarted := New(api, db, cfg, nil)
	if err := restarted.ProcessBroadcasts(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if err := restarted.ProcessBroadcasts(ctx, 20); err != nil {
		t.Fatal(err)
	}
	x, err = db.Broadcast(ctx, id, 100)
	if err != nil || x.Sent != 2 || x.Status != "completed" {
		t.Fatal("restart lost or duplicated broadcast", x, err)
	}
	if len(caller.bodies)-before != 2 {
		t.Fatal("worker altered active screen or sent duplicates", caller.methods[before:])
	}
	for _, raw := range caller.bodies[before:] {
		var p map[string]any
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		if p["text"] != "Hello <friends>" || p["parse_mode"] != nil {
			t.Fatal("plain message mutated", raw)
		}
	}
	if strings.Contains(strings.Join(caller.methods, ","), "Widget") {
		t.Fatal("legacy used widget")
	}
}

func TestBroadcastRejectsInactivePromoAndRevokedSender(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BotStyle: "legacy", Currency: "RUB"}
	b := New(api, db, cfg, nil)
	if err := db.AddStaff(ctx, 100, storage.RoleOwner, "Owner", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureTelegramSubject(ctx, 501); err != nil {
		t.Fatal(err)
	}
	if err := db.CreatePromo(ctx, domain.PromoCode{Code: "SALE", DiscountPercent: 10, IsActive: false}); err != nil {
		t.Fatal(err)
	}
	b.handleBroadcastInput(ctx, telego.ChatID{ID: 100}, 100, "all", "hello promo=SALE")
	drafts, err := db.RecentBroadcasts(ctx, 100)
	if err != nil || len(drafts) != 0 {
		t.Fatal("inactive promo accepted")
	}
	draft, err := db.CreateBroadcast(ctx, 100, "all", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmBroadcast(ctx, draft.ID, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RemoveStaff(ctx, 100); err != nil {
		t.Fatal(err)
	}
	before := len(caller.methods)
	if err := b.ProcessBroadcasts(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if len(caller.methods) != before {
		t.Fatal("revoked staff sent broadcast")
	}
	x, err := db.Broadcast(ctx, draft.ID, 100)
	if err != nil || x.Status != "cancelled" || x.Failed != 1 {
		t.Fatal("revoked delivery state", x, err)
	}
}

func TestLegacyTicketStaffControlsAndCompensationHistory(t *testing.T) {
	for _, rich := range []bool{false, true} {
		t.Run(fmt.Sprint(rich), func(t *testing.T) {
			db := testdb.Open(t)
			ctx := context.Background()
			caller := &scriptedCaller{}
			api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
			cfg := &config.Settings{BotStyle: "legacy", Currency: "RUB", RichMessages: rich, SupportCompensationDailyDays: 3}
			registry, _ := payments.BuildRegistry(cfg, nil)
			svc := payments.New(db, &lifecyclePanel{}, registry, cfg)
			b := New(api, db, cfg, svc)
			if rich {
				stub, _ := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
				b.rich = stub.rich
			}
			s, err := db.EnsureTelegramSubject(ctx, 501)
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Issue", "Client question", "human")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AddStaff(ctx, 100, storage.RoleSupport, "Support", 1); err != nil {
				t.Fatal(err)
			}
			_, customerKB, err := b.ticketPageScreen(ctx, 501, ticket.ID, -1)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(fmt.Sprint(customerKB), "compensate") || strings.Contains(fmt.Sprint(customerKB), "ticket:close") {
				t.Fatal("customer has staff actions")
			}
			if _, _, err := b.ticketPageScreen(ctx, 502, ticket.ID, -1); err == nil {
				t.Fatal("foreign history visible")
			}
			text, staffKB, err := b.ticketPageScreen(ctx, 100, ticket.ID, -1)
			if err != nil || !strings.Contains(fmt.Sprint(staffKB), "ticket:compensate:") || !strings.Contains(text, "Клиент") {
				t.Fatal("missing staff controls or author", text, staffKB, err)
			}
			ref := fmt.Sprint(ticket.ID)
			b.ticketCallback(ctx, "comp", telego.ChatID{ID: 100}, 0, 100, "compensate", ref)
			b.ticketCallback(ctx, "repeat", telego.ChatID{ID: 100}, 0, 100, "compensate", ref)
			messages, err := db.TicketMessages(ctx, ticket.ID, 100)
			if err != nil || len(messages) != 2 || !strings.Contains(messages[0].Text, "компенсацию") {
				t.Fatal("compensation not recorded once", messages, err)
			}
			_, staffKB, err = b.ticketPageScreen(ctx, 100, ticket.ID, -1)
			if err != nil || strings.Contains(fmt.Sprint(staffKB), "compensate") {
				t.Fatal("compensation action still available")
			}
			if strings.Contains(strings.Join(caller.methods, ","), "Widget") {
				t.Fatal("legacy support used widgets")
			}
		})
	}
}

func TestPanelWorkerKeepsActiveLegacyScreenAndDeduplicates(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{BotStyle: "legacy"}, nil)
	if _, err := db.EnsureTelegramSubject(ctx, 501); err != nil {
		t.Fatal(err)
	}
	e := storage.PanelEvent{ID: "test-device", Event: "user_hwid_devices.added", TelegramID: 501, DeviceLabel: "Pixel · Android"}
	for i := 0; i < 2; i++ {
		if err := db.RecordPanelEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.ProcessPanelEvents(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if err := b.ProcessPanelEvents(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.methods, ",") != "sendMessage" || !strings.Contains(caller.bodies[0], "Pixel") {
		t.Fatal("notification replaced screen or duplicated", caller.methods)
	}
}

func TestLegacyFailedScreenPersistenceKeepsCommandAndPreviousInterface(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{BotStyle: "legacy", BotCleanChat: true}, nil)
	if err := db.SaveNativeMessage(ctx, 501, 42); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBotCard(ctx, 501, 43, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `CREATE FUNCTION reject_native_save() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated persistence failure'; END $$; CREATE TRIGGER reject_native_save BEFORE INSERT OR UPDATE ON bot_ui_preferences FOR EACH ROW EXECUTE FUNCTION reject_native_save()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_native_save ON bot_ui_preferences; DROP FUNCTION IF EXISTS reject_native_save()`)
	})
	message := telego.Message{MessageID: 99, From: &telego.User{ID: 501}, Chat: telego.Chat{ID: 501, Type: "private"}, Text: "/start"}
	handler := b.cleanCommand(func(ctx *telegohandler.Context, m telego.Message) error {
		ctx = ctx.WithContext(context.WithValue(ctx, newCardKey{}, true))
		b.send(ctx, telego.ChatID{ID: 501}, "New screen", nil)
		return nil
	})
	if err := handler((&telegohandler.Context{}).WithContext(ctx), message); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(caller.methods, ","), "deleteMessage") {
		t.Fatal("cleanup preceded persistent replacement", caller.methods)
	}
	if id := db.NativeMessageID(ctx, 501); id != 42 {
		t.Fatal("old screen lost", id)
	}
}
