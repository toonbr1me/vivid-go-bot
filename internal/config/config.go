// Package config собирает настройки из окружения и из переопределений,
// которые администратор сохраняет в базе.
//
// Секреты и параметры подключения остаются в окружении: чтобы прочитать
// override из базы, уже нужно знать DATABASE_URL. Всё остальное — сроки,
// цены, бренд, ссылки — можно менять в админке без перезапуска.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Settings — полный набор настроек процесса.
type Settings struct {
	runtime *runtimeSettings
	// Подключения и секреты (только окружение).
	TelegramToken       string
	DatabaseURL         string
	APIBaseURL          string
	APIToken            string
	RemnawaveNamespace  string
	SubscriptionBaseURL string
	AdminIDs            map[int64]bool

	// Подписка.
	DefaultExpireDays  int
	TrafficLimitBytes  *int64
	ExternalSquadUUID  string
	ExternalSquadName  string
	InternalSquads     []string
	RequestTimeoutSecs float64
	Currency           string
	ExchangeRateNote   string

	// Бренд и контакты.
	BrandName       string
	LegalEntity     string
	SupportEmail    string
	SupportUser     string
	PrivacyURL      string
	TermsURL        string
	ConnectGuideURL string
	PhotoURLs       map[string]string

	// Сайт и мини-приложение.
	SiteEnabled    bool
	WebappEnabled  bool
	PreviewEnabled bool
	// RichMessages — отправлять экраны и ответы богатыми сообщениями
	// Telegram (таблицы, заголовки, кнопки в тексте). При отказе сервера
	// бот сам возвращается к обычному HTML.
	RichMessages       bool
	BotStyle           string
	BotHTMLTemplate    string
	BotHTMLTheme       string
	BotCleanChat       bool
	RecurringEnabled   bool
	RecurringProviders []string
	PublicBaseURL      string
	SiteBaseURL        string
	WebhookHost        string
	WebhookPort        int
	GoogleClientID     string
	GoogleSecret       string
	BotUsername        string
	// Timezone — часовой пояс сервиса: по нему бот здоровается («доброй
	// ночи» в три часа ночи по вашему времени). Пусто — время сервера.
	Timezone string

	// Платежи.
	PaymentProviders  []string
	PaymentMethods    []string
	LinkTTLMinutes    int
	SuccessRedirect   string
	FailRedirect      string
	MinPaymentAmount  Money
	RefBalancePercent int
	ReferralMode      string

	WataBaseURL      string
	WataToken        string
	WataPublicKeyURL string
	WataMethodTokens map[string]string

	PlategaBaseURL  string
	PlategaMerchant string
	PlategaSecret   string

	YooKassaBaseURL string
	YooKassaShopID  string
	YooKassaSecret  string

	StarsEnabled  bool
	StarsPrice    Money
	StarsPriceRaw string

	// Пробные периоды.
	TrialWebDays      int
	TrialBotDays      int
	TrialWebSquads    []string
	TrialBotSquads    []string
	TrialTrafficBytes *int64
	TrialResetOnPaid  bool

	// Устройства.
	DeviceLimitEnabled bool
	DeviceLimitBase    int

	// Поддержка и уведомления.
	SupportCompensationDailyDays int
	PromoMaxCodesPerUser         int
	RemindUnpaidHours            int
	RemindNotConnectedHours      int
	RemnawaveWebhookSecret       string

	LLMEnabled    bool
	LLMProvider   string
	LLMURL        string
	LLMModel      string
	LLMAPIKey     string
	LLMTimeout    int
	LLMMaxReplies int
	LLMShare      int

	// Куда класть пользовательские шаблоны и тему.
	WebDir string
}

// Money — сумма в копейках для настроек.
type Money = domain.Money

// Source — окружение плюс наложенные переопределения.
type Source struct {
	overlay map[string]string
}

// NewSource создаёт источник. overlay может быть nil.
func NewSource(overlay map[string]string) *Source {
	return &Source{overlay: overlay}
}

