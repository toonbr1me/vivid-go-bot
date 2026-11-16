// Package texts — тексты бота.
//
// Значения по умолчанию живут здесь, а файл WEB_DIR/bot.json накладывается
// поверх: в нём заданы только те ключи, которые другой проект хочет
// переписать. Ключи устроены по экранам: "main.title", "buttons.menu".
//
// В значениях работают подстановки: {{.Brand}}, {{.Currency}} и любые
// другие, которые передаёт экран. Нет подстановки — остаётся пустое
// место, а не ошибка: лишний ключ не должен ронять бота.
package texts

// Texts — все тексты бота.
type Texts struct {
	Common       CommonTexts       `json:"common"`
	Buttons      ButtonTexts       `json:"buttons"`
	Main         MainTexts         `json:"main"`
	Subscription SubscriptionTexts `json:"subscription"`
	Buy          BuyTexts          `json:"buy"`
	Plan         PlanTexts         `json:"plan"`
	Trial        TrialTexts        `json:"trial"`
	Devices      DevicesTexts      `json:"devices"`
	Profile      ProfileTexts      `json:"profile"`
	History      HistoryTexts      `json:"history"`
	Settings     SettingsTexts     `json:"settings"`
	Promo        PromoTexts        `json:"promo"`
	Referral     ReferralTexts     `json:"referral"`
	Support      SupportTexts      `json:"support"`
	Info         InfoTexts         `json:"info"`
	Help         HelpTexts         `json:"help"`
	More         MoreTexts         `json:"more"`
	Connect      ConnectTexts      `json:"connect"`
	Payment      PaymentTexts      `json:"payment"`
	Session      SessionTexts      `json:"session"`
	Ticket       TicketTexts       `json:"ticket"`
	Menu         MenuTexts         `json:"menu"`
	Persona      PersonaTexts      `json:"persona"`
	Rich         RichTexts         `json:"rich"`
}

// RichTexts — подписи богатых сообщений: таблицы, сноски и подсказки.
//
// Отдельная группа нужна белому лейблу: заголовки колонок и служебные
// строки видны в чате, и их тоже должно быть можно переписать в bot.json.
type RichTexts struct {
	Parameter    string `json:"parameter"`
	Value        string `json:"value"`
	Status       string `json:"status"`
	Subscription string `json:"subscription"`
	Until        string `json:"until"`
	Traffic      string `json:"traffic"`
	Devices      string `json:"devices"`
	Plan         string `json:"plan"`
	Term         string `json:"term"`
	Price        string `json:"price"`
	Device       string `json:"device"`
	Platform     string `json:"platform"`
	Code         string `json:"code"`
	Slot         string `json:"slot"`
	NoData       string `json:"no_data"`
	None         string `json:"none"`
	PaymentOnce  string `json:"payment_once"`
	PaymentLabel string `json:"payment_label"`
	AutoRenewNo  string `json:"auto_renew_no"`
	Connect      string `json:"connect"`
	ConnectHint  string `json:"connect_hint"`
	ChooseAction string `json:"choose_action"`
	TrialHint    string `json:"trial_hint"`
	PanelDown    string `json:"panel_down"`
	HintActive   string `json:"hint_active"`
	HintExpired  string `json:"hint_expired"`
	HintNone     string `json:"hint_none"`
	Forever      string `json:"forever"`
	Expired      string `json:"expired"`
}

// PersonaTexts — характер бота: маскот и приветствия по времени суток.
type PersonaTexts struct {
	// Mascot — значок рядом с приветствием.
	Mascot string `json:"mascot"`
	// Greetings — приветствия по времени суток: morning, day, evening, night.
	Greetings map[string]string `json:"greetings"`
}

// SessionTexts — вход на сайт по коду и привязка Telegram.
type SessionTexts struct {
	CodeTitle       string `json:"code_title"`
	CodeBody        string `json:"code_body"`
	OpenSite        string `json:"open_site"`
	OpenAccount     string `json:"open_account"`
	CodeFailed      string `json:"code_failed"`
	EmptyCode       string `json:"empty_code"`
	CodeCheckFailed string `json:"code_check_failed"`
	CodeNotFound    string `json:"code_not_found"`
	AccountNotFound string `json:"account_not_found"`
	AlreadyLinked   string `json:"already_linked"`
	OtherTelegram   string `json:"other_telegram"`
	AnotherProfile  string `json:"another_profile"`
	TelegramFailed  string `json:"telegram_failed"`
	AttachFailed    string `json:"attach_failed"`
	LinkedTitle     string `json:"linked_title"`
	LinkedBody      string `json:"linked_body"`
}

