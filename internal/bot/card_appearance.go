package bot

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
)

type WidgetPreviewAssets struct{ Markup, CSS, Runtime string }

func WidgetDocument(theme appearance.Theme) (string, error) {
	if err := theme.Validate(); err != nil {
		return "", err
	}
	html := strings.Replace(interfaceWidgetHTML, "</style>", theme.CSS()+"</style>", 1)
	return html, validateWidgetDocument(html)
}

func validateWidgetDocument(html string) error {
	var compressed bytes.Buffer
	w, _ := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	_, _ = w.Write([]byte(html))
	_ = w.Close()
	if len(html) > 32768 || compressed.Len() > 4096 {
		return fmt.Errorf("оформление превышает лимит Telegram: HTML %d/32768, zlib %d/4096", len(html), compressed.Len())
	}
	return nil
}

// The gallery uses the exact widget markup, CSS and runtime, not a separately
// drawn imitation. It replaces TelegramWidget only with an isolated demo API.
func PreviewAssets(theme appearance.Theme) (WidgetPreviewAssets, error) {
	html, err := WidgetDocument(theme)
	if err != nil {
		return WidgetPreviewAssets{}, err
	}
	_, css, _ := strings.Cut(html, "<style>")
	css, _, _ = strings.Cut(css, "</style>")
	_, body, _ := strings.Cut(html, "<body>")
	markup, runtime, _ := strings.Cut(body, "<script>")
	runtime, _, _ = strings.Cut(runtime, "</script>")
	runtime = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(runtime), "createVividWidget(document,window.TelegramWidget);"))
	return WidgetPreviewAssets{previewMascot(markup), css, runtime}, nil
}

func (b *Bot) widgetAppearance(ctx context.Context) (string, string, error) {
	theme, err := appearance.Load(b.cfg.Current().WebDir)
	if err != nil {
		return "", "", err
	}
	if b.db != nil {
		value, err := b.db.AppSetting(ctx, appearance.SettingKey)
		if err != nil {
			return "", "", err
		}
		if value != "" {
			theme, err = appearance.Parse([]byte(value), theme)
			if err != nil {
				return "", "", err
			}
		}
	}
	if b.cfg.Current().BotHTMLTheme == "telegram" {
		theme.Accent = "telegram"
	}
	mascotURL := ""
	if site := b.siteURL(); strings.HasPrefix(site, "https://") {
		mascotURL = site + "/static/bot-mascot/vi-peek.webp"
	}
	html, err := widgetTemplate(b.cfg.Current().WebDir, b.cfg.Current().BotHTMLTemplate, theme, mascotURL)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(html))
	return html, hex.EncodeToString(sum[:]), nil
}
