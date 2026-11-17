package bot

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
)

// The reserve theme is a compatibility fixture: bot preview updates must not
// silently stop loading it or reintroduce the default mascot into its layout.
func TestVyzhivalovoReserveThemeLoadsInBotPreview(t *testing.T) {
	dir := filepath.Join("..", "..", "web", "themes", "vyzhivalovo")
	theme, err := appearance.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	document, err := widgetTemplate(dir, "default", theme, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(document, "#mascot{display:none}") {
		t.Fatal("reserve theme must hide the unrelated default mascot")
	}
}
