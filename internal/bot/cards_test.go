package bot

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestInterfaceCardLimits(t *testing.T) {
	var compressed bytes.Buffer
	w, _ := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	_, _ = w.Write([]byte(interfaceWidgetHTML))
	_ = w.Close()
	if len(interfaceWidgetHTML) > 32768 || compressed.Len() > 4096 {
		t.Fatalf("widget exceeds Telegram limits: %d/%d", len(interfaceWidgetHTML), compressed.Len())
	}
	if strings.Contains(interfaceWidgetHTML, "innerHTML") || strings.Contains(interfaceWidgetHTML, "fetch(") {
		t.Fatal("unsafe client rendering")
	}
	var rows [][]telego.InlineKeyboardButton
	for i := 0; i < 19; i++ {
		rows = append(rows, []telego.InlineKeyboardButton{button(strings.Repeat("Устройство ", 10), fmt.Sprintf("nav:device%d", i))})
	}
	state := textCard("История\n"+strings.Repeat("Подписка оплачена. ", 100), keyboard(rows...))
	var restored strings.Builder
	for _, page := range state.Pages {
		page.Revision = "abcdef1234567"
		payload, _ := json.Marshal(page)
		if len(payload) > 1024 {
			t.Fatalf("page exceeds private state limit: %d", len(payload))
		}
		restored.WriteString(strings.ReplaceAll(page.Body, " ", ""))
		if len(page.Buttons) > 4 {
			t.Fatal("too many controls")
		}
	}
	if restored.String() != strings.ReplaceAll(strings.TrimSpace(strings.Repeat("Подписка оплачена. ", 100)), " ", "") {
		t.Fatal("pagination lost text")
	}
}

func TestCardDoesNotLoseLongSingleLineMessages(t *testing.T) {
	text := strings.Repeat("Ответ поддержки. ", 70)
	state := textCard(text, nil)
	var joined strings.Builder
	for _, page := range state.Pages {
		joined.WriteString(strings.ReplaceAll(page.Body, " ", ""))
	}
	if joined.String() != strings.ReplaceAll(strings.TrimSpace(text), " ", "") {
		t.Fatal("long first line was truncated")
	}
}

func TestInterfaceUpdatesSameMessageAfterRestart(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil)
	if err = db.SetBotView(ctx, 7, "cards"); err != nil {
		t.Fatal(err)
	}
	state := textCard("Подписка\nАктивна", keyboard([]telego.InlineKeyboardButton{button("Далее", "nav:more")}))
	if err = b.deliverUI(ctx, 7, 0, state); err != nil {
		t.Fatal(err)
	}
	id, _, err := db.BotCard(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	b = New(api, db, &config.Settings{}, nil)
	if err = b.deliverUI(ctx, 7, id, textCard("Устройства\nДва устройства", nil)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.methods, ",") != "sendWidget,editMessageWidgetData" {
		t.Fatalf("message was recreated: %v", caller.methods)
	}
	var request map[string]json.RawMessage
	_ = json.Unmarshal([]byte(caller.bodies[0]), &request)
	if _, exists := request["reply_markup"]; exists {
		t.Fatal("native keyboard leaked into new mode")
	}
	if _, exists := request["public_data"]; exists {
		t.Fatal("private data leaked into shared state")
	}
}

func TestInterfaceRejectsReplayedHiddenAndForeignActions(t *testing.T) {
	db := testdb.Open(t)
	base := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{}, nil)
	_ = db.SetBotView(base, 7, "cards")
	rows := [][]telego.InlineKeyboardButton{}
	for i := 0; i < 5; i++ {
		rows = append(rows, []telego.InlineKeyboardButton{copyButton(fmt.Sprintf("Copy %d", i), "private-link")})
	}
	if err := b.deliverUI(base, 7, 0, textCard("Ссылки", keyboard(rows...))); err != nil {
		t.Fatal(err)
	}
	id, raw, _ := db.BotCard(base, 7)
	var stored cardState
	_ = json.Unmarshal(raw, &stored)
	q := telego.WidgetQuery{ID: "query", From: telego.User{ID: 7}, Chat: &telego.Chat{ID: 7, Type: "private"}, MessageID: id, Type: &telego.WidgetQueryTypeData{Type: "data"}}
	ctx := (&telegohandler.Context{}).WithContext(base)
	for _, data := range []string{"ui:old:a:0", "ui:" + stored.Pages[0].Revision + ":a:4"} {
		q.Data = data
		if err := b.answerCardQuery(ctx, q); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(caller.bodies[len(caller.bodies)-1], "private-link") {
			t.Fatal("unauthorized action exposed private payload")
		}
	}
	q.Data = "ui:" + stored.Pages[0].Revision + ":a:0"
	q.Chat.ID = 8
	if err := b.answerCardQuery(ctx, q); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(caller.bodies[len(caller.bodies)-1], "private-link") {
		t.Fatal("foreign chat exposed private payload")
	}
	q.Chat.ID = 7
	q.Data = "ui:" + stored.Pages[0].Revision + ":p:1"
	if err := b.answerCardQuery(ctx, q); err != nil {
		t.Fatal(err)
	}
	before := len(caller.methods)
	if err := b.answerCardQuery(ctx, q); err != nil {
		t.Fatal(err)
	}
	if len(caller.methods) != before+1 || caller.methods[before] != "answerWidgetQuery" {
		t.Fatal("replayed request changed state")
	}
}
