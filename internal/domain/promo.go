package domain

import (
	"regexp"
	"strings"
	"time"
)

// PromoCode — правила применения кода. Хранится в базе как есть; снимок
// параметров вознаграждения копируется в заказ при оплате.
type PromoCode struct {
	Code                string
	DiscountPercent     int
	DiscountFixed       Money
	RepeatDiscountPct   int
	RepeatDiscountFixed Money
	BonusDays           int
	GrantDays           int
	MaxUses             int
	UsedCount           int
	PerUserLimit        int
	IsActive            bool
	ExpiresAt           *time.Time
	StartsAt            *time.Time
	PlanIDs             []string
	PartnerID           *int64
	RewardType          string
	RewardValue         Money
	RewardScope         string
}

var promoCodeRE = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,31}$`)

// NormalizePromoCode приводит ввод к каноническому виду.
func NormalizePromoCode(raw string) (string, bool) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	return code, promoCodeRE.MatchString(code)
}

// IsGiftPromo — код выдаёт дни напрямую, без тарифа.
func (p PromoCode) IsGiftPromo() bool { return p.GrantDays > 0 }

// AppliesToPlan проверяет ограничение по списку тарифов.
func (p PromoCode) AppliesToPlan(planID string) bool {
	if len(p.PlanIDs) == 0 {
		return true
	}
	for _, id := range p.PlanIDs {
		if id == planID {
			return true
		}
	}
	return false
}

// Live проверяет срок действия относительно момента now.
func (p PromoCode) Live(now time.Time) bool {
	if p.StartsAt != nil && now.Before(*p.StartsAt) {
		return false
	}
	if p.ExpiresAt != nil && !now.Before(*p.ExpiresAt) {
		return false
	}
	return true
}

// Exhausted — исчерпан общий лимит применений.
func (p PromoCode) Exhausted() bool {
	return p.MaxUses > 0 && p.UsedCount >= p.MaxUses
}

// PromoResolution — итог подбора кода под заказ.
type PromoResolution struct {
	Code            string
	DiscountPercent int
	DiscountFixed   Money
	BonusDays       int
	PartnerID       *int64
	RewardType      string
	RewardValue     Money
	RewardScope     string
}

// PromoOutcome — применённая скидка и снимок вознаграждения.
type PromoOutcome struct {
	Amount         Money
	OriginalAmount Money
	PromoCode      string
	PartnerID      *int64
	RewardType     string
	RewardValue    Money
	RewardScope    string
	BonusDays      int
}

// ApplyPromo считает итоговую сумму и заполняет снимок вознаграждения.
// Пустой код возвращает цену без изменений.
func ApplyPromo(price Money, planID string, promo *PromoCode, minimum Money) PromoOutcome {
	out := PromoOutcome{Amount: price, OriginalAmount: price}
	if promo == nil || !promo.AppliesToPlan(planID) {
		return out
	}
	out.PromoCode = promo.Code
	out.BonusDays = promo.BonusDays
	out.PartnerID = promo.PartnerID
	out.RewardType = promo.RewardType
	out.RewardValue = promo.RewardValue
	out.RewardScope = promo.RewardScope
	out.Amount = QuotePrice(price, promo.DiscountPercent, promo.DiscountFixed, minimum)
	return out
}
