package bot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/bot/texts"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

// Меню собирается по раскладке: другой проект переписывает её файлом.
func TestMenuFollowsConfiguredLayout(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"menu": {
			"layout": {
				"active": [["subscription"], ["profile", "support"]]
			},
			"buttons": {
				"subscription": {"label": "⚡ Мой доступ", "action": "nav:subscription"}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, texts.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{BrandName: "Vivid", Currency: "RUB", WebDir: dir}
	bot := New(nil, nil, cfg, nil)

	markup := bot.buildMenu("ACTIVE", menuConditions{Payments: true, Trial: true, Devices: true})
	if markup == nil || len(markup.InlineKeyboard) != 2 {
		t.Fatalf("рядов = %v, ждали 2", markup)
	}
	first := markup.InlineKeyboard[0][0]
	if first.Text != "⚡ Мой доступ" {
		t.Errorf("подпись из файла не применилась: %q", first.Text)
	}
	if first.CallbackData != "nav:subscription" {
		t.Errorf("действие = %q, ждали nav:subscription", first.CallbackData)
	}
	// Состояния, которых в файле нет, остаются встроенными.
	other := bot.buildMenu("NONE", menuConditions{Payments: true, Trial: true, Devices: true})
	if other == nil || len(other.InlineKeyboard) == 0 {
		t.Fatal("встроенная раскладка должна остаться")
	}
}

func TestMenuHidesButtonsWhoseConditionFails(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"menu": {
			"layout": {"inactive": [["trial", "buy", "profile"]]},
			"buttons": {
				"trial": {"label": "🎁 Попробовать", "action": "nav:trial", "require": ["trial"]},
				"buy": {"label": "💳 Купить", "action": "nav:buy", "require": ["payments"]},
				"profile": {"label": "👤 Профиль", "action": "nav:profile"}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, texts.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{BrandName: "Vivid", Currency: "RUB", WebDir: dir}
	bot := New(nil, nil, cfg, nil)

	// Платежей нет и пробного периода нет — остаётся только профиль.
	markup := bot.buildMenu("NONE", menuConditions{Payments: false, Trial: false})
	if markup == nil || len(markup.InlineKeyboard) != 1 {
		t.Fatalf("рядов = %v, ждали 1", markup)
	}
	row := markup.InlineKeyboard[0]
	if len(row) != 1 || row[0].CallbackData != "nav:profile" {
		t.Errorf("осталась не та кнопка: %v", row)
	}
}

func TestMenuPacksRows(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"menu": {
			"layout": {"active": [{"pack": 2, "buttons": ["profile", "support", "referral", "more"]}]}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, texts.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{BrandName: "Vivid", Currency: "RUB", WebDir: dir}
	bot := New(nil, nil, cfg, nil)

	markup := bot.buildMenu("ACTIVE", menuConditions{Payments: true, Trial: true, Devices: true})
	if markup == nil || len(markup.InlineKeyboard) != 2 {
		t.Fatalf("рядов = %v, ждали 2 (по две кнопки)", markup)
	}
	for i, row := range markup.InlineKeyboard {
		if len(row) != 2 {
			t.Errorf("в ряду %d кнопок %d, ждали 2", i, len(row))
		}
	}
}

func TestMenuButtonMayPointOutside(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"menu": {
			"layout": {"active": [["channel"]]},
			"buttons": {
				"channel": {"label": "📣 Канал", "url": "https://t.me/example"}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, texts.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{BrandName: "Vivid", Currency: "RUB", WebDir: dir}
	bot := New(nil, nil, cfg, nil)

	markup := bot.buildMenu("ACTIVE", menuConditions{})
	if markup == nil || len(markup.InlineKeyboard) != 1 {
		t.Fatalf("кнопка со ссылкой не появилась: %v", markup)
	}
	if markup.InlineKeyboard[0][0].URL == "" {
		t.Error("кнопка должна вести по внешней ссылке")
	}
}
