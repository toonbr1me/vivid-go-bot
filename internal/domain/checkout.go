package domain

import "time"

// Checkout — то, что получает покупатель после нажатия «Оплатить».
//
// URL пуст у бесплатного заказа (промокод закрыл цену целиком) и у триала:
// платить нечего, доступ уже выдан. Reused показывает, что новый счёт не
// создавался, — человек вернулся на ту же ссылку, которую открывал раньше.
type Checkout struct {
	URL        string
	OrderID    string
	Amount     Money
	ExpiresAt  *time.Time
	Trial      bool
	Free       bool
	Reused     bool
	Stars      int
	DeviceFrom *time.Time
}

// PlanQuote — тариф с ценой после скидки и причиной отказа кода.
type PlanQuote struct {
	Plan      Plan
	Price     Money
	Amount    Money
	Rejection string
	BonusDays int
}

// DeviceQuote — пакет устройств, посчитанный за остаток подписки.
type DeviceQuote struct {
	Pack       DevicePack
	Amount     Money
	ExpiresAt  time.Time
	BilledDays int
}

// DeviceSlotView — строка экрана «Устройства».
type DeviceSlotView struct {
	OrderID    string
	Title      string
	Devices    int
	ExpiresAt  *time.Time
	Renewable  bool
	Price      Money
	PackID     string
	IsPlanSlot bool
}

// Access — состояние подписки глазами панели.
type Access struct {
	Found            bool
	Active           bool
	URL              string
	Username         string
	PanelUserID      int64
	PanelUserUUID    string
	ExpireAt         *time.Time
	TrafficUsedBytes int64
	TrafficLimit     *int64
	DeviceLimit      int
	Devices          []AccessDevice
}

// AccessDevice — привязанное устройство подписки.
type AccessDevice struct {
	HWID      string
	Platform  string
	Model     string
	CreatedAt time.Time
}
