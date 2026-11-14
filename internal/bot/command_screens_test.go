package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestStartAndMenuKeepHTMLOnNewDocumentRefusal(t *testing.T) {
	for _, command := range []string{"start", "menu"} {
		t.Run(command, func(t *testing.T) {
			db := testdb.Open(t)
			base := context.Background()
			ctx := (&telegohandler.Context{}).WithContext(base)
			caller := &refusedWidgetCaller{}
			api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
			cfg := &config.Settings{BrandName: "Vivid"}
			registry, _ := payments.BuildRegistry(cfg, nil)
			b := New(api, db, cfg, payments.New(db, journeyPanel{}, registry, cfg))
			_ = db.SetBotView(base, 7, "cards")
			old, _ := prepareCard(b.connectPickerCard())
			if err := db.SaveBotCard(base, 7, 42, old); err != nil {
				t.Fatal(err)
			}
			message := telego.Message{MessageID: 99, Chat: telego.Chat{ID: 7, Type: "private"}, From: &telego.User{ID: 7}, Text: "/" + command}
			var err error
			if command == "start" {
				err = b.handleStart(ctx, message)
			} else {
				err = b.handleMenuCommand(ctx, message)
			}
			if err != nil {
				t.Fatal(err)
			}
			var reply struct {
				Text  string                  `json:"text"`
				Reply *telego.ReplyParameters `json:"reply_parameters"`
			}
			_ = json.Unmarshal([]byte(caller.bodies[len(caller.bodies)-1]), &reply)
			if reply.Reply == nil || reply.Reply.MessageID != 42 || reply.Text != "HTML-кабинет обновлён." {
				t.Fatalf("command did not point to its actual HTML screen: %+v", reply)
			}
			id, raw, _ := db.BotCard(base, 7)
			var state cardState
			_ = json.Unmarshal(raw, &state)
			if id != 42 || state.Pages[0].Tab != "subscription" || b.viewMode(base, 7) != "cards" {
				t.Fatal("HTML was replaced by the ordinary mode")
			}
			if strings.Contains(strings.Join(caller.methods, ","), "deleteMessage") {
				t.Fatal("working HTML deleted after a rejected new send")
			}
		})
	}
}

func TestFreshWidgetReplacesPreviousOnlyAfterSuccess(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{}, nil)
	_ = db.SetBotView(ctx, 7, "cards")
	old, _ := prepareCard(b.connectPickerCard())
	_ = db.SaveBotCard(ctx, 7, 42, old)
	b.showCommandScreen(ctx, telego.ChatID{ID: 7}, "connect", 7)
	if strings.Join(caller.methods, ",") != "sendWidget,deleteMessage" {
		t.Fatalf("unsafe replacement order: %v", caller.methods)
	}
}

func TestOrdinaryMainUsesDesignedRichDocumentAndNoWidget(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BrandName: "Vivid", RichMessages: true, DeviceLimitEnabled: true}
	registry, _ := payments.BuildRegistry(cfg, nil)
	b := New(api, db, cfg, payments.New(db, journeyPanel{}, registry, cfg))
	stub, request := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
	b.rich = stub.rich
	_ = db.SetBotView(ctx, 7, "classic")
	b.showCommandScreen(ctx, telego.ChatID{ID: 7}, "main", 7)
	for _, fragment := range []string{"<h3>Vivid</h3>", "<table compact>", "Оплачено до", "Устройства", "nav:connect", "nav:profile", "nav:support", "style=\"primary\""} {
		if !strings.Contains(request.RichMessage.HTML, fragment) {
			t.Errorf("rich main lacks %s", fragment)
		}
	}
	if strings.Contains(request.RichMessage.HTML, "https://example.com/private") {
		t.Fatal("connection secret unnecessarily printed on home")
	}
	if len(caller.methods) != 0 {
		t.Fatalf("ordinary mode sent native widgets or duplicate buttons: %v", caller.methods)
	}
}

func TestRichCardDocumentEscapesContentAndKeepsDevicePaging(t *testing.T) {
	state := cardWithButtons(cardPage{Title: "Устройства", Body: "Одна проверка\n<unsafe>", Stats: []cardStat{{Label: "Устройства", Value: "4 / 2", Warn: true}}}, []telego.InlineKeyboardButton{button("Открыть", "card:device:full-id")})
	state.Pages = append(state.Pages, state.Pages[0])
	state.Current = 1
	html, kb := richCardDocument(state)
	if strings.Contains(html, "<unsafe>") || !strings.Contains(html, "&lt;unsafe&gt;") || !strings.Contains(html, "<blockquote>") || !strings.Contains(html, "Страница 2 из 2") {
		t.Fatal("unsafe or incomplete rich document")
	}
	if kb.InlineKeyboard[len(kb.InlineKeyboard)-2][0].CallbackData != "devices:page:0" {
		t.Fatal("device paging lost")
	}
}
