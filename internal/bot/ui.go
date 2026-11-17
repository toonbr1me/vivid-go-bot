package bot

import (
	"strconv"
	"strings"

	"github.com/mymmrac/telego"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// ---------------------------------------------------------------------------
// Мелкие конструкторы кнопок: клавиатуры собираются рядами, и повторять
// структуру telego в каждом экране незачем.
// ---------------------------------------------------------------------------

func button(text, callback string) telego.InlineKeyboardButton {
	return telego.InlineKeyboardButton{Text: text}.WithCallbackData(callback)
}

func urlButton(text, url string) telego.InlineKeyboardButton {
	return telego.InlineKeyboardButton{Text: text}.WithURL(url)
}

func copyButton(text, payload string) telego.InlineKeyboardButton {
	return telego.InlineKeyboardButton{Text: text}.WithCopyText(&telego.CopyTextButton{Text: payload})
}

func keyboard(rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{InlineKeyboard: usableRows(rows)}
}

// usableRows убирает кнопки, которые Telegram не примет.
//
// Одна кнопка с негодным адресом отменяет всё сообщение: Telegram требует
// https (или tg) для ссылок в кнопках, а локальный адрес сайта вроде
// http://localhost такой ссылкой не является. Лучше не показать кнопку,
// чем не показать экран: остальные кнопки и текст доходят.
func usableRows(rows [][]telego.InlineKeyboardButton) [][]telego.InlineKeyboardButton {
	out := make([][]telego.InlineKeyboardButton, 0, len(rows))
	for _, row := range rows {
		clean := make([]telego.InlineKeyboardButton, 0, len(row))
		for _, item := range row {
			if item.URL != "" && !telegramURL(item.URL) {
				continue
			}
			if item.Style == "" {
				item.Style = actionStyle(item.CallbackData)
			}
			clean = append(clean, item)
		}
		if len(clean) == 0 {
			continue
		}
		out = append(out, clean)
	}
	return out
}

func actionStyle(action string) string {
	switch {
	case strings.HasPrefix(action, "dev:confirmrm:"), strings.HasPrefix(action, "ticket:close:"), strings.HasPrefix(action, "promo:clear"):
		return "danger"
	case action == "nav:buy", action == "nav:trial", strings.HasPrefix(action, "pay:"), strings.HasPrefix(action, "payb:"), strings.HasPrefix(action, "check:"):
		return "success"
	case action == "nav:connect", strings.HasPrefix(action, "plan:"):
		return "primary"
	default:
		return ""
	}
}

// telegramURL — адрес, который Telegram готов открыть в кнопке.
func telegramURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "tg://")
}

func (b *Bot) backRow() []telego.InlineKeyboardButton {
	return []telego.InlineKeyboardButton{button(b.texts.Buttons.Back, "nav:main")}
}

// backRow — кнопка «назад» для мест, где настроек ещё нет: свободные
// функции экрана оплаты. Текст совпадает со встроенным.
func backRow() []telego.InlineKeyboardButton {
	return []telego.InlineKeyboardButton{button("← Главное меню", "nav:main")}
}

// siteURL — адрес сайта, если он включён и настроен.
func (b *Bot) siteURL() string {
	if !b.cfg.Current().SiteEnabled {
		return ""
	}
	return strings.TrimRight(b.cfg.Current().SiteBaseURL, "/")
}

func (b *Bot) legalURLs() (terms, privacy string) {
	site := b.siteURL()
	terms = b.cfg.Current().TermsURL
	if terms == "" && site != "" {
		terms = site + "/terms"
	}
	privacy = b.cfg.Current().PrivacyURL
	if privacy == "" && site != "" {
		privacy = site + "/privacy"
	}
	return terms, privacy
}

func (b *Bot) referralLink(subject domain.Subject) string {
	if subject.RefCode == nil || *subject.RefCode == "" {
		return ""
	}
	if b.cfg.Current().BotUsername == "" {
		return ""
	}
	return "https://t.me/" + b.cfg.Current().BotUsername + "?start=ref_" + *subject.RefCode
}

// happURL — https-страница-мостик: Telegram не принимает happ:// в кнопках,
// а на Windows deep link режется ещё и системой.
func (b *Bot) happURL(subscriptionURL string) string {
	site := b.siteURL()
	if site == "" || !strings.HasPrefix(subscriptionURL, "http") {
		return ""
	}
	return site + "/happ?u=" + queryEscape(subscriptionURL)
}

