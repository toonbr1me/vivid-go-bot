package domain

import "time"

// PlanProfile holds the optional overrides sold with an order.
type PlanProfile struct {
	BaseDevices       *int
	TrafficLimitBytes *int64
	InternalSquads    []string
	ExternalSquadUUID *string
}

// Статусы заказа. Набор намеренно короткий: всё, что не оплачено и не
// отменено, — «pending», а выдача отслеживается отдельными полями.
const (
	OrderPending     = "pending"
	OrderPaid        = "paid"
	OrderProvisioned = "provisioned"
	OrderFailed      = "failed"
	OrderCancelled   = "cancelled"
	OrderRefunded    = "refunded"
)

// Виды заказов.
const (
	KindSubscription = "subscription"
	KindDevices      = "devices"
	KindGift         = "gift"
	KindBalance      = "balance"
)

// Order — заказ на покупку. У веб-заказа TelegramID может быть nil.
type Order struct {
	PlanProfile          *PlanProfile
	ID                   int64
	OrderID              string
	Provider             string
	TelegramID           *int64
	AccountID            *int64
	Username             *string
	PlanID               string
	Kind                 string
	Amount               Money
	OriginalAmount       Money
	Currency             string
	PaymentMethod        *string
	Status               string
	PaymentURL           *string
	PaymentLinkID        *string
	PaymentExpiresAt     *time.Time
	TransactionID        *string
	PromoCode            *string
	PlanDays             *int
	PromoBonusDays       *int
	GrantedDays          *int
	ReferrerID           *int64
	PartnerID            *int64
	RewardType           *string
	RewardValue          Money
	RewardScope          *string
	DeviceCount          *int
	DeviceDays           *int
	DeviceExpiresAt      *time.Time
	RenewGrantID         *string
	DeviceRenewals       []DeviceRenewal
	RenewalAmount        Money
	CreatedAt            time.Time
	UpdatedAt            time.Time
	PaidAt               *time.Time
	ProvisionedAt        *time.Time
	ExpireAt             *time.Time
	FulfillmentVer       int
	FulfillmentTries     int
	FulfillmentError     *string
	NotificationDone     *time.Time
	RecurringAgreementID *string
}

// DeviceRenewal — снимок продлеваемого пакетного слота в составе покупки.
type DeviceRenewal struct {
	GrantOrderID string `json:"grant_order_id"`
	PackID       string `json:"pack_id"`
	Devices      int    `json:"devices"`
	Amount       Money  `json:"amount"`
	BilledDays   int    `json:"billed_days"`
}

// PaymentOption — один способ оплаты в витрине.
type PaymentOption struct {
	Provider      string
	Method        string
	ProviderLabel string
	MethodLabel   string
	Recommended   bool
}

// Token — стабильный идентификатор для callback-данных.
func (o PaymentOption) Token() string { return o.Provider + "." + o.Method }

// Label — подпись кнопки.
func (o PaymentOption) Label() string {
	suffix := ""
	if o.Recommended {
		suffix = " — лучший вариант"
	}
	return o.ProviderLabel + " · " + o.MethodLabel + suffix
}

// ProvisioningProfile — куда селить пользователя в панели.
type ProvisioningProfile struct {
	DeviceLimit        *int
	ExternalSquadUUID  string
	InternalSquadUUIDs []string
	TrafficLimitBytes  *int64
}

// ProvisionResult — что именно выдал заказ.
type ProvisionResult struct {
	SubscriptionURL    *string
	ReferralBonusGiven bool
	ReferrerID         *int64
	BonusDays          int
	RefBalanceAmount   Money
	ExpireAt           *time.Time
	DevicesAdded       int
	DeviceLimit        *int
	DeviceExpiresAt    *time.Time
	DeviceRenewedUntil *time.Time
	DevicesRenewed     int
	IsRenewal          bool
}

// Subject — владелец подписки: Telegram-пользователь или веб-аккаунт.
type Subject struct {
	ID            int64
	TelegramID    *int64
	Email         *string
	Name          *string
	GoogleSub     *string
	RemnawaveUser *string
	RefCode       *string
	Source        string
	MergedInto    *int64
	CreatedAt     time.Time
}
