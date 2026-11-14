// Package appearance defines validated presentation tokens shared by the bot
// and its preview/editor. Tokens cannot inject markup or external resources.
package appearance

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const SettingKey = "BOT_UI_THEME"

type Theme struct {
	Name         string `json:"name"`
	Accent       string `json:"accent"`
	Radius       int    `json:"radius"`
	Spacing      int    `json:"spacing"`
	ButtonHeight int    `json:"button_height"`
	Heading      string `json:"heading"`
	Navigation   string `json:"navigation"`
}

func Default() Theme {
	return Theme{Name: "Vivid", Accent: "telegram", Radius: 8, Spacing: 16, ButtonHeight: 44, Heading: "balanced", Navigation: "bottom"}
}

var color = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (t Theme) Validate() error {
	if len([]rune(t.Name)) > 48 {
		return fmt.Errorf("название темы: не более 48 символов")
	}
	if t.Accent != "telegram" && !color.MatchString(t.Accent) {
		return fmt.Errorf("акцент: telegram или цвет #RRGGBB")
	}
	if t.Radius < 4 || t.Radius > 20 {
		return fmt.Errorf("скругление: от 4 до 20 px")
	}
	if t.Spacing < 8 || t.Spacing > 20 {
		return fmt.Errorf("отступы: от 8 до 20 px")
	}
	if t.ButtonHeight < 40 || t.ButtonHeight > 48 {
		return fmt.Errorf("кнопки: от 40 до 48 px")
	}
	if t.Heading != "balanced" && t.Heading != "compact" {
		return fmt.Errorf("размер заголовка: balanced или compact")
	}
	if t.Navigation != "bottom" && t.Navigation != "top" {
		return fmt.Errorf("навигация: top или bottom")
	}
	return nil
}
func Parse(payload []byte, base Theme) (Theme, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&base); err != nil {
		return base, fmt.Errorf("тема: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return base, fmt.Errorf("тема: ожидается один JSON-объект")
	}
	return base, base.Validate()
}
func Load(dir string) (Theme, error) {
	t := Default()
	if strings.TrimSpace(dir) == "" {
		return t, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "bot-theme.json"))
	if os.IsNotExist(err) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	return Parse(data, t)
}
func (t Theme) CSS() string {
	if t.Validate() != nil {
		t = Default()
	}
	accent := "var(--tg-theme-button-color)"
	text := "var(--tg-theme-button-text-color)"
	if t.Accent != "telegram" {
		accent = t.Accent
		text = contrast(t.Accent)
	}
	heading := "1.45em"
	if t.Heading == "compact" {
		heading = "1.2em"
	}
	css := fmt.Sprintf(`body{margin:%dpx}button{border-radius:%dpx}h2{font-size:%s}#actions button{min-height:%dpx;padding:%dpx 12px}#actions button.primary{background:%s;color:%s}nav button.active{color:var(--tg-theme-accent-text-color)}#actions{gap:%dpx}`, t.Spacing, t.Radius, heading, t.ButtonHeight, max(8, (t.ButtonHeight-24)/2), accent, text, max(4, t.Spacing/2))
	if t.Navigation == "top" {
		css += `body{display:flex;flex-direction:column}header{order:-2}nav{order:-1;border-top:0;border-bottom:1px solid var(--tg-theme-section-separator-color);margin:0 0 16px;padding:0 0 8px}`
	}
	return css
}
func contrast(hex string) string {
	var r, g, b int
	_, _ = fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	linear := func(v int) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	if .2126*linear(r)+.7152*linear(g)+.0722*linear(b) > .179 {
		return "#111827"
	}
	return "#ffffff"
}
