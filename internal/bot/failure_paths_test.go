package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

type failureCaller struct {
	scriptedCaller
	reject map[string]bool
	before func(string) error
}

func (c *failureCaller) Call(ctx context.Context, url string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
	method := url[strings.LastIndex(url, "/")+1:]
	response, err := c.scriptedCaller.Call(ctx, url, data)
	if c.before != nil {
		if err := c.before(method); err != nil {
			return nil, err
		}
	}
	if c.reject[method] {
		return &telegoapi.Response{Ok: false, Error: &telegoapi.Error{ErrorCode: 400, Description: func() string {
			if method == "sendWidget" {
				return "INPUT_MEDIA_WIDGET_NOT_ALLOWED"
			}
			return "simulated Telegram refusal"
		}()}}, nil
	}
	return response, err
}

func TestCleanCommandDeletesOnlyAfterSuccessfulDelivery(t *testing.T) {
	for _, scenario := range []string{"success", "handler_error", "no_delivery", "telegram_refused", "group_chat", "cleanup_disabled"} {
		t.Run(scenario, func(t *testing.T) {
			caller := &failureCaller{reject: map[string]bool{}}
			api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
			b := New(api, nil, &config.Settings{BotStyle: "legacy", BotCleanChat: scenario != "cleanup_disabled"}, nil)
			message := telego.Message{MessageID: 99, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: "/start"}
			if scenario == "group_chat" {
				message.Chat.ID = -100
				message.Chat.Type = "group"
			}
			if scenario == "telegram_refused" {
				caller.reject["sendMessage"] = true
			}
			ctx := (&telegohandler.Context{}).WithContext(context.Background())
			_ = b.cleanCommand(func(ctx *telegohandler.Context, m telego.Message) error {
				if scenario == "handler_error" {
					return errors.New("handler failed")
				}
				if scenario == "no_delivery" {
					return nil
				}
				return b.deliver(ctx, telego.ChatID{ID: m.Chat.ID}, 0, "Готово", nil)
			})(ctx, message)
			deleted := strings.Contains(strings.Join(caller.methods, ","), "deleteMessage")
			if deleted != (scenario == "success") {
				t.Fatalf("unsafe command deletion: %v", caller.methods)
			}
			if deleted && strings.Join(caller.methods, ",") != "sendMessage,deleteMessage" {
				t.Fatal("incoming message deleted before response")
			}
		})
	}
}

func TestWidgetProductionRefusalKeepsOldCardAndIncomingCommand(t *testing.T) {
	db := testdb.Open(t)
	base := context.Background()
	caller := &failureCaller{reject: map[string]bool{"sendWidget": true, "editMessageWidgetData": true}}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BrandName: "Vivid", BotStyle: "html", BotCleanChat: true}
	registry, _ := payments.BuildRegistry(cfg, nil)
	b := New(api, db, cfg, payments.New(db, journeyPanel{}, registry, cfg))
	old, _ := prepareCard(b.connectPickerCard())
	if err := db.SaveBotCard(base, 7, 42, old); err != nil {
		t.Fatal(err)
	}
	ctx := (&telegohandler.Context{}).WithContext(base)
	_ = b.cleanCommand(b.handleStart)(ctx, telego.Message{MessageID: 99, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: "/start"})
	id, _, err := db.BotCard(base, 7)
	if err != nil || id != 42 {
		t.Fatal("refused replacement lost the old screen")
	}
	if strings.Contains(strings.Join(caller.methods, ","), "deleteMessage") {
		t.Fatal("working screen or incoming command deleted after failed widget delivery")
	}
	if strings.Contains(strings.Join(caller.methods, ","), "pinChatMessage") {
		t.Fatal("production start still sent interface selector")
	}
}

func TestLegacyConfiguredStartDoesNotCallWidgetAPI(t *testing.T) {
	db := testdb.Open(t)
	ctx := (&telegohandler.Context{}).WithContext(context.Background())
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BrandName: "Vivid", BotStyle: "legacy", BotCleanChat: true}
	registry, _ := payments.BuildRegistry(cfg, nil)
	b := New(api, db, cfg, payments.New(db, journeyPanel{}, registry, cfg))
	_ = db.SetBotView(ctx, 7, "cards")
	_ = b.cleanCommand(b.handleStart)(ctx, telego.Message{MessageID: 99, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: "/start"})
	methods := strings.Join(caller.methods, ",")
	if strings.Contains(methods, "Widget") || strings.Contains(methods, "pinChatMessage") || !strings.Contains(methods, "sendMessage") {
		t.Fatalf("legacy start uses unavailable widget API: %s", methods)
	}
}

func TestSupportWriteFailureDoesNotDeleteIncomingText(t *testing.T) {
	db := testdb.Open(t)
	ctx := (&telegohandler.Context{}).WithContext(context.Background())
	_, err := db.Pool.Exec(ctx, `CREATE FUNCTION reject_support_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated message write failure'; END $$; CREATE TRIGGER reject_support_write BEFORE INSERT ON support_messages FOR EACH ROW EXECUTE FUNCTION reject_support_write()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_support_write ON support_messages; DROP FUNCTION IF EXISTS reject_support_write()`)
	})
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BotStyle: "legacy", BotCleanChat: true}
	registry, _ := payments.BuildRegistry(cfg, nil)
	b := New(api, db, cfg, payments.New(db, nil, registry, cfg))
	b.setInput(7, inputTicket, "")
	_ = b.handleText(ctx, telego.Message{MessageID: 99, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: "Нельзя потерять это сообщение"})
	if strings.Contains(strings.Join(caller.methods, ","), "deleteMessage") {
		t.Fatal("unsaved support message was deleted")
	}
}

func TestTicketCardRejectsForeignOwnerAndKeepsLongHistory(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	cfg := &config.Settings{BotStyle: "html"}
	b := New(nil, db, cfg, nil)
	owner, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, owner.ID, ptrBotID(7), "Подключение", "Первое сообщение", "")
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("Длинное сообщение поддержки. ", 30)
	if _, err = db.AddTicketMessage(ctx, ticket.ID, owner.ID, ptrBotID(7), long, true, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = b.ticketCard(ctx, 8, ticket.ID); err == nil {
		t.Fatal("foreign ticket was disclosed")
	}
	state, err := b.ticketCard(ctx, 7, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, p := range state.Pages {
		body.WriteString(strings.ReplaceAll(p.Body, " ", ""))
	}
	if !strings.Contains(body.String(), strings.ReplaceAll(strings.TrimSpace(long), " ", "")) {
		t.Fatal("pagination lost support content")
	}
	if state.Current != len(state.Pages)-1 {
		t.Fatal("history did not open at the latest message")
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE support_tickets SET status='closed' WHERE id=$1`, ticket.ID); err != nil {
		t.Fatal(err)
	}
	state, err = b.ticketCard(ctx, 7, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range state.Actions {
		if a.CallbackData == fmt.Sprintf("ticket:reply:%d", ticket.ID) {
			t.Fatal("closed ticket offered reply action")
		}
	}
}
func ptrBotID(id int64) *int64 { return &id }
