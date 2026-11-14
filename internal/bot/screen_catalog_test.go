package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

type refusedWidgetCaller struct{ scriptedCaller }

func (c *refusedWidgetCaller) Call(ctx context.Context, url string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
	response, err := c.scriptedCaller.Call(ctx, url, data)
	if strings.HasSuffix(url, "/sendWidget") {
		return &telegoapi.Response{Ok: false, Error: &telegoapi.Error{ErrorCode: 400, Description: "Bad Request: INPUT_MEDIA_WIDGET_NOT_ALLOWED"}}, nil
	}
	return response, err
}

func TestRefusedWidgetDoesNotReplaceHTMLModeWithAnOrdinaryMenu(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &refusedWidgetCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil)
	if err = db.SetBotView(ctx, 7, "cards"); err != nil {
		t.Fatal(err)
	}
	b.showScreen(ctx, telego.ChatID{ID: 7}, 0, "connect", 7)
	if got := strings.Join(caller.methods, ","); got != "sendWidget,sendMessage" {
		t.Fatalf("unexpected fallback: %s", got)
	}
	if !strings.Contains(caller.bodies[1], "выбранный вид сохранён") || strings.Contains(caller.bodies[1], "reply_markup") || b.viewMode(ctx, 7) != "cards" {
		t.Fatal("HTML mode was silently replaced by an ordinary menu")
	}
}

func TestRefusedNewTemplateKeepsExistingCompatibleHTMLWidget(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &refusedWidgetCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil)
	old, err := prepareCard(b.connectPickerCard())
	if err != nil {
		t.Fatal(err)
	}
	old.Appearance = "previous-theme"
	if err = db.SaveBotCard(ctx, 7, 42, old); err != nil {
		t.Fatal(err)
	}
	if err = b.deliverUI(ctx, 7, 0, b.platformCard("android")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(caller.methods, ","); got != "sendWidget,editMessageWidgetData" {
		t.Fatalf("HTML widget was replaced by ordinary text: %s", got)
	}
	id, raw, err := db.BotCard(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	var updated cardState
	_ = json.Unmarshal(raw, &updated)
	if id != 42 || updated.Appearance != "previous-theme" || updated.Pages[0].Title != "Android" {
		t.Fatal("existing widget state or appearance was lost")
	}
	// An incompatible document must never receive the new data protocol.
	updated.Version--
	if err = db.SaveBotCard(ctx, 7, 42, updated); err != nil {
		t.Fatal(err)
	}
	caller.methods = nil
	if err = b.deliverUI(ctx, 7, 0, b.connectPickerCard()); err == nil {
		t.Fatal("incompatible widget was reused")
	}
	if strings.Join(caller.methods, ",") != "sendWidget" {
		t.Fatal("incompatible document received new data")
	}
}

func TestCatalogHasCompleteNavigableScreensWithinTelegramLimits(t *testing.T) {
	screens := ScreenCatalog()
	if len(screens) != 42 {
		t.Fatalf("catalog lost a screen: %d", len(screens))
	}
	ids := map[string]bool{"rich": true}
	for _, s := range screens {
		if ids[s.ID] {
			t.Fatalf("duplicate screen: %s", s.ID)
		}
		ids[s.ID] = true
		if s.RichHTML == "" || len(s.State.Pages) == 0 {
			t.Fatalf("empty screen: %s", s.ID)
		}
		if len(s.Targets) != len(s.State.Actions) {
			t.Fatalf("missing targets: %s", s.ID)
		}
		for _, p := range s.State.Pages {
			payload, _ := json.Marshal(p)
			if len(payload) > 1024 {
				t.Fatalf("oversized %s: %d", s.ID, len(payload))
			}
			for _, b := range p.Buttons {
				if b.Index < 0 || b.Index >= len(s.State.Actions) {
					t.Fatalf("invalid action in %s", s.ID)
				}
			}
		}
	}
	for _, s := range screens {
		for _, target := range s.Targets {
			if !ids[target] {
				t.Errorf("broken route %s -> %s", s.ID, target)
			}
		}
	}
}

func TestThemeChangeReplacesImmutableWidgetOnlyOnce(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil)
	state := textCard("Проверка темы", nil)
	if err = b.deliverUI(ctx, 7, 0, state); err != nil {
		t.Fatal(err)
	}
	id, _, _ := db.BotCard(ctx, 7)
	theme := appearance.Default()
	theme.Accent = "#34764d"
	theme.Navigation = "top"
	theme.Radius = 20
	theme.Spacing = 20
	theme.ButtonHeight = 48
	if _, err = WidgetDocument(theme); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(theme)
	if err = db.SaveAppSetting(ctx, appearance.SettingKey, string(payload), 7); err != nil {
		t.Fatal(err)
	}
	if err = b.deliverUI(ctx, 7, id, state); err != nil {
		t.Fatal(err)
	}
	id, _, _ = db.BotCard(ctx, 7)
	if err = b.deliverUI(ctx, 7, id, state); err != nil {
		t.Fatal(err)
	}
	// The stub always returns message 1, so there is no distinct old ID to delete.
	if got := strings.Join(caller.methods, ","); got != "sendWidget,sendWidget,editMessageWidgetData" {
		t.Fatalf("unexpected template lifecycle: %s", got)
	}
}
