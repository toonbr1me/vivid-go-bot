package domain

import (
	"strconv"
	"strings"
	"time"
)

// Partner — партнёр программы. Он приводит клиентов по своим кодам и
// получает за это вознаграждение.
type Partner struct {
	ID            int64
	SubjectID     *int64
	Name          string
	Contact       string
	Status        string
	PayoutDetails string
	Notes         string
	CreatedAt     time.Time
}

// PartnerInviteCode — одноразовый код, по которому партнёр привязывает
// свой аккаунт к партнёрству.
type PartnerInviteCode struct {
	Code      string
	PartnerID int64
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// PartnerAttribution — кто пришёл к какому партнёру.
type PartnerAttribution struct {
	SubjectID int64
	PartnerID int64
	Code      *string
	Source    string
	CreatedAt time.Time
}

// PartnerReward — начисление партнёру за заказ.
type PartnerReward struct {
	ID        int64
	PartnerID int64
	OrderID   string
	SubjectID *int64
	Amount    Money
	Status    string
	CreatedAt time.Time
}

// PartnerSummary — сводка по партнёру для списка.
type PartnerSummary struct {
	Partner
	CodesCount   int
	ClientsCount int
	Rewarded     Money
	Accrued      Money
}

// Статусы партнёра.
const (
	PartnerActive = "active"
	PartnerPaused = "paused"
)

// Статусы начисления партнёру.
const (
	PartnerRewardAccrued   = "accrued"
	PartnerRewardCredited  = "credited"
	PartnerRewardCancelled = "cancelled"
)

// Область действия награды: только за первую покупку клиента или за все.
const (
	RewardScopeAll   = "all"
	RewardScopeFirst = "first"
)

// Тип награды: процент от тарифа или фиксированная сумма.
const (
	RewardTypePercent = "percent"
	RewardTypeFixed   = "fixed"
)

// PartnerRewardAmount считает вознаграждение партнёру за заказ.
//
// Процент берётся от суммы тарифа: продление пакетов устройств в неё не
// входит — за них партнёру не платят.
//
// Размер награды хранится в минорных единицах, как и остальные деньги:
// процент — тоже (10 % — это 1000). Иначе «10» в процентах и «10» в рублях
// были бы одной и той же величиной в разных смыслах.
func PartnerRewardAmount(rewardType string, rewardValue Money, amount, renewalAmount Money) Money {
	base := amount - renewalAmount
	if base < 0 {
		base = 0
	}
	switch rewardType {
	case RewardTypeFixed:
		if rewardValue < 0 {
			return 0
		}
		return rewardValue
	case RewardTypePercent:
		percent := int64(rewardValue) / 100
		if percent <= 0 {
			return 0
		}
		return Money(int64(base) * percent / 100)
	default:
		return 0
	}
}

// RewardValueFromInput переводит то, что ввёл человек, в минорные единицы.
//
// Для процента вводят «10» — это десять процентов, а не десять рублей.
// Для фиксированной суммы вводят сумму в рублях.
func RewardValueFromInput(rewardType, raw string) Money {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if raw == "" {
		return 0
	}
	if rewardType == RewardTypePercent {
		number, err := strconv.ParseFloat(raw, 64)
		if err != nil || number <= 0 {
			return 0
		}
		return Money(number * 100)
	}
	value, err := ParseMoney(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// RewardValueText — размер награды словами: «10 %» или «50 ₽».
func RewardValueText(rewardType string, rewardValue Money) string {
	switch rewardType {
	case RewardTypePercent:
		return strconv.FormatInt(int64(rewardValue)/100, 10) + " %"
	case RewardTypeFixed:
		return rewardValue.String() + " ₽"
	default:
		return ""
	}
}
