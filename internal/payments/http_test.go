package payments

import (
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func TestParseMajorCents(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"123.45", 12345},
		{"123,45", 12345},
		{"123", 12300},
		{"123.4", 12340},
		{"123.456", 12345},
		{"0", 0},
		{"-50.25", -5025},
		{" 99.99 ", 9999},
	}
	for _, item := range cases {
		got, err := parseMajorCents(item.raw)
		if err != nil {
			t.Fatalf("parseMajorCents(%q): %v", item.raw, err)
		}
		if got != item.want {
			t.Errorf("parseMajorCents(%q) = %d, хотели %d", item.raw, got, item.want)
		}
	}
	if _, err := parseMajorCents("abc"); err == nil {
		t.Error("parseMajorCents(\"abc\") должен был вернуть ошибку")
	}
}

func TestMoneyFromMajorKeepsCents(t *testing.T) {
	// Шлюз может вернуть сумму числом: копейки теряться не должны.
	value := moneyFromMajor(123.45)
	if value == nil {
		t.Fatal("moneyFromMajor вернул nil")
	}
	if *value != 12345 {
		t.Errorf("moneyFromMajor(123.45) = %d, хотели 12345", *value)
	}
	if value := moneyFromMajor("99.90"); value == nil || *value != 9990 {
		t.Errorf("moneyFromMajor(\"99.90\") = %v, хотели 9990", value)
	}
	if value := moneyFromMajor(nil); value != nil {
		t.Errorf("moneyFromMajor(nil) = %v, хотели nil", value)
	}
}

func TestJSONStringKeepsDecimals(t *testing.T) {
	if got := jsonString(123.45); got != "123.45" {
		t.Errorf("jsonString(123.45) = %q, хотели \"123.45\"", got)
	}
	if got := jsonString(123.0); got != "123" {
		t.Errorf("jsonString(123.0) = %q, хотели \"123\"", got)
	}
	if got := jsonString(nil); got != "" {
		t.Errorf("jsonString(nil) = %q, хотели пустую строку", got)
	}
}

func TestStarsPayloadRoundTrip(t *testing.T) {
	orderID, stars, ok := ParseStarsPayload(StarsPayload("tg-1-abcdef", 42))
	if !ok {
		t.Fatal("ParseStarsPayload не разобрал свой же payload")
	}
	if orderID != "tg-1-abcdef" || stars != 42 {
		t.Errorf("получили %q/%d, хотели tg-1-abcdef/42", orderID, stars)
	}
	for _, broken := range []string{"", "abc", "abc|", "|42", "abc|0", "abc|xx"} {
		if _, _, ok := ParseStarsPayload(broken); ok {
			t.Errorf("ParseStarsPayload(%q) не должен был разобрать payload", broken)
		}
	}
}

func TestStarsForRoundsUp(t *testing.T) {
	provider := NewStarsProvider(nil, domain.FromRubles(10))
	if got := provider.StarsFor(domain.FromRubles(10)); got != 1 {
		t.Errorf("10 рублей = %d звёзд, хотели 1", got)
	}
	if got := provider.StarsFor(domain.FromRubles(11)); got != 2 {
		t.Errorf("11 рублей = %d звёзд, хотели 2", got)
	}
	if got := provider.StarsFor(domain.FromRubles(30)); got != 3 {
		t.Errorf("30 рублей = %d звёзд, хотели 3", got)
	}
}
