package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

// Витрина собирается из блоков: владелец задаёт их в theme.json.
func TestThemeBlocksReplaceBuiltIn(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"blocks": [
			{"type": "hero", "title": "Свой заголовок", "text": "Свой текст"},
			{"type": "steps", "title": "Как начать", "steps": ["Первый шаг", "Второй шаг"]}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	values, err := loadTheme(&config.Settings{WebDir: dir})
	if err != nil {
		t.Fatalf("тема не загрузилась: %v", err)
	}
	if len(values.Blocks) != 2 {
		t.Fatalf("блоков %d, ждали 2", len(values.Blocks))
	}
	if values.Blocks[0].Type != "hero" || values.Blocks[0].Title != "Свой заголовок" {
		t.Errorf("первый блок = %+v", values.Blocks[0])
	}
	if len(values.Blocks[1].Steps) != 2 {
		t.Errorf("шаги = %v, ждали два", values.Blocks[1].Steps)
	}
}

// Без блоков в файле собираются встроенные — с учётом окружения.
func TestThemeBuildsDefaultBlocks(t *testing.T) {
	values, err := loadTheme(&config.Settings{WebDir: t.TempDir(), BrandName: "Другой"})
	if err != nil {
		t.Fatalf("тема не загрузилась: %v", err)
	}
	if len(values.Blocks) == 0 {
		t.Fatal("встроенные блоки должны быть")
	}
	if values.Blocks[0].Type != "hero" {
		t.Errorf("первый блок = %q, ждали hero", values.Blocks[0].Type)
	}
	// Названия разделов из темы попадают в блоки.
	found := false
	for _, block := range values.Blocks {
		if block.Type == "features" && len(block.Items) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("встроенный блок преимуществ должен содержать пункты")
	}
}

// Ошибка в блоках не должна проходить молча: витрина без блоков — пустая
// страница.
func TestBrokenBlocksFailStart(t *testing.T) {
	dir := t.TempDir()
	body := `{"blocks": [{"type": "hero", "title": 5}]}`
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTheme(&config.Settings{WebDir: dir}); err == nil {
		t.Fatal("ожидалась ошибка разбора блоков")
	}
}

// Блоки попадают на страницу.
func TestRenderLandingWithCustomBlocks(t *testing.T) {
	dir := t.TempDir()
	body := `{"blocks": [
		{"type": "hero", "title": "Заголовок из файла"},
		{"type": "steps", "title": "Порядок", "steps": ["Сначала", "Потом"]}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := newRenderer(&config.Settings{WebDir: dir})
	if err != nil {
		t.Fatalf("рендерер не собрался: %v", err)
	}
	data := pageData{Site: r.theme, Meta: pageMeta{Title: "Витрина"}, Data: landingData{}}
	rec := httptest.NewRecorder()
	if err := r.render(rec, 200, "landing.html", data); err != nil {
		t.Fatalf("страница не отрисовалась: %v", err)
	}
	html := rec.Body.String()
	if !strings.Contains(html, "Заголовок из файла") {
		t.Errorf("в выводе нет заголовка из файла")
	}
	if !strings.Contains(html, "Сначала") {
		t.Errorf("в выводе нет шагов из файла")
	}
}
