package config

import (
	"strings"
	"testing"
)

// Настройки читаются из окружения: тесты должны быть hermetic, поэтому
// ключевые переменные задаются явно, а лишние убираются.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"BOT_TOKEN", "SUPPORT_BOT_TOKEN", "SUPPORT_BOT_USERNAME", "SUPPORT_BOT_ENABLED", "DATABASE_URL", "API_BASE_URL", "API_TOKEN", "ADMIN_IDS",
		"REMNAWAVE_WEBHOOK_SECRET", "REMNAWAVE_NAMESPACE", "SUBSCRIPTION_BASE_URL",
		"BRAND_NAME", "CURRENCY", "SITE_ENABLED", "WEBAPP_ENABLED", "PREVIEW_ENABLED",
		"RICH_MESSAGES_ENABLED", "DEFAULT_EXPIRE_DAYS", "TRAFFIC_LIMIT_BYTES",
		"TRIAL_BOT_DAYS", "DEVICE_LIMIT_ENABLED", "PAYMENT_PROVIDERS", "PAYMENT_METHODS",
		"MIN_PAYMENT_AMOUNT", "PUBLIC_BASE_URL", "SITE_BASE_URL", "TELEGRAM_STARS_ENABLED",
		"TELEGRAM_STARS_PRICE", "WEB_DIR", "SUPPORT_LLM_ENABLED", "PHOTO_URLS",
		"YOOKASSA_SHOP_ID", "YOOKASSA_SECRET_KEY", "WATA_ACCESS_TOKEN",
		"PLATEGA_MERCHANT_ID", "PLATEGA_SECRET", "BOT_STYLE", "STYLE", "BOT_HTML_TEMPLATE", "BOT_HTML_THEME", "BOT_CLEAN_CHAT", "PAYMENTS_RECURRING_ENABLED", "PAYMENTS_RECURRING_PROVIDERS",
	} {
		t.Setenv(name, "")
	}
}

func baseEnv(t *testing.T) {
	t.Helper()
	clearEnv(t)
	t.Setenv("BOT_TOKEN", "123:test")
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
}

