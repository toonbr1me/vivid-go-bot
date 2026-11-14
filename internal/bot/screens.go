package bot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mymmrac/telego"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// mainScreen — точка входа. Статус подписки берём из панели, но сбой не
// мешает показать меню: человек не должен оставаться без кнопок.
func (b *Bot) mainScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	access, err := b.accessFor(ctx, subject)

	status := "UNKNOWN"
	statusLine := b.texts.Main.StatusUnknown
	if err == nil || access.Found {
		switch {
		case access.Found && access.Active:
			status = "ACTIVE"
			expire, ok := daysLeft(access.ExpireAt, time.Now())
			if ok && expire >= 0 {
				statusLine = b.text(b.texts.Main.StatusActive, map[string]any{
					"Date": dateText(*access.ExpireAt), "Days": expire, "DaysWord": daysWord(expire),
				})
			} else {
				statusLine = b.texts.Main.StatusShort
			}
		case access.ExpireAt != nil:
			status = "EXPIRED"
			statusLine = b.text(b.texts.Main.StatusExpired, map[string]any{"Date": dateText(*access.ExpireAt)})
		default:
			status = "NONE"
			statusLine = b.texts.Main.StatusNone
		}
	}

	trialAvailable := b.trialAvailable(ctx, subject)
	text := b.text(b.texts.Main.Title, map[string]any{"Status": statusLine})
	if greeting := b.greeting(); greeting != "" {
		text = greeting + "\n\n" + text
	}
	if status == "NONE" && trialAvailable && b.cfg.TrialBotDays > 0 {
		text += b.text(b.texts.Main.TrialHint, map[string]any{
			"Days": b.cfg.TrialBotDays, "DaysWord": daysWord(b.cfg.TrialBotDays),
		})
	}
	return text, b.mainKeyboard(status, trialAvailable)
}

// subscriptionScreen показывает срок, трафик и ссылку подключения.
func (b *Bot) subscriptionScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		return b.texts.Common.ProfileError, keyboard(b.backRow())
	}
	access, panelErr := b.accessFor(ctx, subject)
	if panelErr != nil && !access.Found {
		return b.texts.Common.PanelDown, keyboard(b.backRow())
	}
	if !access.Found && access.ExpireAt == nil {
		return b.noSubscriptionScreen()
	}

	var lines []string
	lines = append(lines, b.texts.Subscription.Title, "")

	now := time.Now().UTC()
	status := b.texts.Subscription.Unknown
	switch {
	case access.Active:
		status = b.texts.Subscription.Active
	case access.ExpireAt != nil && access.ExpireAt.Before(now):
		status = b.texts.Subscription.Expired
	}
	info := status
	if access.Username != "" {
		info += "  ·  👤 <code>" + escapeHTML(access.Username) + "</code>"
	}
	lines = append(lines, "<blockquote>"+info+"</blockquote>")

	days, hasExpire := daysLeft(access.ExpireAt, now)
	if hasExpire {
		switch {
		case days > 0:
			block := b.text(b.texts.Subscription.Until, map[string]any{
				"Date": dateText(*access.ExpireAt), "Days": days, "DaysWord": daysWord(days),
				"Bar": expiryBar(days),
			})
			lines = append(lines, "<blockquote>"+block+"</blockquote>")
		case days == 0:
			lines = append(lines, "<blockquote>"+b.text(b.texts.Subscription.Today,
				map[string]any{"Date": dateText(*access.ExpireAt)})+"</blockquote>")
		default:
			lines = append(lines, "<blockquote>"+b.text(b.texts.Subscription.ExpiredAgo, map[string]any{
				"Date": dateText(*access.ExpireAt), "Days": -days, "DaysWord": daysWord(days),
			})+"</blockquote>")
		}
	}

	if access.TrafficUsedBytes > 0 || (access.TrafficLimit != nil && *access.TrafficLimit > 0) {
		if access.TrafficLimit != nil && *access.TrafficLimit > 0 {
			limit := *access.TrafficLimit
			percent := float64(access.TrafficUsedBytes) / float64(limit) * 100
			if percent > 100 {
				percent = 100
			}
			block := b.text(b.texts.Subscription.Traffic, map[string]any{
				"Bar":     trafficBar(access.TrafficUsedBytes, limit, 14),
				"Percent": fmt.Sprintf("%.0f", percent),
				"Used":    bytesText(access.TrafficUsedBytes),
				"Limit":   bytesText(limit),
			})
			lines = append(lines, "<blockquote>"+block+"</blockquote>")
		} else {
			block := b.text(b.texts.Subscription.TrafficNone, map[string]any{
				"Used": bytesText(access.TrafficUsedBytes),
			})
			lines = append(lines, "<blockquote>"+block+"</blockquote>")
		}
	}

	if access.URL != "" {
		lines = append(lines, "", b.texts.Subscription.LinkTitle,
			"<code>"+escapeHTML(access.URL)+"</code>", "",
			b.texts.Subscription.LinkHint)
	} else {
		lines = append(lines, "", b.texts.Subscription.LinkMissing)
	}
	if panelErr != nil {
		lines = append(lines, "", b.texts.Subscription.PanelPart)
	}
	return strings.Join(lines, "\n"), b.subscriptionKeyboard(access)
}

