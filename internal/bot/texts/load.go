package texts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// FileName — файл с текстами в каталоге WEB_DIR.
const FileName = "bot.json"

// Load читает тексты: значения по умолчанию и файл WEB_DIR/bot.json поверх
// них.
//
// В файле заданы только те ключи, которые другой проект хочет переписать:
// остальные остаются встроенными. Ошибка разбора — отказ запуска: молча
// половина текстов на старом языке хуже, чем явная остановка.
func Load(dir string) (*Texts, error) {
	values := Default()
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &values, nil
	}
	path := filepath.Join(dir, FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &values, nil
		}
		return nil, fmt.Errorf("тексты бота %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("тексты бота %s: %w", path, err)
	}
	mergeMenu(&values, raw)
	return &values, nil
}

// mergeMenu сличает меню по частям.
//
// Раскладка и кнопки меняются независимо: переписать раскладку активного
// состояния не должно стирать описания кнопок, и наоборот. Обычный разбор
// JSON заменил бы обе карты целиком.
func mergeMenu(values *Texts, raw []byte) {
	var overlay struct {
		Menu struct {
			Layout  map[string][]MenuRow  `json:"layout"`
			Buttons map[string]MenuButton `json:"buttons"`
		} `json:"menu"`
	}
	if err := json.Unmarshal(raw, &overlay); err != nil {
		return
	}
	for state, rows := range overlay.Menu.Layout {
		values.Menu.Layout[state] = rows
	}
	for id, button := range overlay.Menu.Buttons {
		values.Menu.Buttons[id] = button
	}
}

// Render подставляет значения в текст.
//
// Неизвестная подстановка даёт пустое место, а не ошибку: лишний ключ в
// bot.json не должен ронять бота посреди работы.
func (t *Texts) Render(text string, data map[string]any) string {
	if text == "" {
		return ""
	}
	tpl, err := template.New("text").Option("missingkey=zero").Parse(text)
	if err != nil {
		return text
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		return text
	}
	return buf.String()
}
