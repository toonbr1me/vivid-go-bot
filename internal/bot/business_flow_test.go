package bot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestConfiguredStyleOverridesPreviousUserPreference(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	_ = db.SetBotView(ctx, 7, "cards")
	b := &Bot{db: db, cfg: &config.Settings{BotStyle: "legacy"}}
	if b.viewMode(ctx, 7) != "classic" {
		t.Fatal("stored selection overrode env")
	}
	b.cfg.BotStyle = "html"
	_ = db.SetBotView(ctx, 7, "classic")
	if b.viewMode(ctx, 7) != "cards" {
		t.Fatal("HTML env ignored")
	}
}

func TestCleanSupportPreservesContentInHistoryBeforeDeleting(t *testing.T) {
	db := testdb.Open(t)
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BotStyle: "html", BotCleanChat: true, BrandName: "Vivid"}
	registry, _ := payments.BuildRegistry(cfg, nil)
	b := New(api, db, cfg, payments.New(db, journeyPanel{}, registry, cfg))
	b.setInput(7, inputTicket, "")
	ctx := (&telegohandler.Context{}).WithContext(context.Background())
	err := b.handleText(ctx, telego.Message{MessageID: 99, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: "Не удаётся подключиться"})
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := db.EnsureTelegramSubject(ctx, 7)
	ticket, _ := db.OpenTicketForSubject(ctx, subject.ID)
	if ticket == nil {
		t.Fatal("deleted without creating a ticket")
	}
	messages, err := db.TicketMessages(ctx, ticket.ID, 10)
	if err != nil || len(messages) != 1 || messages[0].Text != "Не удаётся подключиться" {
		t.Fatal("support text not persisted")
	}
	if !strings.Contains(strings.Join(caller.methods, ","), "deleteMessage") {
		t.Fatal("incoming support message left in chat")
	}
	_, raw, err := db.BotCard(ctx, 7)
	var state cardState
	_ = json.Unmarshal(raw, &state)
	if err != nil || len(state.Pages) == 0 || !strings.Contains(state.Pages[0].Title, "Обращение #") {
		t.Fatal("ticket history not shown as a card")
	}
}

func TestCheckoutToggleFiltersUnsupportedMethods(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	cfg := &config.Settings{BrandName: "Vivid", Currency: "RUB", RecurringEnabled: true, RecurringProviders: []string{"yookassa", "platega"}, PaymentProviders: []string{"yookassa", "platega", "wata"}, PaymentMethods: []string{"card", "sbp"}}
	registry, _ := payments.BuildRegistry(cfg, nil)
	b := New(nil, db, cfg, payments.New(db, journeyPanel{}, registry, cfg))
	_ = db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, Devices: 3, IsActive: true})
	_ = db.SaveCheckoutPreference(ctx, 7, "30d", false)
	state, err := b.checkoutCard(ctx, 7, "30d")
	if err != nil {
		t.Fatal(err)
	}
	count := func(s cardState) int {
		n := 0
		for _, a := range s.Actions {
			if strings.HasPrefix(a.CallbackData, "pay:") || strings.HasPrefix(a.CallbackData, "payr:") {
				n++
			}
		}
		return n
	}
	if count(state) <= 2 {
		t.Fatal("ordinary methods missing")
	}
	_ = db.SaveCheckoutPreference(ctx, 7, "30d", true)
	state, err = b.checkoutCard(ctx, 7, "30d")
	if err != nil {
		t.Fatal(err)
	}
	if count(state) != 2 {
		t.Fatalf("wrong recurring methods: %v", state.Actions)
	}
	for _, a := range state.Actions {
		if strings.HasPrefix(a.CallbackData, "payr:") && (strings.Contains(a.CallbackData, "wata") || strings.HasSuffix(a.CallbackData, "yookassa.sbp") || strings.HasSuffix(a.CallbackData, "platega.card")) {
			t.Fatal("unsupported method offered")
		}
	}
	prepared, err := prepareCard(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prepared.Pages {
		raw, _ := json.Marshal(p)
		if len(raw) > 1024 {
			t.Fatal("checkout exceeds Telegram private data limit")
		}
	}
}

func TestFileTemplateValidatesDOMAndCompressedSize(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "bot", "html", "example")
	_ = os.MkdirAll(base, 0755)
	_ = os.WriteFile(filepath.Join(base, "interface.html"), []byte(interfaceWidgetHTML), 0644)
	_ = os.WriteFile(filepath.Join(base, "style.css"), []byte("#hero-copy{font-size:16px}"), 0644)
	html, err := widgetTemplate(dir, "example", appearance.Default(), "https://example.com/vi.webp")
	if err != nil || !strings.Contains(html, "https://example.com/vi.webp") {
		t.Fatalf("file template: %v", err)
	}
	_ = os.WriteFile(filepath.Join(base, "interface.html"), []byte("<html>missing runtime</html>"), 0644)
	if _, err = widgetTemplate(dir, "example", appearance.Default(), ""); err == nil {
		t.Fatal("broken template accepted")
	}
	if _, err = widgetTemplate(dir, "../outside", appearance.Default(), ""); err == nil {
		t.Fatal("path traversal accepted")
	}
}
