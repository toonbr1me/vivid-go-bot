package config

import "testing"

func TestBotStyleAndRecurringDefaults(t *testing.T) {
	baseEnv(t)
	s, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.BotStyle != "legacy" || s.BotHTMLTheme != "telegram" || !s.BotCleanChat || s.RecurringEnabled {
		t.Fatal("unexpected interface/payment defaults")
	}
	t.Setenv("STYLE", "html")
	s, err = Load(nil)
	if err != nil || s.BotStyle != "html" {
		t.Fatal("STYLE alias ignored")
	}
	t.Setenv("BOT_STYLE", "legacy")
	s, err = Load(nil)
	if err != nil || s.BotStyle != "legacy" {
		t.Fatal("BOT_STYLE must take precedence")
	}
	t.Setenv("BOT_STYLE", "typo")
	if _, err = Load(nil); err == nil {
		t.Fatal("invalid style accepted")
	}
}

func TestHTMLTemplateCannotEscapeItsDirectory(t *testing.T) {
	baseEnv(t)
	t.Setenv("BOT_HTML_TEMPLATE", "../../outside")
	if _, err := Load(nil); err == nil {
		t.Fatal("path traversal in template name accepted")
	}
}