func TestPanelWebhookSecretIsEnvironmentOnly(t *testing.T) {
	baseEnv(t)
	t.Setenv("REMNAWAVE_WEBHOOK_SECRET", "environment-secret")
	cfg, err := Load(map[string]string{"REMNAWAVE_WEBHOOK_SECRET": "database-value"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RemnawaveWebhookSecret != "environment-secret" {
		t.Fatal("database overrides webhook authentication")
	}
}

func TestDevelopmentPanelScopeIsEnvironmentOnly(t *testing.T) {
	baseEnv(t)
	t.Setenv("REMNAWAVE_NAMESPACE", "devgo")
	t.Setenv("SUBSCRIPTION_BASE_URL", "https://test2.example.com")
	cfg, err := Load(map[string]string{"REMNAWAVE_NAMESPACE": "prod", "SUBSCRIPTION_BASE_URL": "https://prod.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RemnawaveNamespace != "devgo" || cfg.SubscriptionBaseURL != "https://test2.example.com" {
		t.Fatal("database overrides development isolation")
	}
}

// Обязательные настройки: без них сервис не должен стартовать.
func TestLoadRequiresTokenAndDatabase(t *testing.T) {
	clearEnv(t)
	if _, err := Load(nil); err == nil || !strings.Contains(err.Error(), "BOT_TOKEN") {
		t.Fatalf("ждали ошибку про BOT_TOKEN, получили %v", err)
	}
	t.Setenv("BOT_TOKEN", "123:test")
	if _, err := Load(nil); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("ждали ошибку про DATABASE_URL, получили %v", err)
	}
}

// Значения по умолчанию: сервис должен подниматься с минимумом настроек.
func TestLoadDefaults(t *testing.T) {
	baseEnv(t)
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Currency != "RUB" {
		t.Errorf("валюта = %q, ждали RUB", settings.Currency)
	}
	if settings.BrandName != "Vivid" {
		t.Errorf("бренд = %q, ждали Vivid", settings.BrandName)
	}
	if !settings.SiteEnabled || !settings.WebappEnabled {
		t.Errorf("сайт должен быть включён по умолчанию: %+v", settings)
	}
	if !settings.RichMessages {
		t.Error("богатые сообщения должны быть включены по умолчанию")
	}
	if !settings.DeviceLimitEnabled {
		t.Error("лимит устройств должен быть включён по умолчанию")
	}
	if settings.TrialBotDays != 7 {
		t.Errorf("пробный период бота = %d, ждали 7", settings.TrialBotDays)
	}
	if settings.DefaultExpireDays != 30 {
		t.Errorf("срок по умолчанию = %d, ждали 30", settings.DefaultExpireDays)
	}
	if settings.WebDir != "web" {
		t.Errorf("каталог сайта = %q, ждали web", settings.WebDir)
	}
	if settings.TrafficLimitBytes != nil {
		t.Errorf("лимит трафика без настройки = %v, ждали nil", *settings.TrafficLimitBytes)
	}
}

// Переопределения из базы важнее окружения: админка меняет их без рестарта.
func TestLoadOverlayBeatsEnvironment(t *testing.T) {
	baseEnv(t)
	t.Setenv("BRAND_NAME", "Из окружения")
	settings, err := Load(map[string]string{"BRAND_NAME": "Из базы"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.BrandName != "Из базы" {
		t.Fatalf("бренд = %q, ждали «Из базы»", settings.BrandName)
	}
}

// Пустое значение в базе не затирает значение из окружения.
func TestLoadOverlayIgnoresEmptyValues(t *testing.T) {
	baseEnv(t)
	t.Setenv("BRAND_NAME", "Из окружения")
	settings, err := Load(map[string]string{"BRAND_NAME": "  "})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.BrandName != "Из окружения" {
		t.Fatalf("бренд = %q, ждали «Из окружения»", settings.BrandName)
	}
}

// Флаги читаются в разной записи: 1, true, yes, on.
func TestLoadBooleanValues(t *testing.T) {
	baseEnv(t)
	for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Setenv("SITE_ENABLED", value)
		settings, err := Load(nil)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !settings.SiteEnabled {
			t.Fatalf("SITE_ENABLED=%q должен быть включён", value)
		}
	}
	for _, value := range []string{"0", "false", "no", "off", "мусор"} {
		t.Setenv("SITE_ENABLED", value)
		settings, err := Load(nil)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if settings.SiteEnabled {
			t.Fatalf("SITE_ENABLED=%q должен быть выключен", value)
		}
	}
}

// Богатые сообщения выключаются флагом.
func TestLoadRichMessagesCanBeDisabled(t *testing.T) {
	baseEnv(t)
	t.Setenv("RICH_MESSAGES_ENABLED", "false")
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.RichMessages {
		t.Fatal("богатые сообщения должны быть выключены")
	}
}

func TestLoadSupportBotIsOptInAndEnvironmentOnly(t *testing.T) {
	baseEnv(t)
	t.Setenv("SUPPORT_BOT_TOKEN", "456:support")
	t.Setenv("SUPPORT_BOT_USERNAME", "@vivid_support")
	t.Setenv("SUPPORT_BOT_ENABLED", "true")
	settings, err := Load(map[string]string{
		"SUPPORT_BOT_TOKEN":    "database-token",
		"SUPPORT_BOT_USERNAME": "database-user",
		"SUPPORT_BOT_ENABLED":  "false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.SupportBotToken != "456:support" {
		t.Fatalf("support bot token was not loaded from environment")
	}
	if settings.SupportBotUsername != "vivid_support" || !settings.SupportBotEnabled {
		t.Fatalf("support bot public settings = username %q enabled %t", settings.SupportBotUsername, settings.SupportBotEnabled)
	}
}

// Числа проверяются на мусор и на минимум.
func TestLoadRejectsBadNumbers(t *testing.T) {
	baseEnv(t)
	t.Setenv("DEFAULT_EXPIRE_DAYS", "много")
	if _, err := Load(nil); err == nil || !strings.Contains(err.Error(), "DEFAULT_EXPIRE_DAYS") {
		t.Fatalf("ждали ошибку про DEFAULT_EXPIRE_DAYS, получили %v", err)
	}
	t.Setenv("DEFAULT_EXPIRE_DAYS", "0")
	if _, err := Load(nil); err == nil {
		t.Fatal("нулевой срок должен отклоняться")
	}
}

// Списки принимают запятую, точку с запятой и перенос строки.
func TestLoadLists(t *testing.T) {
	baseEnv(t)
	t.Setenv("PAYMENT_PROVIDERS", "wata;platega")
	t.Setenv("PAYMENT_METHODS", "card,sbp\ntpay")
	t.Setenv("WATA_ACCESS_TOKEN", "token")
	t.Setenv("PLATEGA_MERCHANT_ID", "merchant")
	t.Setenv("PLATEGA_SECRET", "secret")
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.PaymentProviders) != 2 || settings.PaymentProviders[0] != "wata" {
		t.Fatalf("провайдеры: %+v", settings.PaymentProviders)
	}
	if len(settings.PaymentMethods) != 3 {
		t.Fatalf("способы: %+v", settings.PaymentMethods)
	}
}

// Администраторы читаются списком; мусор молча пропускается.
func TestLoadAdminIDs(t *testing.T) {
	baseEnv(t)
	t.Setenv("ADMIN_IDS", "100, 200,мусор")
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !settings.AdminIDs[100] || !settings.AdminIDs[200] {
		t.Fatalf("администраторы: %+v", settings.AdminIDs)
	}
	if settings.AdminIDs[300] {
		t.Fatalf("лишний администратор: %+v", settings.AdminIDs)
	}
}

// Минимальная сумма платежа хранится в копейках.
func TestLoadMinimumPaymentAmount(t *testing.T) {
	baseEnv(t)
	t.Setenv("MIN_PAYMENT_AMOUNT", "15")
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.MinPaymentAmount != 1500 {
		t.Fatalf("минимальная сумма = %d, ждали 1500", settings.MinPaymentAmount)
	}
}

// Лимит трафика понимает суффиксы.
func TestLoadTrafficLimit(t *testing.T) {
	baseEnv(t)
	t.Setenv("TRAFFIC_LIMIT_BYTES", "100GB")
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.TrafficLimitBytes == nil || *settings.TrafficLimitBytes != 100*(1<<30) {
		t.Fatalf("лимит трафика = %v", settings.TrafficLimitBytes)
	}
	t.Setenv("TRAFFIC_LIMIT_BYTES", "много")
	if _, err := Load(nil); err == nil {
		t.Fatal("мусор в лимите трафика должен отклоняться")
	}
}

func TestParseBytes(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"1024", 1024},
		{"1KB", 1 << 10},
		{"100MB", 100 << 20},
		{"2GB", 2 << 30},
		{"1TB", 1 << 40},
		{"512b", 512},
	}
	for _, test := range cases {
		got, err := ParseBytes(test.raw)
		if err != nil {
			t.Fatalf("ParseBytes(%q): %v", test.raw, err)
		}
		if got != test.want {
			t.Fatalf("ParseBytes(%q) = %d, ждали %d", test.raw, got, test.want)
		}
	}
	for _, raw := range []string{"", "много", "-5", "GB"} {
		if _, err := ParseBytes(raw); err == nil {
			t.Fatalf("ParseBytes(%q) должен был вернуть ошибку", raw)
		}
	}
}

// Звёзды без цены — ошибка на старте, а не в момент оплаты.
func TestLoadStarsRequirePrice(t *testing.T) {
	baseEnv(t)
	t.Setenv("TELEGRAM_STARS_ENABLED", "true")
	if _, err := Load(nil); err == nil {
		t.Fatal("ждали ошибку про цену звёзд")
	}
	t.Setenv("TELEGRAM_STARS_PRICE", "1.5")
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.StarsPrice != 150 {
		t.Fatalf("цена звёзд = %d, ждали 150", settings.StarsPrice)
	}
	if len(settings.PaymentProviders) == 0 || settings.PaymentProviders[0] != "stars" {
		t.Fatalf("звёзды должны стать способом оплаты: %+v", settings.PaymentProviders)
	}
}

// Платёжный шлюз без ключей — ошибка на старте.
func TestLoadRejectsProviderWithoutKeys(t *testing.T) {
	baseEnv(t)
	t.Setenv("PAYMENT_PROVIDERS", "yookassa")
	if _, err := Load(nil); err == nil {
		t.Fatal("ждали ошибку про ключи YooKassa")
	}
	t.Setenv("YOOKASSA_SHOP_ID", "shop")
	t.Setenv("YOOKASSA_SECRET_KEY", "secret")
	if _, err := Load(nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

// Адрес сайта: SITE_BASE_URL по умолчанию равен PUBLIC_BASE_URL.
func TestLoadSiteBaseURL(t *testing.T) {
	baseEnv(t)
	t.Setenv("PUBLIC_BASE_URL", "https://example.com/")
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.PublicBaseURL != "https://example.com" {
		t.Fatalf("публичный адрес = %q", settings.PublicBaseURL)
	}
	if settings.SiteBaseURL != "https://example.com" {
		t.Fatalf("адрес сайта = %q", settings.SiteBaseURL)
	}
	t.Setenv("SITE_BASE_URL", "https://cabinet.example.com")
	settings, err = Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.SiteBaseURL != "https://cabinet.example.com" {
		t.Fatalf("адрес сайта = %q", settings.SiteBaseURL)
	}
}

// Словарь картинок разбирается из JSON; мусор — ошибка.
func TestLoadPhotoURLs(t *testing.T) {
	baseEnv(t)
	t.Setenv("PHOTO_URLS", `{"main":"https://example.com/a.png"}`)
	settings, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.PhotoURLs["main"] != "https://example.com/a.png" {
		t.Fatalf("картинки: %+v", settings.PhotoURLs)
	}
	t.Setenv("PHOTO_URLS", "{не json}")
	if _, err := Load(nil); err == nil {
		t.Fatal("битый JSON должен отклоняться")
	}
}
