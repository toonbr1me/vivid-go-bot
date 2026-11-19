package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestSupportContactAndAllTopicsRemainReachable(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, subject.ID, subject.TelegramID, "Существующее обращение", "Не подключается", storage.SupportModeHuman)
	if err != nil {
		t.Fatal(err)
	}
	b := New(nil, db, &config.Settings{BrandName: "Vivid", AdminIDs: map[int64]bool{7: true}}, nil)
	assertKeyboard := func(kb *telego.InlineKeyboardMarkup) {
		t.Helper()
		if kb == nil || len(kb.InlineKeyboard) == 0 || len(kb.InlineKeyboard[0]) != 1 {
			t.Fatal("missing contact row", kb)
		}
		first := kb.InlineKeyboard[0][0]
		if first.CallbackData != "support:new" || first.Text != "Связаться с поддержкой" || first.Style != "primary" {
			t.Fatal("contact is not first", first)
		}
		available := map[string]bool{}
		for _, row := range kb.InlineKeyboard {
			for _, action := range row {
				available[action.CallbackData] = true
			}
		}
		for _, action := range []string{"support:list", fmt.Sprintf("ticket:%d", ticket.ID), "nav:diagnostics", "nav:connect", "admin:tickets"} {
			if !available[action] {
				t.Fatal("support action hidden", action)
			}
		}
		for _, topic := range domain.SupportTopics() {
			if !available["support:topic:"+topic.Key] {
				t.Fatal("support topic hidden", topic.Key)
			}
		}
	}
	_, rich, ok := b.standardRichScreen(ctx, 7, "support")
	if !ok {
		t.Fatal("rich support is unavailable")
	}
	assertKeyboard(rich)
	_, fallback := b.navigationScreen(ctx, 7, "support")
	assertKeyboard(fallback)
	card, ok := b.interfaceScreen(ctx, 7, "support")
	if !ok || len(card.Pages) == 0 || len(card.Pages[0].Buttons) == 0 {
		t.Fatal("widget support is unavailable")
	}
	first := card.Actions[card.Pages[0].Buttons[0].Index]
	if first.CallbackData != "support:new" {
		t.Fatal("widget hides direct contact", first)
	}
}

func TestDirectSupportContactDoesNotRequireATopic(t *testing.T) {
	db := testdb.Open(t)
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{BrandName: "Vivid", BotStyle: "legacy"}, nil)
	ctx := (&telegohandler.Context{}).WithContext(context.Background())
	if err := b.handleCallback(ctx, telego.CallbackQuery{ID: "contact", From: telego.User{ID: 7}, Data: "support:new"}); err != nil {
		t.Fatal(err)
	}
	kind, ref := b.input(7)
	if kind != inputTicket || ref != "" {
		t.Fatal("contact requires a topic or another step", kind, ref)
	}
}
