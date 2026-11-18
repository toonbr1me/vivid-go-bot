package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mymmrac/telego"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
)

// ---------------------------------------------------------------------------
// Кнопки внутри сообщения
// ---------------------------------------------------------------------------

// richButtonRows переносит обычную клавиатуру в текст rich-сообщения.
//
// false означает, что хотя бы одна кнопка не выражается rich-тегом: тогда
// клавиатура остаётся обычной, иначе часть действий потерялась бы.
func richButtonRows(kb *telego.InlineKeyboardMarkup) (string, bool) {
	if kb == nil || len(kb.InlineKeyboard) == 0 {
		return "", false
	}
	rows := make([]string, 0, len(kb.InlineKeyboard))
	for _, row := range kb.InlineKeyboard {
		buttons := make([]string, 0, len(row))
		for _, item := range row {
			tag, ok := richButton(item)
			if !ok {
				return "", false
			}
			buttons = append(buttons, tag)
		}
		if len(buttons) == 0 {
			continue
		}
		rows = append(rows, "<tg-button-row>"+strings.Join(buttons, "")+"</tg-button-row>")
	}
	if len(rows) == 0 {
		return "", false
	}
	return strings.Join(rows, "\n"), true
}

func richButton(item telego.InlineKeyboardButton) (string, bool) {
	if strings.TrimSpace(item.Text) == "" {
		return "", false
	}
	if item.URL != "" && !telegramURL(item.URL) {
		// Такой адрес Telegram не примет: кнопку лучше не встраивать.
		return "", false
	}
	style := ""
	switch item.Style {
	case "danger", "success", "primary", "link":
		style = ` style="` + item.Style + `"`
	}
	label := escapeHTML(item.Text)
	switch {
	case item.CallbackData != "":
		return `<tg-button type="callback_data"` + style + ` data="` + richAttr(item.CallbackData) + `">` + label + "</tg-button>", true
	case item.URL != "":
		return `<tg-button type="url"` + style + ` url="` + richAttr(item.URL) + `">` + label + "</tg-button>", true
	case item.CopyText != nil:
		return `<tg-button type="copy_text"` + style + ` text="` + richAttr(item.CopyText.Text) + `">` + label + "</tg-button>", true
	}
	return "", false
}

var richAttrReplacer = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
)

func richAttr(value string) string { return richAttrReplacer.Replace(value) }

// ---------------------------------------------------------------------------
// Экраны
// ---------------------------------------------------------------------------

// showScreen рисует раздел: богатым сообщением, если он для этого создан,
// иначе обычным. Rich-сообщение — основной путь; при отказе Telegram
// показывается прежний экран, поэтому человек в любом случае видит ответ.
func (b *Bot) showScreen(ctx context.Context, chatID telego.ChatID, messageID int, name string, tgID int64) {
	b.clearInput(tgID)
	mode := b.viewMode(ctx, chatID.ID)
	if mode == "cards" {
		if state, ok := b.interfaceScreen(ctx, tgID, name); ok {
			if err := b.deliverUI(ctx, tgID, messageID, state); err == nil {
				return
			} else {
				slog.Warn("интерактивный экран недоступен", "screen", name, "err", err)
				id, _, _ := b.db.BotCard(ctx, tgID)
				if id == messageID && messageID > 0 {
					return
				}
				_, _ = b.api.SendMessage(ctx, &telego.SendMessageParams{ChatID: chatID, Text: "Не удалось открыть HTML-карточку. Повторите попытку; выбранный вид сохранён."})
				return
			}
		}
	}
	if mode == "classic" {
		if b.richAvailable() {
			if html, kb, ok := b.standardRichScreen(ctx, tgID, name); ok {
				if delivered, _ := b.deliverRich(ctx, chatID, messageID, html, kb); delivered {
					return
				}
			}
		}
		if text, kb, ok := b.compactClassic(ctx, tgID, name); ok {
			b.upsert(ctx, chatID, messageID, text, kb)
			return
		}
	}
	if b.viewMode(ctx, chatID.ID) == "rich" && b.richAvailable() {
		if html, kb, ok := b.richScreen(ctx, tgID, name); ok {
			if delivered, err := b.deliverRich(ctx, chatID, messageID, html, kb); delivered {
				return
			} else if err != nil {
				slog.Warn("экран не отправлен богатым сообщением, рисую обычный", "screen", name, "err", err)
			}
		}
	}
	text, kb := b.navigationScreen(ctx, tgID, name)
	b.upsert(ctx, chatID, messageID, text, kb)
}

