package bot

import (
	"embed"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
)

//go:embed widgets/assets/*.webp
var mascotAssets embed.FS

func MascotAsset(name string) ([]byte, error) {
	if name != "vi-peek.webp" && name != "vi-face.webp" && name != "vi-wave.webp" {
		return nil, os.ErrNotExist
	}
	return mascotAssets.ReadFile("widgets/assets/" + name)
}

var templateNameRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Local files are trusted project code. The runtime remains owned by the bot;
// custom HTML must retain its DOM contract. No admin uploads or arbitrary JS.
func widgetTemplate(dir, name string, theme appearance.Theme, mascotURL string) (string, error) {
	if name == "" {
		name = "default"
	}
	if !templateNameRE.MatchString(name) {
		return "", fmt.Errorf("неверное имя HTML-шаблона")
	}
	source := interfaceWidgetHTML
	base := filepath.Join(dir, "bot", "html", name)
	if dir != "" {
		raw, err := os.ReadFile(filepath.Join(base, "interface.html"))
		if err == nil {
			source = string(raw)
		} else if !os.IsNotExist(err) || name != "default" {
			return "", fmt.Errorf("HTML-шаблон %s: %w", name, err)
		}
	}
	// Compare required runtime IDs against the built-in document before sending.
	ids := regexp.MustCompile(`id="([a-z0-9_-]+)"`).FindAllStringSubmatch(interfaceWidgetHTML, -1)
	for _, id := range ids {
		if !strings.Contains(source, `id="`+id[1]+`"`) {
			return "", fmt.Errorf("в шаблоне отсутствует #%s", id[1])
		}
	}
	if !strings.Contains(source, "createVividWidget(document,window.TelegramWidget)") {
		return "", fmt.Errorf("в шаблоне отсутствует runtime TelegramWidget")
	}
	css := theme.CSS()
	if dir != "" {
		raw, err := os.ReadFile(filepath.Join(base, "style.css"))
		if err == nil {
			css += string(raw)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	source = strings.Replace(source, "</style>", css+"</style>", 1)
	source = strings.ReplaceAll(source, "{{MASCOT_URL}}", escapeHTML(mascotURL))
	if mascotURL == "" {
		source = strings.Replace(source, `id="mascot"`, `id="mascot" style="display:none"`, 1)
	}
	if err := validateWidgetDocument(source); err != nil {
		return "", err
	}
	return source, nil
}

func previewMascot(markup string) string {
	raw, err := MascotAsset("vi-peek.webp")
	if err != nil {
		return markup
	}
	return strings.ReplaceAll(markup, "{{MASCOT_URL}}", "data:image/webp;base64,"+base64.StdEncoding.EncodeToString(raw))
}
