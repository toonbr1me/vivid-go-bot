package bot

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

func miniAppURL(cfg *config.Settings) string {
	if !cfg.SiteEnabled {
		return ""
	}
	u, err := url.Parse(cfg.SiteBaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/app"
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u.String()
}

func (b *Bot) syncMiniAppMenu(ctx context.Context) error {
	target := miniAppURL(b.cfg.Current())
	if target == "" {
		return nil
	}
	current, err := b.api.GetChatMenuButton(ctx, &telego.GetChatMenuButtonParams{})
	if err != nil {
		return err
	}
	if button, ok := current.(*telego.MenuButtonWebApp); ok && button.WebApp.URL == target && button.Text == "Кабинет" {
		return nil
	}
	return b.api.SetChatMenuButton(ctx, &telego.SetChatMenuButtonParams{MenuButton: &telego.MenuButtonWebApp{Type: telego.ButtonTypeWebApp, Text: "Кабинет", WebApp: telego.WebAppInfo{URL: target}}})
}

// Reconcile at startup and periodically: a new SITE_BASE_URL repairs old menu
// links automatically. Telegram outages are retried without stopping polling.
func (b *Bot) maintainMiniAppMenu(ctx context.Context) {
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := b.syncMiniAppMenu(attempt)
		cancel()
		delay := 15 * time.Minute
		if err != nil {
			slog.Warn("бот: кнопка приложения не обновлена", "err", err)
			delay = time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