// richScreen отдаёт богатый вариант раздела. false — у раздела его нет.
func (b *Bot) richScreen(ctx context.Context, tgID int64, name string) (string, *telego.InlineKeyboardMarkup, bool) {
	switch name {
	case "main":
		return b.richMain(ctx, tgID)
	case "subscription":
		return b.richSubscription(ctx, tgID)
	case "buy":
		return b.richBuy(ctx, tgID)
	case "devices":
		return b.richDevices(ctx, tgID)
	default:
		return b.standardRichScreen(ctx, tgID, name)
	}
}

// richMain — главный экран: заголовок, таблица состояния, подсказка.
func (b *Bot) richMain(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup, bool) {
	subject, _ := b.subject(ctx, tgID)
	access, err := b.accessFor(ctx, subject)
	trialAvailable := b.trialAvailable(ctx, subject)

	status := "UNKNOWN"
	if err == nil || access.Found {
		switch {
		case !access.Found && access.ExpireAt != nil && access.ExpireAt.After(time.Now()):
			status = "UNKNOWN"
		case access.Found && access.Active:
			status = "ACTIVE"
		case access.ExpireAt != nil:
			status = "EXPIRED"
		default:
			status = "NONE"
		}
	}

	heading := b.cfg.Current().BrandName
	if mascot := strings.TrimSpace(b.texts.Persona.Mascot); mascot != "" {
		heading = mascot + " " + heading
	}
	var out strings.Builder
	out.WriteString("<h3>" + escapeHTML(heading) + "</h3>")

	greeting := b.greeting()
	name := ""
	if subject.Name != nil {
		name = strings.TrimSpace(*subject.Name)
	}
	if greeting != "" || name != "" {
		out.WriteString("<p>")
		if greeting != "" {
			out.WriteString(escapeHTML(greeting))
		}
		if name != "" {
			if greeting != "" {
				out.WriteString(", ")
			}
			out.WriteString("<b>" + escapeHTML(name) + "</b>")
		}
		out.WriteString(".</p>")
	}

	if status == "NONE" && strings.TrimSpace(b.texts.Main.Lead) != "" {
		out.WriteString("<p>" + b.text(b.texts.Main.Lead, nil) + "</p>")
	}
	out.WriteString("<table bordered striped compact>")
	out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Subscription) + "</td><td align=\"right\"><b>" + escapeHTML(b.richStatus(access, status)) + "</b></td></tr>")
	if status == "NONE" {
		out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.PaymentLabel) + "</td><td align=\"right\">" + escapeHTML(b.texts.Rich.PaymentOnce) + "</td></tr>")
	} else {
		if access.ExpireAt != nil {
			out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Until) + "</td><td align=\"right\">" + escapeHTML(b.richExpire(access.ExpireAt)) + "</td></tr>")
		}
		if label := b.trafficLabel(access); label != "" {
			out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Traffic) + "</td><td align=\"right\">" + escapeHTML(label) + "</td></tr>")
		}
		if access.DeviceLimit > 0 {
			out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Devices) + "</td><td align=\"right\">" + escapeHTML(b.richDeviceCount(access)) + "</td></tr>")
		}
	}
	out.WriteString("</table>")

	hint := b.texts.Rich.HintNone
	switch status {
	case "ACTIVE":
		hint = b.texts.Rich.HintActive
	case "EXPIRED":
		hint = b.texts.Rich.HintExpired
	case "UNKNOWN":
		hint = b.texts.Rich.PanelDown
	}
	if hint != "" {
		out.WriteString("\n<p>" + escapeHTML(hint) + "</p>")
	}
	if status == "NONE" && trialAvailable && b.cfg.Current().TrialBotDays > 0 {
		trial := b.text(b.texts.Rich.TrialHint, map[string]any{
			"Days": b.cfg.Current().TrialBotDays, "DaysWord": daysWord(b.cfg.Current().TrialBotDays),
		})
		out.WriteString("\n<blockquote>" + trial + "</blockquote>")
	}
	if status != "UNKNOWN" && b.texts.Rich.AutoRenewNo != "" {
		out.WriteString("\n<footer>" + escapeHTML(b.customerRecurringFooter(ctx, subject)) + "</footer>")
	}
	kb := b.mainKeyboard(status, trialAvailable)
	if guidance, e := b.pay.CustomerGuidance(ctx, subject, access, "bot"); e == nil && (status != "UNKNOWN" || guidance.Action == "order") {
		out.WriteString("<p>" + escapeHTML(guidance.Detail) + "</p>")
		action := customerGuidanceAction(guidance)
		if action != "" {
			kb = primaryCustomerAction(kb, guidance.Label, action)
		}
	}
	_ = b.db.RecordJourney(ctx, subject.ID, "subscription_viewed", "bot", "")
	return out.String(), kb, true
}

