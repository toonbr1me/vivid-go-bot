package bot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestBerlogaLegacyUsesBrandedRichMainAndSubscription(t *testing.T) {
	for _, screen := range []string{"main", "subscription", "new-user"} {
		t.Run(screen, func(t *testing.T) {
			db := testdb.Open(t)
			caller := &scriptedCaller{}
			api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
			cfg := &config.Settings{BrandName: "Берлога", BotStyle: "legacy", RichMessages: true, DeviceLimitEnabled: true, TrialBotDays: 7, WebDir: filepath.Join("..", "..", "web", "themes", "berloga")}
			registry, _ := payments.BuildRegistry(cfg, nil)
			var panel payments.Panel = journeyPanel{}
			if screen == "new-user" {
				panel = nil
			}
			b := New(api, db, cfg, payments.New(db, panel, registry, cfg))
			stub, request := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
			b.rich = stub.rich
			name := screen
			if screen == "new-user" {
				name = "main"
			}
			b.showCommandScreen(context.Background(), telego.ChatID{ID: 7}, name, 7)
			html := request.RichMessage.HTML
			want := []string{"🐻", "<table bordered striped compact>", "<tg-button-row>", "автосписаний"}
			if screen == "main" {
				want = append(want, "Берлога", "Укрытие до", "nav:subscription", "nav:support")
			}
			if screen == "subscription" {
				want = append(want, "Моя берлога", "Точка входа")
			}
			if screen == "new-user" {
				want = append(want, "Интернет с медвежьей силой", "Оплата", "Разовая", "7 дней", "nav:trial")
			}
			for _, fragment := range want {
				if !strings.Contains(html, fragment) {
					t.Errorf("missing %q in rich screen: %s", fragment, html)
				}
			}
			if len(caller.methods) != 0 {
				t.Fatalf("branded rich unexpectedly used fallback: %v", caller.methods)
			}
		})
	}
}