// TicketTexts — обращения в поддержку.
type TicketTexts struct {
	Empty       string `json:"empty"`
	OpenFailed  string `json:"open_failed"`
	SendFailed  string `json:"send_failed"`
	Added       string `json:"added"`
	Open        string `json:"open"`
	CreateFail  string `json:"create_failed"`
	Created     string `json:"created"`
	NotFound    string `json:"not_found"`
	NoRights    string `json:"no_rights"`
	AnswerFail  string `json:"answer_failed"`
	AnswerTitle string `json:"answer_title"`
	AnswerSent  string `json:"answer_sent"`
	ToTicket    string `json:"to_ticket"`
	Hint        string `json:"hint"`
}

// CommonTexts — то, что повторяется на всех экранах.
type CommonTexts struct {
	Unavailable   string `json:"unavailable"`
	ProfileError  string `json:"profile_error"`
	PanelDown     string `json:"panel_down"`
	TryLater      string `json:"try_later"`
	Empty         string `json:"empty"`
	Back          string `json:"back"`
	NotUnderstood string `json:"not_understood"`
}

// ButtonTexts — подписи кнопок.
type ButtonTexts struct {
	Back           string `json:"back"`
	Menu           string `json:"menu"`
	Profile        string `json:"profile"`
	Support        string `json:"support"`
	More           string `json:"more"`
	Subscription   string `json:"subscription"`
	MySubscription string `json:"my_subscription"`
	Buy            string `json:"buy"`
	ChoosePlan     string `json:"choose_plan"`
	Renew          string `json:"renew"`
	Devices        string `json:"devices"`
	Connect        string `json:"connect"`
	ConnectHow     string `json:"connect_how"`
	Promo          string `json:"promo"`
	Referral       string `json:"referral"`
	Trial          string `json:"trial"`
	TrialFree      string `json:"trial_free"`
	TrialDays      string `json:"trial_days"`
	Happ           string `json:"happ"`
	CopyLink       string `json:"copy_link"`
	OpenPage       string `json:"open_page"`
	CopyReferral   string `json:"copy_referral"`
	PayWithBalance string `json:"pay_with_balance"`
	Pay            string `json:"pay"`
	EditPromo      string `json:"edit_promo"`
	ClearPromo     string `json:"clear_promo"`
	Detach         string `json:"detach"`
	RenewSlot      string `json:"renew_slot"`
	BuySlot        string `json:"buy_slot"`
	CheckPayment   string `json:"check_payment"`
	ReferralMode   string `json:"referral_mode"`
	NewTicket      string `json:"new_ticket"`
	MyTickets      string `json:"my_tickets"`
	ContinueTicket string `json:"continue_ticket"`
	Queue          string `json:"queue"`
	Reply          string `json:"reply"`
	Close          string `json:"close"`
	Help           string `json:"help"`
	Info           string `json:"info"`
	History        string `json:"history"`
	Settings       string `json:"settings"`
	Terms          string `json:"terms"`
	Privacy        string `json:"privacy"`
	Website        string `json:"website"`
	CopyText       string `json:"copy_text"`
	Cancel         string `json:"cancel"`
	Save           string `json:"save"`
	Enable         string `json:"enable"`
	Disable        string `json:"disable"`
	Guide          string `json:"guide"`
	Grant          string `json:"grant"`
	Answer         string `json:"answer"`
	Send           string `json:"send"`
	TryPromo       string `json:"try_promo"`
}

// MainTexts — главное меню.
type MainTexts struct {
	Title         string `json:"title"`
	Lead          string `json:"lead"`
	StatusActive  string `json:"status_active"`
	StatusExpired string `json:"status_expired"`
	StatusShort   string `json:"status_short"`
	StatusNone    string `json:"status_none"`
	StatusUnknown string `json:"status_unknown"`
	TrialHint     string `json:"trial_hint"`
}

// SubscriptionTexts — экран подписки.
type SubscriptionTexts struct {
	Title       string `json:"title"`
	Status      string `json:"status"`
	Active      string `json:"active"`
	Expired     string `json:"expired"`
	Unknown     string `json:"unknown"`
	Until       string `json:"until"`
	Today       string `json:"today"`
	ExpiredAgo  string `json:"expired_ago"`
	Traffic     string `json:"traffic"`
	TrafficNone string `json:"traffic_none"`
	LinkTitle   string `json:"link_title"`
	LinkHint    string `json:"link_hint"`
	LinkMissing string `json:"link_missing"`
	PanelPart   string `json:"panel_part"`
	Empty       string `json:"empty"`
	EmptyTrial  string `json:"empty_trial"`
	EmptyBuy    string `json:"empty_buy"`
}

// BuyTexts — витрина тарифов.
type BuyTexts struct {
	Title       string `json:"title"`
	Promo       string `json:"promo"`
	Choose      string `json:"choose"`
	Unavailable string `json:"unavailable"`
}

// PlanTexts — карточка тарифа.
type PlanTexts struct {
	Term        string `json:"term"`
	Devices     string `json:"devices"`
	Price       string `json:"price"`
	OldPrice    string `json:"old_price"`
	ChoosePay   string `json:"choose_pay"`
	NoProviders string `json:"no_providers"`
	NotFound    string `json:"not_found"`
}