// queryEscape — минимальное кодирование для параметра u.
func queryEscape(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			out.WriteRune(r)
		default:
			for _, raw := range []byte(string(r)) {
				const hex = "0123456789ABCDEF"
				out.WriteByte('%')
				out.WriteByte(hex[raw>>4])
				out.WriteByte(hex[raw&0x0F])
			}
		}
	}
	return out.String()
}

// ---------------------------------------------------------------------------
// Главное меню
// ---------------------------------------------------------------------------

func (b *Bot) mainKeyboard(status string, trialAvailable bool) *telego.InlineKeyboardMarkup {
	return b.buildMenu(status, menuConditions{
		Payments: b.pay.Registry().Enabled(),
		Trial:    trialAvailable,
		Devices:  b.cfg.Current().DeviceLimitEnabled,
	})
}

// ---------------------------------------------------------------------------
// Экран оплаты
// ---------------------------------------------------------------------------

func (b *Bot) subscriptionKeyboard(access domain.Access) *telego.InlineKeyboardMarkup {
	var rows [][]telego.InlineKeyboardButton
	if access.URL != "" {
		if happ := b.happURL(access.URL); happ != "" {
			rows = append(rows, []telego.InlineKeyboardButton{
				urlButton(b.texts.Buttons.Happ, happ),
			})
		}
		rows = append(rows, []telego.InlineKeyboardButton{
			copyButton(b.texts.Buttons.CopyLink, access.URL),
			urlButton(b.texts.Buttons.OpenPage, access.URL),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button(b.texts.Buttons.Renew, "nav:buy"),
	})
	rows = append(rows, []telego.InlineKeyboardButton{
		button(b.texts.Buttons.ConnectHow, "nav:connect"),
	}, b.backRow())
	return keyboard(rows...)
}

func (b *Bot) paymentRows(prefix, id string, options []domain.PaymentOption) [][]telego.InlineKeyboardButton {
	var rows [][]telego.InlineKeyboardButton
	for _, option := range options {
		label := option.ProviderLabel + " · " + option.MethodLabel
		rows = append(rows, []telego.InlineKeyboardButton{
			button(label, prefix+id+":"+option.Token()),
		})
	}
	return rows
}

// ---------------------------------------------------------------------------
// Остальные разделы
// ---------------------------------------------------------------------------

func (b *Bot) profileKeyboard(hasPromo bool, payments bool) *telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{
		{button(b.texts.Buttons.History, "nav:history")},
		{button(b.texts.Buttons.Referral, "nav:referral")},
	}
	row := []telego.InlineKeyboardButton{button(b.texts.Buttons.Settings, "nav:settings")}
	if payments {
		label := b.texts.Buttons.Promo
		if hasPromo {
			label = b.texts.Buttons.Promo + " · применён"
		}
		row = append([]telego.InlineKeyboardButton{button(label, "nav:promo")}, row...)
	}
	rows = append(rows, row)
	rows = append(rows, b.backRow())
	return keyboard(rows...)
}

func (b *Bot) settingsKeyboard(settings storage.NotificationSettings) *telego.InlineKeyboardMarkup {
	toggle := func(enabled bool, label, data string) telego.InlineKeyboardButton {
		if enabled {
			return button(b.texts.Buttons.Disable+" "+label, data)
		}
		return button(b.texts.Buttons.Enable+" "+label, data)
	}
	rows := [][]telego.InlineKeyboardButton{
		{toggle(settings.Expiry, b.texts.Settings.Expiry, "settings:expiry")},
	}
	if b.activityNotificationsEnabled() {
		rows = append(rows, []telego.InlineKeyboardButton{
			toggle(settings.Activity, b.texts.Settings.Activity, "settings:activity"),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.Profile, "nav:profile"),
		button(b.texts.Buttons.Menu, "nav:main"),
	})
	return keyboard(rows...)
}

func (b *Bot) activityNotificationsEnabled() bool {
	return b.cfg.Current().RemindUnpaidHours > 0 || b.cfg.Current().RemindNotConnectedHours > 0
}

func (b *Bot) infoKeyboard() *telego.InlineKeyboardMarkup {
	var rows [][]telego.InlineKeyboardButton
	if site := b.siteURL(); site != "" {
		rows = append(rows, []telego.InlineKeyboardButton{
			urlButton(b.texts.Buttons.Website, site),
		})
		rows = append(rows, []telego.InlineKeyboardButton{
			urlButton(b.texts.Buttons.Support, site+"/support"),
		})
	}
	terms, privacy := b.legalURLs()
	var legal []telego.InlineKeyboardButton
	if privacy != "" {
		legal = append(legal, urlButton(b.texts.Buttons.Privacy, privacy))
	}
	if terms != "" {
		legal = append(legal, urlButton(b.texts.Buttons.Terms, terms))
	}
	if len(legal) > 0 {
		rows = append(rows, legal)
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button(b.texts.Buttons.Support, "nav:support"),
	})
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.More, "nav:more"),
		button(b.texts.Buttons.Menu, "nav:main"),
	})
	return keyboard(rows...)
}

