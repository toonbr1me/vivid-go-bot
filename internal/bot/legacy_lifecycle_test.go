package bot

import (
	"context"
	"fmt"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
	"strings"
	"testing"
	"time"
)

type lifecyclePanel struct {
	journeyPanel
	user   *remnawave.User
	writes int
}

func (p *lifecyclePanel) UsersByTelegramID(context.Context, int64) ([]remnawave.User, error) {
	if p.user == nil {
		return nil, nil
	}
	return []remnawave.User{*p.user}, nil
}
func (p *lifecyclePanel) UserByUsername(context.Context, string) (*remnawave.User, error) {
	return p.user, nil
}
func (p *lifecyclePanel) CreateUser(_ context.Context, v remnawave.CreateUserParams) (remnawave.User, error) {
	p.writes++
	p.user = &remnawave.User{ID: 1, Username: v.Username, Status: "ACTIVE", ExpireAt: remnawave.Timestamp{Time: v.ExpireAt}, SubscriptionURL: "https://example.com/private"}
	return *p.user, nil
}
func (p *lifecyclePanel) UpdateExpireAt(_ context.Context, _ int64, v time.Time, _ []string) (remnawave.User, error) {
	p.writes++
	p.user.ExpireAt = remnawave.Timestamp{Time: v}
	return *p.user, nil
}

// Exercises actual bot handlers, transactional purchases, provisioning journal,
// support storage and both legacy transports. Only external APIs are replaced.
func TestLegacyPurchaseConnectionSupportAndRestartJourney(t *testing.T) {
	for _, rich := range []bool{false, true} {
		t.Run(fmt.Sprintf("rich=%t", rich), func(t *testing.T) {
			db := testdb.Open(t)
			ctx := (&telegohandler.Context{}).WithContext(context.Background())
			caller := &scriptedCaller{}
			api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Settings{BrandName: "Vivid", BotStyle: "legacy", BotCleanChat: true, RichMessages: rich, Currency: "RUB", MinPaymentAmount: 100, SiteEnabled: true, SiteBaseURL: "https://example.com"}
			registry, _ := payments.BuildRegistry(cfg, nil)
			makeBot := func(panel payments.Panel) *Bot {
				b := New(api, db, cfg, payments.New(db, panel, registry, cfg))
				if rich {
					stub, _ := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
					b.rich = stub.rich
				}
				return b
			}
			b := makeBot(nil)
			subject, err := db.EnsureTelegramSubject(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true}); err != nil {
				t.Fatal(err)
			}
			if _, err = db.AddBalance(ctx, subject.ID, 30000, nil); err != nil {
				t.Fatal(err)
			}
			incoming := telego.Message{MessageID: 99, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: "/start"}
			if err = b.cleanCommand(b.handleStart)(ctx, incoming); err != nil {
				t.Fatal(err)
			}
			b.startBalanceCheckout(ctx, "buy", telego.ChatID{ID: 7}, 1, 7, "30d")
			orders, err := db.OrdersForTelegram(ctx, 7, 10)
			if err != nil || len(orders) != 1 || orders[0].Status != domain.OrderPaid {
				t.Fatalf("payment lost: %+v %v", orders, err)
			}
			order := orders[0]
			text, kb := b.orderScreen(ctx, 7, order)
			if strings.Contains(text, "доступ выдан") || strings.Contains(text, "Всё готово") || !strings.Contains(fmt.Sprint(kb), "support:order:") {
				t.Fatal("paid order presented as ready or lacks contextual help")
			}
			panel := &lifecyclePanel{}
			b = makeBot(panel)
			b.checkOrder(ctx, "foreign", telego.ChatID{ID: 8}, 1, 8, order.OrderID)
			if panel.writes != 0 {
				t.Fatal("foreign callback provisioned order")
			}
			b.checkOrder(ctx, "check", telego.ChatID{ID: 7}, 1, 7, order.OrderID)
			ready, err := db.OrderByID(ctx, order.OrderID)
			if err != nil || ready.Status != domain.OrderProvisioned || panel.writes != 1 {
				t.Fatalf("restart did not recover purchase: %+v %v writes=%d", ready, err, panel.writes)
			}
			b = makeBot(panel)
			b.checkOrder(ctx, "replay", telego.ChatID{ID: 7}, 1, 7, order.OrderID)
			if panel.writes != 1 {
				t.Fatal("callback replay granted another period")
			}
			balance, _ := db.Balance(ctx, subject.ID)
			if balance != 20000 {
				t.Fatal("payment charged twice")
			}
			hub := b.connectHubCard(ctx, 7)
			if len(hub.Actions) < 7 {
				t.Fatal("connection hub missing apps")
			}
			for i, app := range []string{"happ", "incy", "clashmi", "flclashx"} {
				if !strings.HasPrefix(hub.Actions[i].URL, "https://example.com/app-import/"+app+"?u=") {
					t.Fatalf("missing direct import: %s", app)
				}
			}
			if err = b.deliverJourney(ctx, 7, 1, hub); err != nil {
				t.Fatal(err)
			}
			if err = b.handleCallback(ctx, telego.CallbackQuery{ID: "help", From: telego.User{ID: 7}, Data: "support:order:" + order.OrderID}); err != nil {
				t.Fatal(err)
			}
			incoming.Text = ""
			incoming.Caption = "Не подключается"
			incoming.Photo = []telego.PhotoSize{{FileID: "private-photo"}}
			if err = b.handleSupportAttachment(ctx, incoming); err != nil {
				t.Fatal(err)
			}
			tickets, _ := db.TicketsForSubject(ctx, subject.ID, 10)
			if len(tickets) != 1 || !strings.Contains(tickets[0].Context, order.OrderID) {
				t.Fatal("order context lost")
			}
			ticket := tickets[0]
			messages, _ := db.TicketMessages(ctx, ticket.ID, 10)
			if len(messages) != 1 || messages[0].FileID == nil {
				t.Fatal("attachment lost during clean chat")
			}
			if _, err = db.AddTicketMessage(ctx, ticket.ID, subject.ID, nil, "Обновите подписку", true, "", ""); err != nil {
				t.Fatal(err)
			}
			b = makeBot(panel)
			ticket, _ = db.TicketByID(ctx, ticket.ID)
			if !ticket.Unread() {
				t.Fatal("new answer indicator lost on restart")
			}
			b.ticketCallback(ctx, "open", telego.ChatID{ID: 7}, 1, 7, fmt.Sprint(ticket.ID), "")
			ticket, _ = db.TicketByID(ctx, ticket.ID)
			if ticket.Unread() {
				t.Fatal("opened ticket remains unread")
			}
			b.ticketCallback(ctx, "resolve", telego.ChatID{ID: 7}, 1, 7, "resolve", fmt.Sprint(ticket.ID))
			b.setInput(7, inputTicket, fmt.Sprint(ticket.ID))
			incoming.Text = "stale reply"
			incoming.Photo = nil
			incoming.Caption = ""
			if err = b.handleText(ctx, incoming); err != nil {
				t.Fatal(err)
			}
			ticket, _ = db.TicketByID(ctx, ticket.ID)
			messages, _ = db.TicketMessages(ctx, ticket.ID, 20)
			if ticket.Status != "closed" || len(messages) != 2 {
				t.Fatal("stale reply reopened ticket")
			}
			for _, method := range caller.methods {
				if strings.Contains(strings.ToLower(method), "widget") {
					t.Fatalf("legacy called disabled widget API: %s", method)
				}
			}
		})
	}
}

