package domain

import "testing"

// Награда партнёру: процент или фиксированная сумма, в минорных единицах.

func TestPartnerRewardAmountPercent(t *testing.T) {
	// 10 % от 300 ₽ = 30 ₽ = 3000 копеек.
	got := PartnerRewardAmount(RewardTypePercent, 1000, FromRubles(300), 0)
	if got != 3000 {
		t.Fatalf("награда = %d, ждали 3000", got)
	}
}

// Продление пакетов устройств в базу награды не входит.
func TestPartnerRewardAmountExcludesRenewal(t *testing.T) {
	got := PartnerRewardAmount(RewardTypePercent, 1000, FromRubles(300), FromRubles(100))
	if got != 2000 {
		t.Fatalf("награда = %d, ждали 2000", got)
	}
}

// Продление больше суммы заказа не даёт отрицательной базы.
func TestPartnerRewardAmountNeverNegative(t *testing.T) {
	got := PartnerRewardAmount(RewardTypePercent, 1000, FromRubles(100), FromRubles(300))
	if got != 0 {
		t.Fatalf("награда = %d, ждали 0", got)
	}
}

// Фиксированная награда не зависит от суммы заказа.
func TestPartnerRewardAmountFixed(t *testing.T) {
	got := PartnerRewardAmount(RewardTypeFixed, 5000, FromRubles(300), FromRubles(100))
	if got != 5000 {
		t.Fatalf("награда = %d, ждали 5000", got)
	}
}

// Нулевое и отрицательное значение награды не даёт денег.
func TestPartnerRewardAmountRejectsNonPositive(t *testing.T) {
	if got := PartnerRewardAmount(RewardTypePercent, 0, FromRubles(300), 0); got != 0 {
		t.Fatalf("награда = %d, ждали 0", got)
	}
	if got := PartnerRewardAmount(RewardTypeFixed, -100, FromRubles(300), 0); got != 0 {
		t.Fatalf("награда = %d, ждали 0", got)
	}
	if got := PartnerRewardAmount("unknown", 1000, FromRubles(300), 0); got != 0 {
		t.Fatalf("награда = %d, ждали 0", got)
	}
}

// Ввод партнёра: «10» для процента — это десять процентов, а не десять рублей.
func TestRewardValueFromInput(t *testing.T) {
	cases := []struct {
		name string
		kind string
		raw  string
		want Money
	}{
		{"процент", RewardTypePercent, "10", 1000},
		{"процент с дробью", RewardTypePercent, "12.5", 1250},
		{"процент ноль", RewardTypePercent, "0", 0},
		{"процент мусор", RewardTypePercent, "abc", 0},
		{"сумма", RewardTypeFixed, "50", 5000},
		{"сумма с копейками", RewardTypeFixed, "50.25", 5025},
		{"сумма запятой", RewardTypeFixed, "50,25", 5025},
		{"сумма пустая", RewardTypeFixed, "  ", 0},
		{"сумма мусор", RewardTypeFixed, "abc", 0},
		{"отрицательная сумма", RewardTypeFixed, "-50", 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := RewardValueFromInput(test.kind, test.raw); got != test.want {
				t.Fatalf("RewardValueFromInput(%q, %q) = %d, ждали %d",
					test.kind, test.raw, got, test.want)
			}
		})
	}
}

// Подпись награды словами: «10 %» или «50.00 ₽».
func TestRewardValueText(t *testing.T) {
	if got := RewardValueText(RewardTypePercent, 1000); got != "10 %" {
		t.Fatalf("подпись = %q, ждали \"10 %%\"", got)
	}
	if got := RewardValueText(RewardTypeFixed, 5000); got != "50.00 ₽" {
		t.Fatalf("подпись = %q, ждали \"50.00 ₽\"", got)
	}
	if got := RewardValueText("unknown", 1000); got != "" {
		t.Fatalf("подпись = %q, ждали пусто", got)
	}
}