func (b *Bot) noSubscriptionScreen() (string, *telego.InlineKeyboardMarkup) {
	text := b.texts.Subscription.Empty
	if b.cfg.TrialBotDays > 0 {
		text += b.text(b.texts.Subscription.EmptyTrial, map[string]any{
			"Days": b.cfg.TrialBotDays, "DaysWord": daysWord(b.cfg.TrialBotDays),
		})
	} else {
		text += b.texts.Subscription.EmptyBuy
	}
	var rows [][]telego.InlineKeyboardButton
	if b.cfg.TrialBotDays > 0 {
		rows = append(rows, []telego.InlineKeyboardButton{
			button(b.text(b.texts.Buttons.TrialDays, map[string]any{
				"Days": b.cfg.TrialBotDays, "DaysWord": daysWord(b.cfg.TrialBotDays),
			}), "nav:trial"),
		})
	}
	if b.pay.Registry().Enabled() {
		rows = append(rows, []telego.InlineKeyboardButton{
			button(b.texts.Buttons.Buy, "nav:buy"),
		})
	}
	rows = append(rows, b.backRow())
	return text, keyboard(rows...)
}

// buyScreen — витрина тарифов с учётом введённого промокода.
func (b *Bot) buyScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	quotes, err := b.pay.QuotePlans(ctx, subject.ID, b.currentPromo(tgID))
	if err != nil {
		return b.texts.Common.TryLater, keyboard(b.backRow())
	}
	quotes = sellableQuotes(quotes)
	if len(quotes) == 0 {
		return b.texts.Buy.Unavailable, keyboard(b.backRow())
	}

	best := domain.BestPriceIDs(planList(quotes))
	lines := []string{b.texts.Buy.Title, ""}
	if promo := b.currentPromo(tgID); promo != "" {
		lines = append(lines, b.text(b.texts.Buy.Promo, map[string]any{"Promo": escapeHTML(promo)}), "")
	}
	for _, quote := range quotes {
		line := fmt.Sprintf("%s · <b>%s</b> · %d %s", quote.Plan.Title,
			priceText(quote.Amount, b.cfg.Currency), quote.Plan.Days, daysWord(quote.Plan.Days))
		if quote.Amount < quote.Price {
			line += "  <s>" + priceText(quote.Price, b.cfg.Currency) + "</s>"
		}
		if best[quote.Plan.ID] {
			line += "  🔥"
		}
		if quote.Rejection != "" {
			line += "\n   ⚠️ " + escapeHTML(quote.Rejection)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", b.texts.Buy.Choose)

	var rows [][]telego.InlineKeyboardButton
	var pair []telego.InlineKeyboardButton
	for _, quote := range quotes {
		if quote.Rejection != "" {
			continue
		}
		label := quote.Plan.Title + " · " + priceText(quote.Amount, b.cfg.Currency)
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
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// planScreen — карточка тарифа со способами оплаты.
func (b *Bot) planScreen(ctx context.Context, tgID int64, planID string) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	plan, err := b.pay.Plan(ctx, planID)
	if err != nil {
		return b.texts.Plan.NotFound, keyboard(b.backRow())
	}
	amount := plan.Price
	quotes, _ := b.pay.QuotePlans(ctx, subject.ID, b.currentPromo(tgID))
	for _, quote := range quotes {
		if quote.Plan.ID == planID && quote.Rejection == "" {
			amount = quote.Amount
		}
	}

	lines := []string{
		"💳 <b>" + escapeHTML(plan.Title) + "</b>",
		"",
		b.text(b.texts.Plan.Term, map[string]any{"Days": plan.Days, "DaysWord": daysWord(plan.Days)}),
	}
	if plan.Devices > 0 {
		lines = append(lines, b.text(b.texts.Plan.Devices, map[string]any{"Devices": plan.Devices}))
	}
	lines = append(lines, b.text(b.texts.Plan.Price, map[string]any{"Price": priceText(amount, b.cfg.Currency)}))
	if amount < plan.Price {
		lines = append(lines, b.text(b.texts.Plan.OldPrice, map[string]any{"Price": priceText(plan.Price, b.cfg.Currency)}))
	}
	lines = append(lines, "", b.texts.Plan.ChoosePay)

	var rows [][]telego.InlineKeyboardButton
	for _, option := range b.pay.PaymentOptions(false) {
		if b.checkoutRecurring(ctx, tgID, planID) && !b.pay.RecurringSupported(option, plan.Days) {
			continue
		}
		prefix := "pay:"
		if b.checkoutRecurring(ctx, tgID, planID) {
			prefix = "payr:"
		}
		rows = append(rows, []telego.InlineKeyboardButton{
			button(option.ProviderLabel+" · "+option.MethodLabel,
				prefix+planID+":"+option.Token()),
		})
	}

	balance, _ := b.db.Balance(ctx, subject.ID)
	if balance > 0 && balance >= amount && !b.checkoutRecurring(ctx, tgID, planID) {
		rows = append(rows, []telego.InlineKeyboardButton{
			button(b.text(b.texts.Buttons.PayWithBalance, map[string]any{
				"Balance": priceText(balance, b.cfg.Currency),
			}), "payb:"+planID),
		})
	}
	if len(rows) == 0 && balance < amount {
		lines = append(lines, "", b.texts.Plan.NoProviders)
	}
	if b.cfg.RecurringEnabled {
		label := "☐ Продлевать автоматически?"
		if b.checkoutRecurring(ctx, tgID, planID) {
			label = "☑ Продлевать автоматически?"
			lines = append(lines, fmt.Sprintf("Автоплатёж: %s каждые %d дней. Отключить можно в аккаунте.", priceText(amount, b.cfg.Currency), plan.Days))
		}
		rows = append([][]telego.InlineKeyboardButton{{button(label, "recurring:toggle:"+planID)}}, rows...)
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← Тарифы", "nav:buy"),
		button("🏠 Меню", "nav:main"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

func sellableQuotes(quotes []domain.PlanQuote) []domain.PlanQuote {
	out := make([]domain.PlanQuote, 0, len(quotes))
	for _, quote := range quotes {
		out = append(out, quote)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Plan.SortOrder < out[j].Plan.SortOrder
	})
	return out
}

func planList(quotes []domain.PlanQuote) []domain.Plan {
	out := make([]domain.Plan, 0, len(quotes))
	for _, quote := range quotes {
		out = append(out, quote.Plan)
	}
	return out
}

// devicesScreen сводит лимит из базы, подключённые устройства из панели и
// докупленные слоты.
func (b *Bot) devicesScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	if !b.cfg.DeviceLimitEnabled {
		return b.texts.Devices.Disabled, keyboard(b.backRow())
	}
	subject, err := b.subject(ctx, tgID)
	if err != nil {
		return b.texts.Common.ProfileError, keyboard(b.backRow())
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

	lines := []string{b.texts.Devices.Title, ""}
	if limit > 0 {
		free := limit - connected
		if free < 0 {
			free = 0
		}
		lines = append(lines, b.text(b.texts.Devices.Limit, map[string]any{
			"Limit": limit, "Connected": connected, "Free": free,
		}))
	} else {
		lines = append(lines, b.text(b.texts.Devices.Connected, map[string]any{"Connected": connected}))
	}
	if access.ExpireAt != nil {
		lines = append(lines, b.text(b.texts.Devices.Until, map[string]any{"Date": dateText(*access.ExpireAt)}))
	}
	if limit > 0 && connected >= limit {
		lines = append(lines, "", b.texts.Devices.Full)
	}

	var rows [][]telego.InlineKeyboardButton
	if len(access.Devices) > 0 {
		lines = append(lines, "", b.texts.Devices.ConnectedTtl)
		lines = append(lines, b.texts.Devices.DetachHint)
		for _, device := range access.Devices {
			name := device.Model
			if name == "" {
				name = b.texts.Devices.DeviceName
			}
			label := name
			if device.Platform != "" {
				label += " · " + device.Platform
			}
			tail := device.HWID
			if len(tail) > 8 {
				tail = tail[len(tail)-8:]
			}
			lines = append(lines, "• "+escapeHTML(label)+" <code>("+escapeHTML(tail)+")</code>")
			rows = append(rows, []telego.InlineKeyboardButton{
				button(b.text(b.texts.Buttons.Detach, map[string]any{"Device": firstRunes(label, 24)}), "dev:rm:"+tail),
			})
		}
	}

	if len(slots) > 0 {
		lines = append(lines, "", b.texts.Devices.Slots)
		for _, slot := range slots {
			switch {
			case slot.ExpiresAt == nil:
				lines = append(lines, b.text(b.texts.Devices.SlotForever, map[string]any{"Title": escapeHTML(slot.Title)}))
			case slot.ExpiresAt.After(now):
				lines = append(lines, b.text(b.texts.Devices.SlotUntil, map[string]any{
					"Title": escapeHTML(slot.Title), "Date": dateText(*slot.ExpiresAt),
				}))
			default:
				lines = append(lines, b.text(b.texts.Devices.SlotExpired, map[string]any{"Title": escapeHTML(slot.Title)}))
			}
			if slot.Renewable {
				rows = append(rows, []telego.InlineKeyboardButton{
					button(b.text(b.texts.Buttons.RenewSlot, map[string]any{
						"Slot": firstRunes(slot.Title, 22), "Price": priceText(slot.Price, b.cfg.Currency),
					}), "dev:renew:"+slot.OrderID),
				})
			}
		}
	}

	if len(quotes) > 0 {
		lines = append(lines, "", b.texts.Devices.BuySlots)
		for _, quote := range quotes {
			title := quote.Pack.Title
			if quote.Pack.Devices > 0 {
				title = fmt.Sprintf("+%d %s", quote.Pack.Devices, devicesWord(quote.Pack.Devices))
			}
			lines = append(lines, "• "+escapeHTML(title)+" · "+priceText(quote.Amount, b.cfg.Currency))
			rows = append(rows, []telego.InlineKeyboardButton{
				button(b.text(b.texts.Buttons.BuySlot, map[string]any{
					"Slot": title, "Price": priceText(quote.Amount, b.cfg.Currency),
				}), "dev:buy:"+quote.Pack.ID),
			})
		}
	} else {
		lines = append(lines, "", b.texts.Devices.BuySlotsHint)
	}

	rows = append(rows, b.backRow())
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// profileScreen — деньги, промокод и быстрые переходы.
func (b *Bot) profileScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	balance, _ := b.db.Balance(ctx, subject.ID)
	promo := b.currentPromo(tgID)

	lines := []string{b.texts.Profile.Title, ""}
	lines = append(lines, b.text(b.texts.Profile.Balance, map[string]any{"Balance": priceText(balance, b.cfg.Currency)}))
	if promo != "" {
		lines = append(lines, b.text(b.texts.Profile.Promo, map[string]any{"Promo": escapeHTML(promo)}))
	} else {
		lines = append(lines, b.texts.Profile.PromoNone)
	}
	if access, _ := b.accessFor(ctx, subject); access.Found {
		switch {
		case access.Active:
			lines = append(lines, b.texts.Profile.Active)
		case access.ExpireAt != nil:
			lines = append(lines, b.text(b.texts.Profile.Expired, map[string]any{"Date": dateText(*access.ExpireAt)}))
		}
	}
	lines = append(lines, "", b.texts.Profile.Note)

	payments := b.pay.Registry().Enabled()
	kb := b.profileKeyboard(promo != "", payments)
	if a, err := b.db.LatestRecurringForSubject(ctx, subject.ID); err == nil {
		lines = append(lines, "", "<b>Автопродление</b>", escapeHTML(a.StatusText()), escapeHTML(fmt.Sprintf("%s / %d %s · %s", priceText(a.Amount, a.Currency), a.Days, daysWord(a.Days), a.ProviderName())))
		if a.Enabled && a.NextChargeAt != nil {
			lines = append(lines, "Следующее списание: "+dateTimeText(*a.NextChargeAt))
		}
		if a.StoppedAt != nil {
			lines = append(lines, "Остановлено: "+dateTimeText(*a.StoppedAt))
		}
		if a.Enabled {
			kb.InlineKeyboard = append([][]telego.InlineKeyboardButton{{button("Отключить автопродление", "recurring:cancel")}}, kb.InlineKeyboard...)
		}
	}
	return strings.Join(lines, "\n"), kb
}

// historyScreen — последние покупки.
func (b *Bot) historyScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	orders, err := b.pay.OrderHistory(ctx, subject, 10)
	if err != nil {
		return b.texts.History.Failed, keyboard(b.backRow())
	}
	lines := []string{b.texts.History.Title, ""}
	if len(orders) == 0 {
		lines = append(lines, b.texts.History.Empty)
	}
	var rows [][]telego.InlineKeyboardButton
	for _, order := range orders {
		title := orderKindLabel(order.Kind)
		switch {
		case order.Kind == domain.KindDevices && order.DeviceCount != nil:
			title += fmt.Sprintf(" · +%d %s", *order.DeviceCount, devicesWord(*order.DeviceCount))
		case order.PlanID != "":
			if plan, err := b.pay.Plan(ctx, order.PlanID); err == nil {
				title += " · " + plan.Title
			}
		}
		line := fmt.Sprintf("%s · %s · %s", dateText(order.CreatedAt), title,
			order.Progress(time.Now()).Label)
		if order.Amount > 0 {
			line += " · " + priceText(order.Amount, order.Currency)
		}
		lines = append(lines, line)
		rows = append(rows, []telego.InlineKeyboardButton{button("Заказ · "+shortID(order.OrderID), "check:"+order.OrderID)})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.Profile, "nav:profile"),
		button("🏠 Меню", "nav:main"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

func (b *Bot) settingsScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	settings, err := b.db.NotificationSettingsFor(ctx, tgID)
	if err != nil {
		return b.texts.Settings.Failed, keyboard(b.backRow())
	}
	mark := func(on bool) string {
		if on {
			return "✅"
		}
		return "☑️"
	}
	lines := []string{
		b.texts.Settings.Title,
		"",
		b.texts.Settings.Lead,
		"",
		mark(settings.Expiry) + "  " + b.texts.Settings.Expiry,
	}
	if b.activityNotificationsEnabled() {
		lines = append(lines, mark(settings.Activity)+"  "+b.texts.Settings.Activity)
	}
	lines = append(lines, "", b.texts.Settings.Note)
	return strings.Join(lines, "\n"), b.settingsKeyboard(settings)
}

func (b *Bot) promoScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	promo := b.currentPromo(tgID)
	lines := []string{b.texts.Promo.Title, ""}
	if promo != "" {
		lines = append(lines, b.text(b.texts.Promo.Current, map[string]any{"Promo": escapeHTML(promo)}),
			b.texts.Promo.CurrentEnd)
	} else {
		lines = append(lines, b.texts.Promo.Hint, b.texts.Promo.GiftHint)
	}

	var rows [][]telego.InlineKeyboardButton
	if promo != "" {
		rows = append(rows, []telego.InlineKeyboardButton{
			button(b.texts.Buttons.ClearPromo, "promo:clear"),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button(b.texts.Buttons.EditPromo, "promo:enter"),
	})
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← "+b.texts.Buttons.Profile, "nav:profile"),
		button("🏠 Меню", "nav:main"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

func (b *Bot) referralScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	stats, err := b.db.ReferralStatsFor(ctx, subject.ID)
	if err != nil {
		return "⚠️ Не удалось загрузить данные.", keyboard(b.backRow())
	}
	mode, _ := b.db.ReferralMode(ctx, subject.ID)
	if mode == "" {
		mode = b.cfg.ReferralMode
	}
	if mode == "" {
		mode = "days"
	}
	link := b.referralLink(subject)
	lines := []string{
		b.texts.Referral.Title,
		"",
		b.text(b.texts.Referral.Invited, map[string]any{"Invited": stats.Invited}),
		b.text(b.texts.Referral.Rewards, map[string]any{"Rewards": stats.RewardCount}),
		b.text(b.texts.Referral.EarnedDays, map[string]any{"Days": stats.EarnedDays}),
		"",
		b.text(b.texts.Referral.Balance, map[string]any{"Balance": priceText(stats.Balance, b.cfg.Currency)}),
		"",
		b.text(b.texts.Referral.Mode, map[string]any{"Mode": referralModeLabel(mode)}),
	}
	switch mode {
	case "balance":
		lines = append(lines, "За каждую покупку друга — "+fmt.Sprint(b.cfg.RefBalancePercent)+
			"% от суммы на ваш баланс.")
	case "both":
		lines = append(lines, "За каждую покупку друга — "+fmt.Sprint(b.cfg.RefBalancePercent)+
			"% на баланс, первые две покупки — ещё и днями подписки.")
	default:
		lines = append(lines, "За первого оплатившего друга — 7 дней, за второго — 3 дня.")
	}
	if link != "" {
		lines = append(lines, "", "🔗 Ваша ссылка:", "<code>"+escapeHTML(link)+"</code>")
	}
	lines = append(lines, "", "Режим можно переключить кнопкой ниже.")
	return strings.Join(lines, "\n"), b.referralKeyboard(link, mode)
}

// referralModeLabel — как называть режим награды.
func referralModeLabel(mode string) string {
	switch mode {
	case "balance":
		return "деньги на баланс"
	case "both":
		return "и дни, и деньги"
	default:
		return "дни подписки"
	}
}

func (b *Bot) supportScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	subject, _ := b.subject(ctx, tgID)
	open, err := b.db.OpenTicketForSubject(ctx, subject.ID)
	if err != nil {
		open = nil
	}
	lines := []string{
		b.texts.Support.Title,
		"",
		b.texts.Support.Lead,
	}
	if open != nil {
		lines = append(lines, "", b.text(b.texts.Support.OpenTicket, map[string]any{"Subject": escapeHTML(open.Subject)}),
			b.texts.Support.Continue)
	}
	staff := b.isStaff(ctx, tgID)
	return strings.Join(lines, "\n"), b.supportKeyboard(open, staff)
}

func (b *Bot) infoScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	brand := b.cfg.BrandName
	lines := []string{b.texts.Info.Title, ""}
	lines = append(lines, b.text(b.texts.Info.Lead, map[string]any{"Brand": escapeHTML(brand)}))
	lines = append(lines, "", b.texts.Info.Documents, b.texts.Info.DocumentsH)
	if b.cfg.LegalEntity != "" {
		lines = append(lines, "", b.text(b.texts.Info.Executor, map[string]any{"Entity": escapeHTML(b.cfg.LegalEntity)}))
	}
	lines = append(lines, "", b.texts.Info.Payment, b.texts.Info.PaymentH)
	lines = append(lines, "", b.texts.Info.Refund, b.texts.Info.RefundH)
	lines = append(lines, "", b.texts.Info.Contacts, b.texts.Info.ContactsH)
	if b.cfg.SupportEmail != "" {
		lines = append(lines, b.text(b.texts.Info.Mail, map[string]any{"Email": escapeHTML(b.cfg.SupportEmail)}))
	}
	if b.cfg.SupportUser != "" {
		lines = append(lines, b.text(b.texts.Info.Telegram, map[string]any{"Username": escapeHTML(b.cfg.SupportUser)}))
	}
	_ = tgID
	return strings.Join(lines, "\n"), b.infoKeyboard()
}

func (b *Bot) helpScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	lines := []string{
		b.texts.Help.Title,
		"",
		"🔹 /start — главное меню",
		"🔹 /buy — выбрать тариф",
		"🔹 /trial — пробный период",
		"🔹 /subscription — моя подписка",
		"🔹 /status — живая карточка подписки",
		"🔹 /devices — устройства",
		"🔹 /promo — промокод",
		"🔹 /referral — друзья и баланс",
		"🔹 /support — поддержка",
		"🔹 /weblogin — войти на сайт",
		"🔹 /info — о сервисе",
		"🔹 /help — этот список",
	}
	if b.isStaff(ctx, tgID) {
		lines = append(lines, "", b.texts.Help.Staff,
			"🔹 /admin — панель",
			"🔹 /stats — статистика",
			"🔹 /tickets — обращения",
			"🔹 /grant — выдать дни",
			"🔹 /reply — ответить в обращение",
			"🔹 /close — закрыть обращение")
	}
	return strings.Join(lines, "\n"), b.helpKeyboard()
}

func (b *Bot) moreScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	text := b.text(b.texts.More.Title, nil) + "\n\n" + b.texts.More.Body
	return text, b.moreKeyboard()
}

func (b *Bot) connectScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	text := b.text(b.texts.Connect.Title, nil) + "\n\n" + b.texts.Connect.Body
	if b.cfg.ConnectGuideURL != "" {
		text += "\n\n" + b.texts.Connect.Guide
	}
	return text, b.connectKeyboard()
}
