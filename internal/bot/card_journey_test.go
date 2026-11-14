package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestPrivateLocalActionsAreSplitWithoutLosingPayloads(t *testing.T) {
	var actions []telego.InlineKeyboardButton
	for i := 0; i < 4; i++ {
		actions = append(actions, urlButton(fmt.Sprintf("Приложение %d", i), "https://example.com/"+strings.Repeat("x", 210)+fmt.Sprint(i)))
	}
	actions = append(actions, copyButton("Скопировать ссылку", "https://example.com/private-subscription"))
	state, err := prepareCard(cardWithButtons(cardPage{Title: "Подключение", Body: strings.Repeat("Инструкция ", 15)}, actions))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Pages) < 3 {
		t.Fatal("large local payloads were not paginated")
	}
	seen := map[int]bool{}
	for _, page := range state.Pages {
		payload, _ := json.Marshal(page)
		if len(payload) > 1024 {
			t.Fatalf("private data exceeds limit: %d", len(payload))
		}
		for _, control := range page.Buttons {
			if seen[control.Index] {
				t.Fatal("duplicated action")
			}
			seen[control.Index] = true
			if actions[control.Index].CopyText != nil {
				if control.Payload != "" || control.Kind != "" {
					t.Fatal("copy bypasses the reliable native handoff")
				}
			} else if control.Payload == "" || control.Kind != "u" {
				t.Fatal("link still needs an asynchronous query")
			}
		}
	}
	if len(seen) != len(actions) {
		t.Fatal("pagination lost an action")
	}
	again, err := prepareCard(state)
	if err != nil || len(again.Pages) != len(state.Pages) {
		t.Fatal("pagination is not stable across requests")
	}
}

func TestOldWidgetCodeIsReplacedOnce(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{BrandName: "Vivid"}, nil)
	_ = db.SetBotView(ctx, 7, "cards")
	_ = db.SaveBotCard(ctx, 7, 42, textCard("Старый интерфейс", nil))
	if err := b.deliverUI(ctx, 7, 42, textCard("Новый интерфейс", nil)); err != nil {
		t.Fatal(err)
	}
	id, _, _ := db.BotCard(ctx, 7)
	if err := b.deliverUI(ctx, 7, id, textCard("Следующий экран", nil)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.methods, ",") != "sendWidget,deleteMessage,editMessageWidgetData" {
		t.Fatalf("invalid code upgrade: %v", caller.methods)
	}
}

type journeyPanel struct{ widgetSnapshotPanel }

func (journeyPanel) UsersByTelegramID(context.Context, int64) ([]remnawave.User, error) {
	return []remnawave.User{{ID: 1, Status: "ACTIVE", ExpireAt: remnawave.Timestamp{Time: time.Now().Add(24 * time.Hour)}, SubscriptionURL: "https://example.com/private"}}, nil
}
func (journeyPanel) HWIDDevices(context.Context, int64) ([]remnawave.HWIDDevice, error) {
	var devices []remnawave.HWIDDevice
	for i := 0; i < 7; i++ {
		devices = append(devices, remnawave.HWIDDevice{HWID: fmt.Sprintf("full-hwid-%d", i), DeviceModel: "Pixel", Platform: "Android"})
	}
	return devices, nil
}

