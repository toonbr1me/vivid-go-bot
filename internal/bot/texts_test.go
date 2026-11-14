package bot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/bot/texts"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

// Бот читает тексты из каталога WEB_DIR: другой проект переписывает их
// файлом, не меняя код.
func TestNewReadsTextsFromWebDir(t *testing.T) {
	dir := t.TempDir()
	body := `{"main": {"title": "Здравствуйте, {{.Brand}}"}, "buttons": {"menu": "Домой"}}`
	if err := os.WriteFile(filepath.Join(dir, texts.FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Settings{BrandName: "Другой сервис", Currency: "RUB", WebDir: dir}
	bot := New(nil, nil, cfg, nil)

	if bot.texts.Main.Title != "Здравствуйте, {{.Brand}}" {
		t.Errorf("текст из файла не применился: %q", bot.texts.Main.Title)
	}
	if bot.texts.Buttons.Menu != "Домой" {
		t.Errorf("подпись кнопки из файла не применилась: %q", bot.texts.Buttons.Menu)
	}
	// Незаданные тексты остаются встроенными.
	if bot.texts.Main.StatusNone == "" {
		t.Error("встроенный текст потерялся")
	}
	// Подстановки работают.
	got := bot.text(bot.texts.Main.Title, nil)
	if got == "" || !containsText(got, "Другой сервис") {
		t.Errorf("подстановка бренда не сработала: %q", got)
	}
}

// Бот без файла текстов работает на встроенных.
func TestNewWithoutTextsFile(t *testing.T) {
	cfg := &config.Settings{BrandName: "Vivid", Currency: "RUB", WebDir: t.TempDir()}
	bot := New(nil, nil, cfg, nil)
	if bot.texts.Main.Title != texts.Default().Main.Title {
		t.Error("без файла должны остаться встроенные тексты")
	}
}

func containsText(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
