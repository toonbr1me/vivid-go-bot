package domain

import (
	"testing"
	"time"
)

// Промокод: разбор ввода, срок действия, лимиты и применение скидки.

func TestNormalizePromoCode(t *testing.T) {
	cases := []struct {
		raw   string
		want  string
		valid bool
	}{
		{"vivid", "VIVID", true},
		{"  summer-2026 ", "SUMMER-2026", true},
		{"A1", "A1", true},
		{"A", "A", false},
		{"", "", false},
		{"-lead", "-LEAD", false},
		{"with space", "WITH SPACE", false},
		{"длинный-код-с-кириллицей", "ДЛИННЫЙ-КОД-С-КИРИЛЛИЦЕЙ", false},
	}
	for _, test := range cases {
		got, ok := NormalizePromoCode(test.raw)
		if got != test.want || ok != test.valid {
			t.Fatalf("NormalizePromoCode(%q) = (%q, %v), ждали (%q, %v)",
				test.raw, got, ok, test.want, test.valid)
		}
	}
}

// Код без ограничения по тарифу подходит любому.
func TestPromoAppliesToPlan(t *testing.T) {
	promo := PromoCode{Code: "ALL"}
	if !promo.AppliesToPlan("30d") {
		t.Fatal("код без ограничений должен подходить любому тарифу")
	}
	limited := PromoCode{Code: "ONLY", PlanIDs: []string{"90d"}}
	if !limited.AppliesToPlan("90d") {
		t.Fatal("код должен подходить своему тарифу")
	}
	if limited.AppliesToPlan("30d") {
		t.Fatal("код не должен подходить чужому тарифу")
	}
}

// Срок действия: до начала и после конца код не работает.
func TestPromoLive(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name  string
		promo PromoCode
		want  bool
	}{
		{"без сроков", PromoCode{}, true},
		{"начался", PromoCode{StartsAt: &past}, true},
		{"ещё не начался", PromoCode{StartsAt: &future}, false},
		{"не кончился", PromoCode{ExpiresAt: &future}, true},
		{"уже кончился", PromoCode{ExpiresAt: &past}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := test.promo.Live(now); got != test.want {
				t.Fatalf("Live = %v, ждали %v", got, test.want)
			}
		})
	}
}

// Общий лимит применений: исчерпанный код больше не работает.
func TestPromoExhausted(t *testing.T) {
	if (PromoCode{MaxUses: 0, UsedCount: 99}).Exhausted() {
		t.Fatal("код без лимита не может быть исчерпан")
	}
	if !(PromoCode{MaxUses: 3, UsedCount: 3}).Exhausted() {
		t.Fatal("код должен считаться исчерпанным")
	}
	if (PromoCode{MaxUses: 3, UsedCount: 2}).Exhausted() {
		t.Fatal("код ещё не исчерпан")
	}
}

// Подарочный код выдаёт дни, а не скидку.
func TestPromoIsGift(t *testing.T) {
	if !(PromoCode{GrantDays: 7}).IsGiftPromo() {
		t.Fatal("код с днями — подарочный")
	}
	if (PromoCode{DiscountPercent: 10}).IsGiftPromo() {
		t.Fatal("код со скидкой не подарочный")
	}
}

// Промокод меняет цену и оставляет снимок награды для партнёра.
func TestApplyPromo(t *testing.T) {
	price := FromRubles(300)
	partner := int64(7)
	promo := &PromoCode{
		Code:            "SUMMER",
		DiscountPercent: 10,
		BonusDays:       3,
		PartnerID:       &partner,
		RewardType:      RewardTypePercent,
		RewardValue:     1000,
		RewardScope:     RewardScopeFirst,
	}
	out := ApplyPromo(price, "30d", promo, 0)
	if out.Amount != FromRubles(270) {
		t.Fatalf("цена после скидки = %s, ждали 270.00", out.Amount)
	}
	if out.OriginalAmount != price {
		t.Fatalf("исходная цена = %s, ждали 300.00", out.OriginalAmount)
	}
	if out.PromoCode != "SUMMER" || out.BonusDays != 3 {
		t.Fatalf("снимок промокода: %+v", out)
	}
	if out.PartnerID == nil || *out.PartnerID != partner {
		t.Fatalf("партнёр в снимке: %+v", out.PartnerID)
	}
	if out.RewardType != RewardTypePercent || out.RewardValue != 1000 || out.RewardScope != RewardScopeFirst {
		t.Fatalf("награда в снимке: %+v", out)
	}
}

// Без промокода цена не меняется.
func TestApplyPromoWithoutCode(t *testing.T) {
	price := FromRubles(300)
	out := ApplyPromo(price, "30d", nil, 0)
	if out.Amount != price || out.OriginalAmount != price {
		t.Fatalf("цена изменилась без промокода: %+v", out)
	}
	if out.PromoCode != "" || out.BonusDays != 0 {
		t.Fatalf("промокод без кода: %+v", out)
	}
}

// Промокод не действует на чужой тариф — цена остаётся.
func TestApplyPromoOtherPlan(t *testing.T) {
	price := FromRubles(300)
	promo := &PromoCode{Code: "ONLY90", DiscountPercent: 50, PlanIDs: []string{"90d"}}
	out := ApplyPromo(price, "30d", promo, 0)
	if out.Amount != price {
		t.Fatalf("цена для чужого тарифа = %s, ждали 300.00", out.Amount)
	}
	if out.PromoCode != "" {
		t.Fatalf("промокод применён к чужому тарифу: %+v", out)
	}
}
