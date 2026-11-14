package texts

import "encoding/json"

// MenuTexts — конструктор меню бота.
//
// Раскладка описана по состояниям: у кого-то подписка активна, у кого-то
// нет, у кого-то панель не отвечает. В каждом состоянии — свои ряды
// кнопок, а у кнопки — условия показа: «если платежи включены», «если есть
// пробный период».
//
// По умолчанию всё берётся из встроенных значений: пустой файл — значит
// встроенное меню.
type MenuTexts struct {
	// Layout — раскладка по состояниям: active, inactive, unknown.
	Layout map[string][]MenuRow `json:"layout"`
	// Buttons — описания кнопок по идентификатору.
	Buttons map[string]MenuButton `json:"buttons"`
}

// MenuRow — ряд кнопок.
//
// В файле ряд записывается либо списком кнопок, либо объектом с pack:
// «сколько кнопок в ряд». Второе нужно, когда ряд длинный и укладывать его
// в одну строку неудобно.
type MenuRow struct {
	// Pack — сколько кнопок в ряд. Ноль — весь ряд в одну строку.
	Pack    int      `json:"pack"`
	Buttons []string `json:"buttons"`
}

// UnmarshalJSON принимает и список, и объект с pack.
func (r *MenuRow) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		r.Buttons = list
		r.Pack = 0
		return nil
	}
	type plain MenuRow
	var object plain
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	*r = MenuRow(object)
	return nil
}

// MenuButton — описание кнопки.
type MenuButton struct {
	// Label — подпись. Пусто — берётся встроенная.
	Label string `json:"label"`
	// Action — действие бота (callback-данные).
	Action string `json:"action"`
	// URL — внешняя ссылка.
	URL string `json:"url"`
	// WebApp — ссылка на мини-приложение.
	WebApp string `json:"web_app"`
	// Style — вид кнопки: primary, success, danger. Влияет на подпись в
	// Telegram, а не на поведение. Пусто — акцент по смыслу действия.
	Style string `json:"style"`
	// Require — условия показа: payments, trial, devices. Кнопка видна,
	// только если выполнены все условия.
	Require []string `json:"require"`
	// When — состояния, в которых кнопка показывается. Пусто — во всех.
	When []string `json:"when"`
}

// Состояния меню.
const (
	MenuStateActive   = "active"
	MenuStateInactive = "inactive"
	MenuStateUnknown  = "unknown"
)

// Условия показа кнопок.
const (
	MenuRequirePayments = "payments"
	MenuRequireTrial    = "trial"
	MenuRequireDevices  = "devices"
)

// DefaultMenuLayout — встроенная раскладка меню.
func DefaultMenuLayout() map[string][]MenuRow {
	return map[string][]MenuRow{
		MenuStateActive: {
			{Buttons: []string{"subscription"}},
			{Buttons: []string{"renew", "devices"}},
			{Buttons: []string{"profile", "support"}},
			{Buttons: []string{"referral", "more"}},
		},
		MenuStateInactive: {
			{Buttons: []string{"trial"}},
			{Buttons: []string{"buy"}},
			{Buttons: []string{"connect"}},
			{Buttons: []string{"profile", "support"}},
			{Buttons: []string{"referral", "more"}},
		},
		MenuStateUnknown: {
			{Buttons: []string{"subscription"}},
			{Buttons: []string{"buy"}},
			{Buttons: []string{"trial"}},
			{Buttons: []string{"devices"}},
			{Buttons: []string{"profile", "support"}},
			{Buttons: []string{"referral", "more"}},
		},
	}
}

// DefaultMenuButtons — встроенные кнопки меню.
func DefaultMenuButtons() map[string]MenuButton {
	return map[string]MenuButton{
		"subscription": {Label: "⚡ Подключить устройство", Action: "nav:subscription"},
		"renew":        {Label: "🔄 Продлить", Action: "nav:buy"},
		"devices":      {Label: "📱 Устройства", Action: "nav:devices", Require: []string{MenuRequireDevices}},
		"trial":        {Label: "🎁 Попробовать бесплатно", Action: "nav:trial", Require: []string{MenuRequireTrial}},
		"buy":          {Label: "💳 Выбрать тариф", Action: "nav:buy", Require: []string{MenuRequirePayments}},
		"promo":        {Label: "🏷️ Промокод", Action: "nav:promo"},
		"connect":      {Label: "ℹ️ Как подключиться", Action: "nav:connect"},
		"profile":      {Label: "👤 Профиль", Action: "nav:profile"},
		"support":      {Label: "✉️ Поддержка", Action: "nav:support"},
		"referral":     {Label: "👥 Друзья", Action: "nav:referral"},
		"more":         {Label: "❓ Ещё", Action: "nav:more"},
	}
}