func (s *Source) raw(name string) (string, bool) {
	if s.overlay != nil && Editable(name) {
		if value, ok := s.overlay[name]; ok && strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func (s *Source) optional(name string) string {
	value, _ := s.raw(name)
	return value
}

func (s *Source) require(name string) (string, error) {
	value, ok := s.raw(name)
	if !ok {
		return "", fmt.Errorf("обязательная настройка %s не задана", name)
	}
	return value, nil
}

func (s *Source) int(name string, def int, minimum int) (int, error) {
	value, ok := s.raw(name)
	if !ok {
		return def, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: ожидалось целое число, получено %q", name, value)
	}
	if parsed < minimum {
		return 0, fmt.Errorf("%s: значение %d меньше минимума %d", name, parsed, minimum)
	}
	return parsed, nil
}

func (s *Source) float(name string, def float64, minimum float64) (float64, error) {
	value, ok := s.raw(name)
	if !ok {
		return def, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: ожидалось число, получено %q", name, value)
	}
	if parsed < minimum {
		return 0, fmt.Errorf("%s: значение %v меньше минимума %v", name, parsed, minimum)
	}
	return parsed, nil
}

func (s *Source) boolean(name string, def bool) bool {
	value, ok := s.raw(name)
	if !ok {
		return def
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Source) list(name string) []string {
	value := s.optional(name)
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (s *Source) int64Set(name string) map[int64]bool {
	out := map[int64]bool{}
	for _, part := range s.list(name) {
		parsed, err := strconv.ParseInt(part, 10, 64)
		if err == nil {
			out[parsed] = true
		}
	}
	return out
}

// bytes разбирает "100GB", "512MB", "1024" в байты. Пустое значение — nil.
func (s *Source) bytes(name string) (*int64, error) {
	value, ok := s.raw(name)
	if !ok {
		return nil, nil
	}
	parsed, err := ParseBytes(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &parsed, nil
}

// ParseBytes понимает суффиксы B/KB/MB/GB/TB (двоичные).
func ParseBytes(raw string) (int64, error) {
	text := strings.ToUpper(strings.TrimSpace(raw))
	if text == "" {
		return 0, errors.New("пустой размер")
	}
	suffixes := []struct {
		suffix     string
		multiplier int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
	}
	for _, item := range suffixes {
		if strings.HasSuffix(text, item.suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(text, item.suffix))
			if number == "" {
				return 0, errors.New("нет числа перед суффиксом")
			}
			value, err := strconv.ParseFloat(number, 64)
			if err != nil {
				return 0, fmt.Errorf("размер %q: %w", raw, err)
			}
			if value < 0 {
				return 0, errors.New("размер не может быть отрицательным")
			}
			return int64(value * float64(item.multiplier)), nil
		}
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("размер %q: %w", raw, err)
	}
	if value < 0 {
		return 0, errors.New("размер не может быть отрицательным")
	}
	return value, nil
}

func (s *Source) jsonDict(name string) (map[string]string, error) {
	value := s.optional(name)
	if value == "" {
		return map[string]string{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(value), &data); err != nil {
		return nil, fmt.Errorf("%s: ожидался JSON-объект: %w", name, err)
	}
	out := make(map[string]string, len(data))
	for key, item := range data {
		out[key] = fmt.Sprint(item)
	}
	return out, nil
}

// Load собирает настройки. overlay может содержать значения из app_settings.
func Load(overlay map[string]string) (*Settings, error) {
	env := NewSource(overlay)

	telegramToken, err := env.require("BOT_TOKEN")
	if err != nil {
		return nil, err
	}
	// Панель необязательна: без неё сервис не выдаёт подписку, но сайт,
	// бот и очередь обращений работают. Старт без панели — обычное дело
	// при развёртывании и проверке бота.
	apiBaseURL := env.optional("API_BASE_URL")
	apiToken := env.optional("API_TOKEN")
	databaseURL, err := env.require("DATABASE_URL")
	if err != nil {
		return nil, err
	}

	settings := &Settings{
		TelegramToken:       telegramToken,
		DatabaseURL:         databaseURL,
		APIBaseURL:          strings.TrimRight(apiBaseURL, "/"),
		APIToken:            apiToken,
		RemnawaveNamespace:  strings.TrimSpace(os.Getenv("REMNAWAVE_NAMESPACE")),
		SubscriptionBaseURL: strings.TrimSpace(os.Getenv("SUBSCRIPTION_BASE_URL")),
		AdminIDs:            env.int64Set("ADMIN_IDS"),

		Currency:          fallback(env.optional("CURRENCY"), "RUB"),
		BrandName:         fallback(env.optional("BRAND_NAME"), "Vivid"),
		LegalEntity:       env.optional("LEGAL_ENTITY"),
		SupportEmail:      env.optional("SUPPORT_EMAIL"),
		SupportUser:       strings.TrimPrefix(env.optional("SUPPORT_USERNAME"), "@"),
		PrivacyURL:        env.optional("PRIVACY_POLICY_URL"),
		TermsURL:          env.optional("TERMS_URL"),
		ConnectGuideURL:   env.optional("CONNECT_GUIDE_URL"),
		ExternalSquadName: env.optional("EXTERNAL_SQUAD_NAME"),
		ExternalSquadUUID: env.optional("EXTERNAL_SQUAD_UUID"),
		InternalSquads:    env.list("INTERNAL_SQUAD_UUIDS"),

		SiteEnabled:        env.boolean("SITE_ENABLED", true),
		WebappEnabled:      env.boolean("WEBAPP_ENABLED", true),
		PreviewEnabled:     env.boolean("PREVIEW_ENABLED", true),
		RichMessages:       env.boolean("RICH_MESSAGES_ENABLED", true),
		BotStyle:           strings.ToLower(fallback(env.optional("BOT_STYLE"), fallback(env.optional("STYLE"), "legacy"))),
		BotHTMLTemplate:    fallback(env.optional("BOT_HTML_TEMPLATE"), "default"),
		BotHTMLTheme:       fallback(env.optional("BOT_HTML_THEME"), "telegram"),
		BotCleanChat:       env.boolean("BOT_CLEAN_CHAT", true),
		RecurringEnabled:   env.boolean("PAYMENTS_RECURRING_ENABLED", false),
		RecurringProviders: env.list("PAYMENTS_RECURRING_PROVIDERS"),
		BotUsername:        strings.TrimPrefix(env.optional("BOT_USERNAME"), "@"),
		Timezone:           env.optional("TIMEZONE"),
		GoogleClientID:     env.optional("GOOGLE_CLIENT_ID"),
		GoogleSecret:       env.optional("GOOGLE_CLIENT_SECRET"),

		PaymentProviders: env.list("PAYMENT_PROVIDERS"),
		PaymentMethods:   env.list("PAYMENT_METHODS"),
		ReferralMode:     fallback(env.optional("REFERRAL_MODE"), "days"),
		SuccessRedirect:  env.optional("SUCCESS_REDIRECT_URL"),
		FailRedirect:     env.optional("FAIL_REDIRECT_URL"),
		WataBaseURL:      fallback(env.optional("WATA_API_BASE_URL"), "https://api.wata.pro/api/h2h"),
		WataToken:        env.optional("WATA_ACCESS_TOKEN"),
		WataPublicKeyURL: fallback(env.optional("WATA_PUBLIC_KEY_URL"), "https://api.wata.pro/api/h2h/public-key"),
		WataMethodTokens: map[string]string{},
		PlategaBaseURL:   fallback(env.optional("PLATEGA_API_BASE_URL"), "https://app.platega.io"),
		PlategaMerchant:  env.optional("PLATEGA_MERCHANT_ID"),
		PlategaSecret:    env.optional("PLATEGA_SECRET"),
		YooKassaBaseURL:  fallback(env.optional("YOOKASSA_API_BASE_URL"), "https://api.yookassa.ru/v3"),
		YooKassaShopID:   env.optional("YOOKASSA_SHOP_ID"),
		YooKassaSecret:   env.optional("YOOKASSA_SECRET_KEY"),
		StarsPriceRaw:    env.optional("TELEGRAM_STARS_PRICE"),

		TrialWebSquads: env.list("TRIAL_WEB_INTERNAL_SQUAD_UUIDS"),
		TrialBotSquads: env.list("TRIAL_BOT_INTERNAL_SQUAD_UUIDS"),

		RemnawaveWebhookSecret: strings.TrimSpace(os.Getenv("REMNAWAVE_WEBHOOK_SECRET")),
		LLMProvider:            fallback(env.optional("SUPPORT_LLM_PROVIDER"), "ollama"),
		LLMURL:                 fallback(env.optional("SUPPORT_LLM_URL"), "http://127.0.0.1:11434"),
		LLMModel:               env.optional("SUPPORT_LLM_MODEL"),
		LLMAPIKey:              env.optional("SUPPORT_LLM_API_KEY"),
		WebDir:                 fallback(env.optional("WEB_DIR"), "web"),
	}

	if settings.PaymentMethods == nil {
		settings.PaymentMethods = []string{"card"}
	}
	for _, method := range []string{"card", "sbp", "tpay", "sberpay"} {
		if token := env.optional("WATA_ACCESS_TOKEN_" + strings.ToUpper(method)); token != "" {
			settings.WataMethodTokens[method] = token
		}
	}

	settings.StarsEnabled = env.boolean("TELEGRAM_STARS_ENABLED", false)
	if settings.StarsEnabled && !contains(settings.PaymentProviders, "stars") {
		settings.PaymentProviders = append(settings.PaymentProviders, "stars")
	}

	// Числовые настройки.
	type intSpec struct {
		name    string
		target  *int
		def     int
		minimum int
	}
	specs := []intSpec{
		{"DEFAULT_EXPIRE_DAYS", &settings.DefaultExpireDays, 30, 1},
		{"WEBHOOK_PORT", &settings.WebhookPort, 8080, 1},
		{"PAYMENT_LINK_EXPIRATION_MINUTES", &settings.LinkTTLMinutes, 4320, 10},
		{"REFERRAL_BALANCE_PERCENT", &settings.RefBalancePercent, 10, 0},
		{"TRIAL_WEB_DAYS", &settings.TrialWebDays, 1, 1},
		{"TRIAL_BOT_DAYS", &settings.TrialBotDays, 7, 1},
		{"DEVICE_LIMIT_BASE", &settings.DeviceLimitBase, 5, 1},
		{"SUPPORT_COMPENSATION_DAILY_DAYS", &settings.SupportCompensationDailyDays, 14, 0},
		{"PROMO_MAX_CODES_PER_USER", &settings.PromoMaxCodesPerUser, 3, 0},
		{"REMIND_UNPAID_INVOICE_HOURS", &settings.RemindUnpaidHours, 0, 0},
		{"REMIND_NOT_CONNECTED_HOURS", &settings.RemindNotConnectedHours, 0, 0},
		{"SUPPORT_LLM_TIMEOUT", &settings.LLMTimeout, 30, 1},
		{"SUPPORT_LLM_MAX_REPLIES", &settings.LLMMaxReplies, 2, 1},
		{"SUPPORT_LLM_SHARE", &settings.LLMShare, 100, 0},
	}
	for _, spec := range specs {
		value, err := env.int(spec.name, spec.def, spec.minimum)
		if err != nil {
			return nil, err
		}
		*spec.target = value
	}

	settings.RequestTimeoutSecs, err = env.float("REQUEST_TIMEOUT", 10, 1)
	if err != nil {
		return nil, err
	}
	settings.TrafficLimitBytes, err = env.bytes("TRAFFIC_LIMIT_BYTES")
	if err != nil {
		return nil, err
	}
	settings.TrialTrafficBytes, err = env.bytes("TRIAL_TRAFFIC_LIMIT_BYTES")
	if err != nil {
		return nil, err
	}
	settings.DeviceLimitEnabled = env.boolean("DEVICE_LIMIT_ENABLED", true)
	settings.TrialResetOnPaid = env.boolean("TRIAL_RESET_TRAFFIC_ON_PAID", true)
	settings.LLMEnabled = env.boolean("SUPPORT_LLM_ENABLED", false)

	minAmount, err := env.int("MIN_PAYMENT_AMOUNT", 10, 0)
	if err != nil {
		return nil, err
	}
	settings.MinPaymentAmount = Money(minAmount) * 100

	settings.PhotoURLs, err = env.jsonDict("PHOTO_URLS")
	if err != nil {
		return nil, err
	}

	publicBase := strings.TrimRight(env.optional("PUBLIC_BASE_URL"), "/")
	settings.PublicBaseURL = publicBase
	siteBase := strings.TrimRight(env.optional("SITE_BASE_URL"), "/")
	if siteBase == "" {
		siteBase = publicBase
	}
	settings.SiteBaseURL = siteBase
	settings.WebhookHost = fallback(env.optional("WEBHOOK_HOST"), "0.0.0.0")

	if settings.StarsEnabled {
		if settings.StarsPriceRaw == "" {
			return nil, errors.New("TELEGRAM_STARS_PRICE обязателен при включённых звёздах")
		}
		parsed, err := parseMoney(settings.StarsPriceRaw)
		if err != nil || parsed <= 0 {
			return nil, errors.New("TELEGRAM_STARS_PRICE должен быть положительным числом")
		}
		settings.StarsPrice = parsed
	}

	// Проверки, которые ловят опечатку при старте, а не при первом платеже.
	for _, provider := range settings.PaymentProviders {
		switch provider {
		case "wata":
			if settings.WataToken == "" {
				return nil, errors.New("WATA_ACCESS_TOKEN обязателен при 'wata' в PAYMENT_PROVIDERS")
			}
		case "platega":
			if settings.PlategaMerchant == "" || settings.PlategaSecret == "" {
				return nil, errors.New("PLATEGA_MERCHANT_ID и PLATEGA_SECRET обязательны при 'platega'")
			}
		case "yookassa":
			if settings.YooKassaShopID == "" || settings.YooKassaSecret == "" {
				return nil, errors.New("YOOKASSA_SHOP_ID и YOOKASSA_SECRET_KEY обязательны при 'yookassa'")
			}
		}
	}
	if settings.BotStyle != "legacy" && settings.BotStyle != "html" {
		return nil, errors.New("BOT_STYLE: legacy или html")
	}
	if settings.BotHTMLTheme != "telegram" && settings.BotHTMLTheme != "custom" {
		return nil, errors.New("BOT_HTML_THEME: telegram или custom")
	}
	for _, r := range settings.BotHTMLTemplate {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return nil, errors.New("BOT_HTML_TEMPLATE: допустимы a-z, 0-9, дефис и подчёркивание")
		}
	}
	for _, p := range settings.RecurringProviders {
		if p != "yookassa" && p != "platega" {
			return nil, errors.New("рекуррентные платежи: yookassa или platega")
		}
	}
	return settings, nil
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

// AssistantEnabled — помощник поддержки настроен и включён.
//
// Модель без адреса или без имени работать не будет: считать помощника
// включённым при такой настройке значит обещать человеку ответы, которых
// не будет.
func (s *Settings) AssistantEnabled() bool {
	return s.LLMEnabled && s.LLMModel != "" && s.LLMURL != ""
}

func contains(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func parseMoney(raw string) (Money, error) {
	text := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	parts := strings.SplitN(text, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	cents := int64(0)
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) > 2 {
			frac = frac[:2]
		}
		for len(frac) < 2 {
			frac += "0"
		}
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return Money(whole*100 + cents), nil
}
