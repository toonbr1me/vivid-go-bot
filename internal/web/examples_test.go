package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/bot/texts"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

// Примеры настройки должны оставаться рабочими: их копируют и правят.
func TestExampleFilesAreValid(t *testing.T) {
	root := filepath.Join("..", "..", "web")

	theme, err := os.ReadFile(filepath.Join(root, "theme.example.json"))
	if err != nil {
		t.Fatalf("пример темы не найден: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), theme, 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := loadTheme(&config.Settings{WebDir: dir})
	if err != nil {
		t.Fatalf("пример темы не разбирается: %v", err)
	}
	if len(values.Blocks) == 0 {
		t.Error("в примере темы должны быть блоки витрины")
	}
	for _, block := range values.Blocks {
		if block.Type == "" {
			t.Error("у блока нет вида")
		}
	}

	bot, err := os.ReadFile(filepath.Join(root, "bot.example.json"))
	if err != nil {
		t.Fatalf("пример текстов бота не найден: %v", err)
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bot.json"), bot, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := texts.Load(dir); err != nil {
		t.Fatalf("пример текстов бота не разбирается: %v", err)
	}
}