// TrialTexts — пробный период.
type TrialTexts struct {
	Done      string `json:"done"`
	Used      string `json:"used"`
	WhileLive string `json:"while_live"`
	Error     string `json:"error"`
}

// DevicesTexts — экран устройств.
type DevicesTexts struct {
	Title        string `json:"title"`
	Disabled     string `json:"disabled"`
	Limit        string `json:"limit"`
	Connected    string `json:"connected"`
	Until        string `json:"until"`
	Full         string `json:"full"`
	ConnectedTtl string `json:"connected_title"`
	DetachHint   string `json:"detach_hint"`
	DeviceName   string `json:"device_name"`
	Slots        string `json:"slots"`
	SlotForever  string `json:"slot_forever"`
	SlotUntil    string `json:"slot_until"`
	SlotExpired  string `json:"slot_expired"`
	BuySlots     string `json:"buy_slots"`
	BuySlotsHint string `json:"buy_slots_hint"`
}

// ProfileTexts — профиль.
type ProfileTexts struct {
	Title     string `json:"title"`
	Balance   string `json:"balance"`
	Promo     string `json:"promo"`
	PromoNone string `json:"promo_none"`
	Active    string `json:"active"`
	Expired   string `json:"expired"`
	Note      string `json:"note"`
}

// HistoryTexts — история покупок.
type HistoryTexts struct {
	Title  string `json:"title"`
	Empty  string `json:"empty"`
	Failed string `json:"failed"`
}

// SettingsTexts — настройки уведомлений.
type SettingsTexts struct {
	Title    string `json:"title"`
	Lead     string `json:"lead"`
	Expiry   string `json:"expiry"`
	Activity string `json:"activity"`
	Note     string `json:"note"`
	Saved    string `json:"saved"`
	Failed   string `json:"failed"`
}

// PromoTexts — промокод.
type PromoTexts struct {
	Title      string `json:"title"`
	Current    string `json:"current"`
	CurrentEnd string `json:"current_end"`
	Hint       string `json:"hint"`
	GiftHint   string `json:"gift_hint"`
	Enter      string `json:"enter"`
	Entered    string `json:"entered"`
	Rejected   string `json:"rejected"`
	BadFormat  string `json:"bad_format"`
	Gift       string `json:"gift"`
	NoFit      string `json:"no_fit"`
}

// ReferralTexts — приглашения.
type ReferralTexts struct {
	Title      string `json:"title"`
	Invited    string `json:"invited"`
	Rewards    string `json:"rewards"`
	EarnedDays string `json:"earned_days"`
	Balance    string `json:"balance"`
	Mode       string `json:"mode"`
	ModeDays   string `json:"mode_days"`
	ModeMoney  string `json:"mode_money"`
	ModeBoth   string `json:"mode_both"`
	Hint       string `json:"hint"`
	SwitchHint string `json:"switch_hint"`
}

// SupportTexts — поддержка.
type SupportTexts struct {
	Title      string `json:"title"`
	Lead       string `json:"lead"`
	OpenTicket string `json:"open_ticket"`
	Continue   string `json:"continue"`
	NewTicket  string `json:"new_ticket"`
	Empty      string `json:"empty"`
}

// InfoTexts — о сервисе.
type InfoTexts struct {
	Title      string `json:"title"`
	Lead       string `json:"lead"`
	Documents  string `json:"documents"`
	DocumentsH string `json:"documents_hint"`
	Executor   string `json:"executor"`
	Payment    string `json:"payment"`
	PaymentH   string `json:"payment_hint"`
	Refund     string `json:"refund"`
	RefundH    string `json:"refund_hint"`
	Contacts   string `json:"contacts"`
	ContactsH  string `json:"contacts_hint"`
	Mail       string `json:"mail"`
	Telegram   string `json:"telegram"`
}

// HelpTexts — список команд.
type HelpTexts struct {
	Title string `json:"title"`
	Staff string `json:"staff"`
}

// MoreTexts — раздел «Ещё».
type MoreTexts struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ConnectTexts — инструкция подключения.
type ConnectTexts struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Guide string `json:"guide"`
}

// PaymentTexts — экраны оплаты.
type PaymentTexts struct {
	Created   string `json:"created"`
	Amount    string `json:"amount"`
	Stars     string `json:"stars"`
	Order     string `json:"order"`
	Expires   string `json:"expires"`
	Reused    string `json:"reused"`
	Hint      string `json:"hint"`
	Done      string `json:"done"`
	FreePromo string `json:"free_promo"`
	Balance   string `json:"balance"`
	NotYet    string `json:"not_yet"`
	Pending   string `json:"pending"`
	Failed    string `json:"failed"`
	Provision string `json:"provision"`
}
