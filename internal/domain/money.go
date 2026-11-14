// Package domain содержит чистые модели и правила, не знающие ни про базу,
// ни про Telegram, ни про HTTP. Всё денежное хранится в минорных единицах
// (копейках), чтобы арифметика не зависела от плавающей точки.
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Money — сумма в копейках. Ноль — допустимое значение.
type Money int64

// FromRubles переводит рубли в копейки.
func FromRubles(rubles int64) Money { return Money(rubles * 100) }

// Rubles возвращает целое число рублей (для каталога тарифов).
func (m Money) Rubles() int64 { return int64(m) / 100 }

// String форматирует сумму как "123.45".
func (m Money) String() string {
	sign := ""
	v := int64(m)
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// ParseMoney разбирает "123.45", "123,45" и "123".
func ParseMoney(raw string) (Money, error) {
	text := strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if text == "" {
		return 0, fmt.Errorf("пустая сумма")
	}
	parts := strings.SplitN(text, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("сумма %q: %w", raw, err)
	}
	if len(parts) == 1 {
		return Money(whole * 100), nil
	}
	frac := parts[1]
	if len(frac) > 2 {
		frac = frac[:2]
	}
	for len(frac) < 2 {
		frac += "0"
	}
	cents, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("сумма %q: %w", raw, err)
	}
	if whole < 0 {
		return Money(whole*100 - cents), nil
	}
	return Money(whole*100 + cents), nil
}

// ApplyDiscount вычитает процент и фиксированную сумму, не опускаясь ниже нуля.
func ApplyDiscount(price Money, percent int, fixed Money) Money {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	discounted := price - Money(int64(price)*int64(percent)/100) - fixed
	if discounted < 0 {
		return 0
	}
	return discounted
}

// QuotePrice применяет скидку и держит сумму в границах [minimum, price].
func QuotePrice(price Money, percent int, fixed Money, minimum Money) Money {
	amount := ApplyDiscount(price, percent, fixed)
	if amount > 0 {
		if amount < minimum {
			amount = minimum
		}
		if amount > price {
			amount = price
		}
	}
	return amount
}
