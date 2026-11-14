package domain

import "testing"

// Деньги — самое важное место в проекте: ошибка в копейках стоит реальных
// рублей. Проверяем разбор, печать и скидки.

// ParseMoney должен понимать то, что вводит человек.
func TestParseMoney(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Money
	}{
		{"целые рубли", "300", 30000},
		{"с копейками", "300.50", 30050},
		{"запятая", "300,50", 30050},
		{"одна копейка", "0.01", 1},
		{"пробелы", "  300  ", 30000},
		{"лишние копейки обрезаются", "300.555", 30055},
		{"одна цифра после точки", "300.5", 30050},
		{"ноль", "0", 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseMoney(test.raw)
			if err != nil {
				t.Fatalf("ParseMoney(%q): %v", test.raw, err)
			}
			if got != test.want {
				t.Fatalf("ParseMoney(%q) = %d, ждали %d", test.raw, got, test.want)
			}
		})
	}
}

// Мусор должен отклоняться, а не превращаться в ноль.
func TestParseMoneyRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"", "   ", "abc", "12abc", "1.2.3", "--5"} {
		if _, err := ParseMoney(raw); err == nil {
			t.Fatalf("ParseMoney(%q) должен был вернуть ошибку", raw)
		}
	}
}

// Отрицательная сумма разбирается со знаком: минус до или после запятой.
func TestParseMoneyNegative(t *testing.T) {
	got, err := ParseMoney("-12.34")
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if got != -1234 {
		t.Fatalf("ParseMoney = %d, ждали -1234", got)
	}
}

// Печать суммы: целые рубли без хвоста, копейки всегда двумя цифрами.
func TestMoneyString(t *testing.T) {
	cases := []struct {
		amount Money
		want   string
	}{
		{0, "0.00"},
		{FromRubles(300), "300.00"},
		{30050, "300.50"},
		{5, "0.05"},
		{-1234, "-12.34"},
	}
	for _, test := range cases {
		if got := test.amount.String(); got != test.want {
			t.Fatalf("Money(%d).String() = %q, ждали %q", test.amount, got, test.want)
		}
	}
}

func TestMoneyRubles(t *testing.T) {
	if got := FromRubles(300).Rubles(); got != 300 {
		t.Fatalf("Rubles = %d, ждали 300", got)
	}
	if got := Money(30050).Rubles(); got != 300 {
		t.Fatalf("Rubles = %d, ждали 300", got)
	}
}

// Скидка не может увести цену в минус и не может быть больше ста процентов.
func TestApplyDiscount(t *testing.T) {
	cases := []struct {
		name    string
		price   Money
		percent int
		fixed   Money
		want    Money
	}{
		{"десять процентов", 30000, 10, 0, 27000},
		{"фиксированная скидка", 30000, 0, 5000, 25000},
		{"процент и фиксированная", 30000, 10, 5000, 22000},
		{"не больше нуля", 3000, 50, 5000, 0},
		{"процент больше ста", 30000, 150, 0, 0},
		{"отрицательный процент игнорируется", 30000, -10, 0, 30000},
		{"без скидки", 30000, 0, 0, 30000},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := ApplyDiscount(test.price, test.percent, test.fixed); got != test.want {
				t.Fatalf("ApplyDiscount(%d, %d, %d) = %d, ждали %d",
					test.price, test.percent, test.fixed, got, test.want)
			}
		})
	}
}

// Итоговая цена держится в границах [минимум, исходная цена].
func TestQuotePrice(t *testing.T) {
	cases := []struct {
		name    string
		price   Money
		percent int
		fixed   Money
		minimum Money
		want    Money
	}{
		{"обычная скидка", 30000, 10, 0, 0, 27000},
		{"минимальная цена", 30000, 90, 0, 25000, 25000},
		{"минимальная цена не мешает скидке", 30000, 10, 0, 1000, 27000},
		{"нулевая цена остаётся нулевой", 0, 50, 0, 1000, 0},
		{"цена не растёт", 30000, -10, 0, 0, 30000},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := QuotePrice(test.price, test.percent, test.fixed, test.minimum)
			if got != test.want {
				t.Fatalf("QuotePrice(%d, %d, %d, %d) = %d, ждали %d",
					test.price, test.percent, test.fixed, test.minimum, got, test.want)
			}
		})
	}
}