func primaryCustomerAction(kb *telego.InlineKeyboardMarkup, label, action string) *telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{{{Text: label, CallbackData: action, Style: "primary"}}}
	if kb != nil {
		for _, row := range kb.InlineKeyboard {
			var kept []telego.InlineKeyboardButton
			for _, item := range row {
				if item.CallbackData != action {
					kept = append(kept, item)
				}
			}
			if len(kept) > 0 {
				rows = append(rows, kept)
			}
		}
	}
	return keyboard(rows...)
}

func customerGuidanceAction(g domain.CustomerGuidance) string {
	if g.Action == "order" {
		return "check:" + g.OrderID
	}
	return map[string]string{"trial": "nav:trial", "buy": "nav:buy", "connect": "nav:connect", "support": "nav:support", "recurring": "nav:profile"}[g.Action]
}

func (b *Bot) customerRecurringFooter(ctx context.Context, subject domain.Subject) string {
	if agreement, err := b.db.LatestRecurringForSubject(ctx, subject.ID); err == nil {
		text := "Автопродление: " + agreement.StatusText()
		if agreement.Enabled && agreement.NextChargeAt != nil {
			text += " · следующее списание " + agreement.NextChargeAt.In(b.timezone()).Format("02.01.2006")
		}
		return text
	}
	return b.texts.Rich.AutoRenewNo
}

// richSubscription — карточка подписки с таблицей.
func (b *Bot) richSubscription(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup, bool) {
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		return "", nil, false
	}
	access, panelErr := b.accessFor(ctx, subject)
	if panelErr != nil && !access.Found {
		return "", nil, false
	}
	if !access.Found && access.ExpireAt == nil {
		return "", nil, false
	}

	status := "UNKNOWN"
	switch {
	case access.Active:
		status = "ACTIVE"
	case access.ExpireAt != nil:
		status = "EXPIRED"
	}

	var out strings.Builder
	out.WriteString("<h3>" + escapeHTML(plainLine(b.texts.Subscription.Title)) + "</h3>")
	out.WriteString("<table bordered striped compact>")
	out.WriteString("<tr><th>" + escapeHTML(b.texts.Rich.Parameter) + "</th><th align=\"right\">" + escapeHTML(b.texts.Rich.Value) + "</th></tr>")
	out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Status) + "</td><td align=\"right\"><b>" + escapeHTML(b.richStatus(access, status)) + "</b></td></tr>")
	if access.ExpireAt != nil {
		out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Until) + "</td><td align=\"right\">" + escapeHTML(b.richExpire(access.ExpireAt)) + "</td></tr>")
	}
	if access.DeviceLimit > 0 || len(access.Devices) > 0 {
		out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Devices) + "</td><td align=\"right\">" + escapeHTML(b.richDeviceCount(access)) + "</td></tr>")
	}
	out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Traffic) + "</td><td align=\"right\">" + escapeHTML(b.richTraffic(access)) + "</td></tr>")
	out.WriteString("</table>")

	if b.texts.Rich.Connect != "" || b.texts.Rich.ConnectHint != "" {
		out.WriteString("\n<p><b>" + escapeHTML(b.texts.Rich.Connect) + "</b>")
		if b.texts.Rich.ConnectHint != "" {
			out.WriteString("<br>" + escapeHTML(b.texts.Rich.ConnectHint))
		}
		out.WriteString("</p>")
	}
	// Ссылка в тексте — запасной путь: кнопку Happ Telegram может не принять,
	// если адрес сайта не https, а подключиться всё равно надо.
	if access.URL != "" {
		out.WriteString("\n<p><code>" + escapeHTML(access.URL) + "</code></p>")
	}
	if panelErr != nil {
		out.WriteString("\n<p>" + escapeHTML(plainLine(b.texts.Subscription.PanelPart)) + "</p>")
	}

	footer := escapeHTML(b.customerRecurringFooter(ctx, subject))
	if access.Username != "" {
		footer = "Профиль: <code>" + escapeHTML(access.Username) + "</code> · " + footer
	}
	if strings.TrimSpace(footer) != "" {
		out.WriteString("\n<footer>" + footer + "</footer>")
	}
	kb := b.subscriptionKeyboard(access)
	if g, e := b.pay.CustomerGuidance(ctx, subject, access, "bot"); e == nil {
		if action := customerGuidanceAction(g); action != "" {
			kb = primaryCustomerAction(kb, g.Label, action)
		}
	}
	return out.String(), kb, true
}

