package texts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWithoutFileKeepsDefaults(t *testing.T) {
	values, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("загрузка: %v", err)
	}
	if values.Main.Title != Default().Main.Title {
		t.Error("без файла тексты должны остаться встроенными")
	}
}

func TestOverlayReplacesOnlyGivenKeys(t *testing.T) {
	dir := t.TempDir()
	body := `{"main": {"title": "Привет, {{.Brand}}"}, "buttons": {"menu": "Домой"}}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	values, err := Load(dir)
	if err != nil {
		t.Fatalf("загрузка: %v", err)
	}
	if values.Main.Title != "Привет, {{.Brand}}" {
		t.Errorf("заданный ключ не заменён: %q", values.Main.Title)
	}
	// Незаданные ключи остаются встроенными.
	if values.Main.StatusNone != Default().Main.StatusNone {
		t.Errorf("незаданный ключ изменился: %q", values.Main.StatusNone)
	}
	if values.Buttons.Menu != "Домой" {
		t.Errorf("подпись кнопки не заменена: %q", values.Buttons.Menu)
	}
}

func TestBrokenOverlayFailsStart(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("ожидалась ошибка разбора bot.json")
	}
}

func TestRenderSubstitutesValues(t *testing.T) {
	values := Default()
	got := values.Render(values.Main.Title, map[string]any{
		"Brand":  "Vivid",
		"Status": "всё хорошо",
	})
	if !strings.Contains(got, "Vivid") || !strings.Contains(got, "всё хорошо") {
		t.Errorf("подстановки не сработали: %q", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("в тексте остались подстановки: %q", got)
	}
}

func TestRenderKeepsTextWhenValueMissing(t *testing.T) {
	values := Default()
	got := values.Render("Цена: {{.Price}}", map[string]any{})
	if strings.Contains(got, "{{") {
		t.Errorf("неизвестная подстановка должна стать пустым местом: %q", got)
	}
}

func TestRenderSurvivesBrokenTemplate(t *testing.T) {
	values := Default()
	broken := "Цена: {{.Price"
	if got := values.Render(broken, nil); got != broken {
		t.Errorf("испорченный шаблон должен остаться как есть: %q", got)
	}
}