func TestDeviceDetailsReturnToTheirOriginalPage(t *testing.T) {
	db := testdb.Open(t)
	base := context.Background()
	ctx := (&telegohandler.Context{}).WithContext(base)
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{DeviceLimitEnabled: true}
	b := New(api, db, cfg, payments.New(db, journeyPanel{}, nil, cfg))
	_ = db.SetBotView(base, 7, "cards")
	state := b.deviceCard(base, 7)
	state.Current = 2
	if err := b.deliverUI(base, 7, 0, state); err != nil {
		t.Fatal(err)
	}
	id, _, _ := db.BotCard(base, 7)
	q := telego.WidgetQuery{From: telego.User{ID: 7}, MessageID: id}
	if err := b.cardAction(ctx, q, "card:device:full-hwid-6"); err != nil {
		t.Fatal(err)
	}
	_, raw, _ := db.BotCard(base, 7)
	var detail cardState
	_ = json.Unmarshal(raw, &detail)
	if detail.Actions[detail.Pages[0].Back-1].CallbackData != "card:devicespage:2" {
		t.Fatal("details lost the original page")
	}
	if detail.Actions[0].CallbackData != "card:confirm:full-hwid-6" {
		t.Fatal("first action detaches without confirmation")
	}
	if err := b.cardAction(ctx, q, "card:confirm:full-hwid-6"); err != nil {
		t.Fatal(err)
	}
	_, raw, _ = db.BotCard(base, 7)
	_ = json.Unmarshal(raw, &detail)
	if detail.Pages[0].Buttons[0].Style != "danger" {
		t.Fatal("destructive confirmation lost its warning style")
	}
	if err := b.cardAction(ctx, q, "card:devicespage:2"); err != nil {
		t.Fatal(err)
	}
	_, raw, _ = db.BotCard(base, 7)
	_ = json.Unmarshal(raw, &detail)
	if detail.Current != 2 {
		t.Fatal("cancel returned to the first page")
	}
	// The panel's removal method is deliberately unimplemented: these steps
	// must inspect and cancel only, never modify the device list.
}

func TestConnectionJourneyShowsOnlySelectedPlatform(t *testing.T) {
	b := &Bot{cfg: &config.Settings{}}
	picker := b.connectPickerCard()
	if len(picker.Pages) != 1 || len(picker.Pages[0].Buttons) != 4 {
		t.Fatal("platform selection is cluttered or incomplete")
	}
	for _, key := range []string{"ios", "android", "windows", "macos"} {
		state, err := prepareCard(b.platformCard(key))
		if err != nil {
			t.Fatal(err)
		}
		if state.Pages[0].Step != 1 || state.Pages[0].Back == 0 {
			t.Fatal("missing installation progress or back action")
		}
		platform, _ := connectionPlatform(key)
		if state.Pages[0].Buttons[0].Payload != platform.URL {
			t.Fatal("wrong platform store")
		}
	}
}

func TestClassicConnectionJourneyKeepsNativeTransport(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{}, nil)
	_ = db.SetBotView(ctx, 7, "classic")
	if err := b.deliverJourney(ctx, 7, 42, b.platformCard("android")); err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.methods, ",") != "editMessageText" {
		t.Fatalf("classic journey was mixed with widgets: %v", caller.methods)
	}
	text, kb := nativeCardScreen(b.connectPickerCard())
	if !strings.Contains(text, "Какое устройство?") || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatal("platform picker lost its compact layout")
	}
}

func TestCopyHandoffKeepsTheActiveCardAndUsesUnprotectedMessage(t *testing.T) {
	db := testdb.Open(t)
	base := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{}, nil)
	_ = db.SetBotView(base, 7, "cards")
	value := "https://example.com/private-subscription"
	if err := b.deliverUI(base, 7, 0, textCard("Подключение", keyboard([]telego.InlineKeyboardButton{copyButton("Получить ссылку", value)}))); err != nil {
		t.Fatal(err)
	}
	id, raw, _ := db.BotCard(base, 7)
	var state cardState
	_ = json.Unmarshal(raw, &state)
	q := telego.WidgetQuery{ID: "copy", From: telego.User{ID: 7}, Chat: &telego.Chat{ID: 7, Type: "private"}, MessageID: id, Type: &telego.WidgetQueryTypeData{Type: "data"}, Data: "ui:" + state.Pages[0].Revision + ":a:0"}
	if err := b.answerCardQuery((&telegohandler.Context{}).WithContext(base), q); err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.methods, ",") != "sendWidget,sendMessage,answerWidgetQuery" {
		t.Fatalf("copy changed the dashboard: %v", caller.methods)
	}
	var message struct {
		ChatID         int64                       `json:"chat_id"`
		ProtectContent bool                        `json:"protect_content"`
		ReplyMarkup    telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	_ = json.Unmarshal([]byte(caller.bodies[1]), &message)
	if message.ChatID != 7 || message.ProtectContent || message.ReplyMarkup.InlineKeyboard[0][0].CopyText.Text != value {
		t.Fatal("copy helper is protected or sent to the wrong chat")
	}
	currentID, _, _ := db.BotCard(base, 7)
	if currentID != id {
		t.Fatal("copy helper replaced active navigation")
	}
}