// richBuy — витрина тарифов таблицей.
func (b *Bot) richBuy(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup, bool) {
	subject, _ := b.subject(ctx, tgID)
	quotes, err := b.pay.QuotePlans(ctx, subject.ID, b.currentPromo(tgID))
	if err != nil {
		return "", nil, false
	}
	quotes = sellableQuotes(quotes)
	if len(quotes) == 0 {
		return "", nil, false
	}

	best := domain.BestPriceIDs(planList(quotes))
	var out strings.Builder
	out.WriteString("<h3>" + escapeHTML(plainLine(b.texts.Buy.Title)) + "</h3>")
	if promo := b.currentPromo(tgID); promo != "" {
		out.WriteString("<p>" + b.text(b.texts.Buy.Promo, map[string]any{"Promo": escapeHTML(promo)}) + "</p>")
	}

	out.WriteString("<table bordered striped compact>")
	out.WriteString("<tr><th>" + escapeHTML(b.texts.Rich.Plan) + "</th><th align=\"center\">" + escapeHTML(b.texts.Rich.Term) + "</th><th align=\"right\">" + escapeHTML(b.texts.Rich.Price) + "</th></tr>")
	for _, quote := range quotes {
		title := escapeHTML(quote.Plan.Title)
		if best[quote.Plan.ID] {
			title += " 🔥"
		}
		price := "<b>" + escapeHTML(priceText(quote.Amount, b.cfg.Current().Currency)) + "</b>"
		if quote.Amount < quote.Price {
			price += " <s>" + escapeHTML(priceText(quote.Price, b.cfg.Current().Currency)) + "</s>"
		}
		term := fmt.Sprintf("%d %s", quote.Plan.Days, daysWord(quote.Plan.Days))
		out.WriteString("<tr><td>" + title + "</td><td align=\"center\">" + escapeHTML(term) + "</td><td align=\"right\">" + price + "</td></tr>")
	}
	out.WriteString("</table>")

	for _, quote := range quotes {
		if quote.Rejection != "" {
			out.WriteString("\n<p>⚠️ <b>" + escapeHTML(quote.Plan.Title) + "</b>: " + escapeHTML(quote.Rejection) + "</p>")
		}
	}
	if b.texts.Rich.ChooseAction != "" {
		out.WriteString("\n<p>" + escapeHTML(b.texts.Rich.ChooseAction) + "</p>")
	}

	var rows [][]telego.InlineKeyboardButton
	var pair []telego.InlineKeyboardButton
	for _, quote := range quotes {
		if quote.Rejection != "" {
			continue
		}
		label := quote.Plan.Title + " · " + priceText(quote.Amount, b.cfg.Current().Currency)
		pair = append(pair, button(label, "plan:"+quote.Plan.ID))
		if len(pair) == 2 {
			rows = append(rows, pair)
			pair = nil
		}
	}
	if len(pair) > 0 {
		rows = append(rows, pair)
	}
	rows = append(rows, b.backRow())
	return out.String(), keyboard(rows...), true
}

