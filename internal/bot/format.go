package bot

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// nbsp — неразрывный пробел. Telegram переносит строку по обычному пробелу,
// и «1 200 ₽» разваливалось бы между разрядом и валютой.
const nbsp = "\u00a0"

// grouped разделяет разряды: 1290 → 1 290.
func grouped(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	digits := strconv.FormatInt(value, 10)
	var out strings.Builder
	out.WriteString(sign)
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteString(nbsp)
		}
		out.WriteRune(r)
	}
	return out.String()
}

// priceText печатает сумму так, как её увидят на витрине: целые рубли без
// хвоста «.00», а копейки — полностью. Именно эта сумма уходит в счёт,
// поэтому округлять её нельзя.
func priceText(amount domain.Money, currency string) string {
	text := amount.String()
	if strings.HasSuffix(text, ".00") {
		text = strings.TrimSuffix(text, ".00")
	}
	if currency == "" {
		return text
	}
	return text + nbsp + currencySymbol(currency)
}

func currencySymbol(currency string) string {
	switch strings.ToUpper(currency) {
	case "RUB":
		return "₽"
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "XTR":
		return "★"
	default:
		return strings.ToUpper(currency)
	}
}

// daysWord согласует «день» с числом: 1 день, 2 дня, 5 дней.
func daysWord(days int) string {
	if days < 0 {
		days = -days
	}
	if 11 <= days%100 && days%100 <= 14 {
		return "дней"
	}
	switch days % 10 {
	case 1:
		return "день"
	case 2, 3, 4:
		return "дня"
	default:
		return "дней"
	}
}

// plural — общее правило для «устройство/устройства/устройств».
func plural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	if 11 <= n%100 && n%100 <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func devicesWord(n int) string {
	return plural(n, "устройство", "устройства", "устройств")
}

func dateText(t time.Time) string { return t.In(time.Local).Format("02.01.2006") }

func dateTimeText(t time.Time) string { return t.In(time.Local).Format("02.01.2006 15:04") }

// daysLeft считает полные сутки до срока. Отрицательное значение — срок
// прошёл, ноль — истекает сегодня.
func daysLeft(expire *time.Time, now time.Time) (int, bool) {
	if expire == nil || expire.IsZero() {
		return 0, false
	}
	return int(expire.Sub(now).Hours() / 24), true
}

func bytesText(value int64) string {
	switch {
	case value < 1024:
		return fmt.Sprintf("%d Б", value)
	case value < 1<<20:
		return fmt.Sprintf("%.1f КБ", float64(value)/1024)
	case value < 1<<30:
		return fmt.Sprintf("%.1f МБ", float64(value)/(1<<20))
	case value < 1<<40:
		return fmt.Sprintf("%.2f ГБ", float64(value)/(1<<30))
	default:
		return fmt.Sprintf("%.2f ТБ", float64(value)/(1<<40))
	}
}

// trafficBar рисует заполненность лимита. Цвет меняется по мере приближения
// к потолку: зелёный → жёлтый → красный.
func trafficBar(used, limit int64, width int) string {
	if width <= 0 {
		width = 14
	}
	if limit <= 0 {
		return strings.Repeat("▫", width)
	}
	ratio := float64(used) / float64(limit)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(width)))
	empty := width - filled
	block := "🟩"
	if ratio >= 0.8 {
		block = "🟥"
	} else if ratio >= 0.5 {
		block = "🟨"
	}
	return strings.Repeat(block, filled) + strings.Repeat("▫", empty)
}

// expiryBar показывает, сколько срока осталось: свежая подписка — зелёная,
// последние дни — красные. Горизонт фиксированный, чтобы 180-дневный тариф
// не выглядел «почти истёкшим» сразу после покупки.
func expiryBar(days int) string {
	const horizon = 30
	if days < 0 {
		days = 0
	}
	used := horizon - days
	if used < 0 {
		used = 0
	}
	return trafficBar(int64(used), horizon, 14)
}

func orderStatusLabel(status string) string {
	switch status {
	case domain.OrderPending:
		return "⏳ ожидает оплаты"
	case domain.OrderPaid:
		return "💰 оплачен"
	case domain.OrderProvisioned:
		return "✅ выдан"
	case domain.OrderFailed:
		return "⚠️ ошибка"
	case domain.OrderCancelled:
		return "✖️ отменён"
	case domain.OrderRefunded:
		return "↩️ возврат"
	default:
		return status
	}
}

func orderKindLabel(kind string) string {
	switch kind {
	case domain.KindSubscription:
		return "Подписка"
	case domain.KindDevices:
		return "Устройства"
	case domain.KindGift:
		return "Подарок"
	case domain.KindBalance:
		return "Баланс"
	default:
		return kind
	}
}

// methodLabel — подпись способа оплаты в истории заказов.
func methodLabel(method string) string {
	switch method {
	case "card":
		return "💳 Карта"
	case "sbp":
		return "📱 СБП"
	case "tpay":
		return "⚡ T-Pay"
	case "sberpay":
		return "🏦 SberPay"
	case "stars":
		return "⭐ Звёзды"
	case "balance":
		return "💰 Баланс"
	case "promo", "promo_gift":
		return "🏷️ Промокод"
	case "manual_grant":
		return "🎁 Выдача"
	case "":
		return ""
	default:
		return method
	}
}

// escapeHTML защищает пользовательский текст (имя тикета, ошибку шлюза) от
// того, чтобы он стал разметкой сообщения.
func escapeHTML(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(text)
}

// shortID укорачивает длинный номер заказа для подписи кнопки.
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}