func TestSupportAppAndDeviceSelectionPersistsContext(t *testing.T) {
	db := testdb.Open(t)
	ctx := (&telegohandler.Context{}).WithContext(context.Background())
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BotStyle: "legacy", BotCleanChat: true}
	registry, _ := payments.BuildRegistry(cfg, nil)
	b := New(api, db, cfg, payments.New(db, journeyPanel{}, registry, cfg))
	if err := b.handleCallback(ctx, telego.CallbackQuery{ID: "app", From: telego.User{ID: 7}, Data: "support:app:happ"}); err != nil {
		t.Fatal(err)
	}
	if err := b.handleCallback(ctx, telego.CallbackQuery{ID: "device", From: telego.User{ID: 7}, Data: "support:device:happ,android"}); err != nil {
		t.Fatal(err)
	}
	if err := b.handleText(ctx, telego.Message{MessageID: 99, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: "Проблема подключения"}); err != nil {
		t.Fatal(err)
	}
	subject, _ := db.EnsureTelegramSubject(ctx, 7)
	tickets, _ := db.TicketsForSubject(ctx, subject.ID, 10)
	if len(tickets) != 1 || !strings.Contains(tickets[0].Context, "Happ") || !strings.Contains(tickets[0].Context, "Android") {
		t.Fatal("selection context lost")
	}
}