// richDevices — лимит, подключённые устройства, слоты и докупка.
func (b *Bot) richDevices(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup, bool) {
	if !b.cfg.Current().DeviceLimitEnabled {
		return "", nil, false
	}
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		return "", nil, false
	}
	access, _ := b.accessFor(ctx, subject)
	now := time.Now().UTC()
	quotes, _ := b.pay.DeviceQuotes(ctx, access.ExpireAt, now)
	slots, _ := b.pay.DeviceSlots(ctx, subject.ID, access.ExpireAt)

	limit := access.DeviceLimit
	if limit <= 0 {
		if managed, err := b.pay.ManagedDeviceLimit(ctx, subject.ID); err == nil {
			limit = managed
		}
	}
	connected := len(access.Devices)

	var out strings.Builder
	out.WriteString("<h3>" + escapeHTML(plainLine(b.texts.Devices.Title)) + "</h3>")
	out.WriteString("<table bordered striped compact>")
	if limit > 0 {
		out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Devices) + "</td><td align=\"right\">" + escapeHTML(fmt.Sprintf("%d / %d", connected, limit)) + "</td></tr>")
	} else {
		out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Devices) + "</td><td align=\"right\">" + escapeHTML(fmt.Sprint(connected)) + "</td></tr>")
	}
	if access.ExpireAt != nil {
		out.WriteString("<tr><td>" + escapeHTML(b.texts.Rich.Until) + "</td><td align=\"right\">" + escapeHTML(dateText(*access.ExpireAt)) + "</td></tr>")
	}
	out.WriteString("</table>")

	var rows [][]telego.InlineKeyboardButton
	if len(access.Devices) > 0 {
		out.WriteString("\n<table bordered striped compact>")
		out.WriteString("<tr><th>" + escapeHTML(b.texts.Rich.Device) + "</th><th>" + escapeHTML(b.texts.Rich.Platform) + "</th><th align=\"right\">" + escapeHTML(b.texts.Rich.Code) + "</th></tr>")
		for _, device := range access.Devices {
			name := device.Model
			if name == "" {
				name = b.texts.Devices.DeviceName
			}
			platform := device.Platform
			if platform == "" {
				platform = b.texts.Rich.NoData
			}
			tail := device.HWID
			if len(tail) > 8 {
				tail = tail[len(tail)-8:]
			}
			out.WriteString("<tr><td>" + escapeHTML(name) + "</td><td>" + escapeHTML(platform) + "</td><td align=\"right\"><code>" + escapeHTML(tail) + "</code></td></tr>")
			rows = append(rows, []telego.InlineKeyboardButton{
				button(b.text(b.texts.Buttons.Detach, map[string]any{"Device": firstRunes(name, 24)}), "dev:rm:"+tail),
			})
		}
		out.WriteString("</table>")
		out.WriteString("\n<p>" + escapeHTML(plainLine(b.texts.Devices.DetachHint)) + "</p>")
	}

	if len(slots) > 0 {
		out.WriteString("\n<table bordered striped compact>")
		out.WriteString("<tr><th>" + escapeHTML(b.texts.Rich.Slot) + "</th><th align=\"right\">" + escapeHTML(b.texts.Rich.Until) + "</th></tr>")
		for _, slot := range slots {
			until := b.texts.Rich.Forever
			switch {
			case slot.ExpiresAt == nil:
				until = b.texts.Rich.Forever
			case slot.ExpiresAt.After(now):
				until = dateText(*slot.ExpiresAt)
			default:
				until = b.texts.Rich.Expired
			}
			out.WriteString("<tr><td>" + escapeHTML(slot.Title) + "</td><td align=\"right\">" + escapeHTML(until) + "</td></tr>")
			if slot.Renewable {
				rows = append(rows, []telego.InlineKeyboardButton{
					button(b.text(b.texts.Buttons.RenewSlot, map[string]any{
						"Slot": firstRunes(slot.Title, 22), "Price": priceText(slot.Price, b.cfg.Current().Currency),
					}), "dev:renew:"+slot.OrderID),
				})
			}
		}
		out.WriteString("</table>")
	}

	if len(quotes) > 0 {
		out.WriteString("\n<ul>")
		for _, quote := range quotes {
			title := quote.Pack.Title
			if quote.Pack.Devices > 0 {
				title = fmt.Sprintf("+%d %s", quote.Pack.Devices, devicesWord(quote.Pack.Devices))
			}
			out.WriteString("<li>" + escapeHTML(title) + " · " + escapeHTML(priceText(quote.Amount, b.cfg.Current().Currency)) + "</li>")
			rows = append(rows, []telego.InlineKeyboardButton{
				button(b.text(b.texts.Buttons.BuySlot, map[string]any{
					"Slot": title, "Price": priceText(quote.Amount, b.cfg.Current().Currency),
				}), "dev:buy:"+quote.Pack.ID),
			})
		}
		out.WriteString("</ul>")
	} else if hint := strings.TrimSpace(b.texts.Devices.BuySlotsHint); hint != "" {
		out.WriteString("\n<p>" + escapeHTML(plainLine(hint)) + "</p>")
	}

	if b.texts.Rich.ChooseAction != "" && (len(rows) > 0) {
		out.WriteString("\n<footer>" + escapeHTML(b.texts.Rich.ChooseAction) + "</footer>")
	}
	rows = append(rows, b.backRow())
	return out.String(), keyboard(rows...), true
}