func (b *Bot) helpKeyboard() *telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{
		{button(b.texts.Buttons.Support, "nav:support")},
	}
	terms, privacy := b.legalURLs()
	var legal []telego.InlineKeyboardButton
	if privacy != "" {
		legal = append(legal, urlButton(b.texts.Buttons.Privacy, privacy))
	}
	if terms != "" {
		legal = append(legal, urlButton(b.texts.Buttons.Terms, terms))
	}
	if len(legal) > 0 {
		rows = append(rows, legal)
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.More, "nav:more"),
		button(b.texts.Buttons.Menu, "nav:main"),
	})
	return keyboard(rows...)
}

func (b *Bot) moreKeyboard() *telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{
		{button("Профиль и баланс", "nav:profile"), button("Пригласить друга", "nav:referral")},
		{button("Настройки", "nav:settings")},
		{button(b.texts.Buttons.Info, "nav:info")},
		{button(b.texts.Buttons.Help, "nav:help")},
		{button(b.texts.Buttons.Connect, "nav:connect")},
	}
	rows = append(rows, b.backRow())
	return keyboard(rows...)
}

func (b *Bot) connectKeyboard() *telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{
		{
			urlButton("🍏 Happ · iOS/Mac", "https://apps.apple.com/app/happ-proxy-utility/id6504287215"),
			urlButton("🤖 Happ · Android", "https://play.google.com/store/apps/details?id=com.happproxy"),
		},
		{
			urlButton("💻 Happ · Windows", "https://github.com/Happ-proxy/happ-desktop/releases/latest/download/setup-Happ.x64.exe"),
		},
		{
			urlButton("🍏 v2rayTun · iOS", "https://apps.apple.com/app/v2raytun/id6476628951"),
			urlButton("🤖 v2rayTun · Android", "https://play.google.com/store/apps/details?id=com.v2raytun.android"),
		},
		{
			urlButton("🍏 Incy · iOS", "https://apps.apple.com/app/incy/id6756943388"),
			urlButton("🤖 Incy · Android", "https://play.google.com/store/apps/details?id=llc.itdev.incy"),
		},
	}
	if b.cfg.Current().ConnectGuideURL != "" {
		rows = append(rows, []telego.InlineKeyboardButton{
			urlButton(b.texts.Buttons.Guide, b.cfg.Current().ConnectGuideURL),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.MySubscription, "nav:subscription"),
		button(b.texts.Buttons.Menu, "nav:main"),
	})
	return keyboard(rows...)
}

func (b *Bot) supportKeyboard(open *storage.Ticket, staff bool) *telego.InlineKeyboardMarkup {
	var rows [][]telego.InlineKeyboardButton
	rows = append(rows, []telego.InlineKeyboardButton{
		button(b.texts.Buttons.NewTicket, "support:new"),
	})
	if open != nil {
		rows = append(rows, []telego.InlineKeyboardButton{
			button(b.texts.Buttons.ContinueTicket, "ticket:"+strconv.FormatInt(open.ID, 10)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button(b.texts.Buttons.MyTickets, "support:list"),
	})
	if staff {
		rows = append(rows, []telego.InlineKeyboardButton{
			button(b.texts.Buttons.Queue, "admin:tickets"),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.Profile, "nav:profile"),
		button(b.texts.Buttons.Menu, "nav:main"),
	})
	return keyboard(rows...)
}

func (b *Bot) referralKeyboard(link, mode string) *telego.InlineKeyboardMarkup {
	var rows [][]telego.InlineKeyboardButton
	if link != "" {
		rows = append(rows, []telego.InlineKeyboardButton{
			copyButton(b.texts.Buttons.CopyReferral, link),
		})
	}
	next := "balance"
	if mode == "balance" {
		next = "days"
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button(b.text(b.texts.Buttons.ReferralMode, map[string]any{"Mode": referralModeLabel(mode), "Next": referralModeLabel(next)}), "ref:mode:"+next),
	})
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.Profile, "nav:profile"),
		button(b.texts.Buttons.Menu, "nav:main"),
	})
	return keyboard(rows...)
}