// ---------------------------------------------------------------------------
// Мелкие подписи
// ---------------------------------------------------------------------------

// richStatus — короткая подпись состояния для ячейки таблицы.
func (b *Bot) richStatus(access domain.Access, status string) string {
	switch status {
	case "ACTIVE":
		return plainLine(b.texts.Subscription.Active)
	case "EXPIRED":
		return plainLine(b.texts.Subscription.Expired)
	case "NONE":
		return b.texts.Rich.None
	default:
		return plainLine(b.texts.Subscription.Unknown)
	}
}

// richExpire — дата окончания и остаток дней.
func (b *Bot) richExpire(expire *time.Time) string {
	if expire == nil {
		return b.texts.Rich.NoData
	}
	label := dateText(*expire)
	days, ok := daysLeft(expire, time.Now())
	if !ok || days < 0 {
		return label
	}
	if days == 0 {
		return label + " · сегодня"
	}
	return fmt.Sprintf("%s · %d %s", label, days, daysWord(days))
}

func (b *Bot) richDeviceCount(access domain.Access) string {
	if access.DeviceLimit > 0 {
		return fmt.Sprintf("%d / %d", len(access.Devices), access.DeviceLimit)
	}
	return fmt.Sprint(len(access.Devices))
}

func (b *Bot) richTraffic(access domain.Access) string {
	if label := b.trafficLabel(access); label != "" {
		return label
	}
	return b.texts.Rich.NoData
}

// trafficLabel — «израсходовано / лимит» без разметки, для ячейки таблицы.
func (b *Bot) trafficLabel(access domain.Access) string {
	if access.TrafficLimit != nil && *access.TrafficLimit > 0 {
		limit := *access.TrafficLimit
		percent := float64(access.TrafficUsedBytes) / float64(limit) * 100
		if percent > 100 {
			percent = 100
		}
		return fmt.Sprintf("%s / %s · %.0f%%", bytesText(access.TrafficUsedBytes), bytesText(limit), percent)
	}
	if access.TrafficUsedBytes > 0 {
		return bytesText(access.TrafficUsedBytes)
	}
	return ""
}

// plainLine убирает разметку из строки настроек: заголовки экранов хранятся
// с HTML, а rich-заголовок разметку внутри себя не принимает.
func plainLine(text string) string {
	return strings.TrimSpace(mdhtml.Plain(text))
}
