package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Форматирование денег, сроков и объёмов — то, что человек видит чаще всего.

func TestPriceText(t *testing.T) {
	cases := []struct {
		name     string
		amount   domain.Money
		currency string
		want     string
	}{
		{"целые рубли без копеек", domain.FromRubles(300), "RUB", "300" + nbsp + "₽"},
		{"копейки остаются", 30050, "RUB", "300.50" + nbsp + "₽"},
		{"без валюты", domain.FromRubles(300), "", "300"},
		{"доллары", domain.FromRubles(10), "USD", "10" + nbsp + "$"},
		{"евро", domain.FromRubles(10), "EUR", "10" + nbsp + "€"},
		{"звёзды", domain.FromRubles(10), "XTR", "10" + nbsp + "★"},
		{"неизвестная валюта", domain.FromRubles(10), "PLN", "10" + nbsp + "PLN"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := priceText(test.amount, test.currency); got != test.want {
				t.Fatalf("priceText = %q, ждали %q", got, test.want)
			}
		})
	}
}

// Сумма не должна разваливаться переносом: разряд и валюта — вместе.
func TestPriceTextKeepsAmountTogether(t *testing.T) {
	got := priceText(domain.FromRubles(1200), "RUB")
	if !strings.Contains(got, nbsp+"₽") {
		t.Fatalf("цена = %q, ждали неразрывный пробел перед валютой", got)
	}
}

func TestDaysWord(t *testing.T) {
	cases := []struct {
		days int
		want string
	}{
		{1, "день"}, {2, "дня"}, {5, "дней"},
		{11, "дней"}, {12, "дней"}, {14, "дней"},
		{21, "день"}, {22, "дня"}, {25, "дней"},
		{0, "дней"}, {-1, "день"},
	}
	for _, test := range cases {
		if got := daysWord(test.days); got != test.want {
			t.Fatalf("daysWord(%d) = %q, ждали %q", test.days, got, test.want)
		}
	}
}

func TestDevicesWord(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "устройство"}, {2, "устройства"}, {5, "устройств"}, {11, "устройств"},
	}
	for _, test := range cases {
		if got := devicesWord(test.n); got != test.want {
			t.Fatalf("devicesWord(%d) = %q, ждали %q", test.n, got, test.want)
		}
	}
}

// Число разрядов отделяется неразрывным пробелом.
func TestGrouped(t *testing.T) {
	if got := grouped(1290); got != "1"+nbsp+"290" {
		t.Fatalf("grouped = %q", got)
	}
	if got := grouped(999); got != "999" {
		t.Fatalf("grouped = %q", got)
	}
	if got := grouped(-1290); got != "-1"+nbsp+"290" {
		t.Fatalf("grouped = %q", got)
	}
}

// Дни до срока: ноль — истекает сегодня, отрицательное — срок прошёл.
func TestDaysLeft(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if days, ok := daysLeft(nil, now); ok || days != 0 {
		t.Fatalf("без срока: (%d, %v)", days, ok)
	}
	zero := time.Time{}
	if _, ok := daysLeft(&zero, now); ok {
		t.Fatal("нулевое время — это не срок")
	}
	later := now.Add(50 * time.Hour)
	days, ok := daysLeft(&later, now)
	if !ok || days != 2 {
		t.Fatalf("дней = %d (ok=%v), ждали 2", days, ok)
	}
	earlier := now.Add(-25 * time.Hour)
	days, ok = daysLeft(&earlier, now)
	if !ok || days != -1 {
		t.Fatalf("дней = %d (ok=%v), ждали -1", days, ok)
	}
}

func TestBytesText(t *testing.T) {
	cases := []struct {
		value int64
		want  string
	}{
		{0, "0 Б"},
		{512, "512 Б"},
		{2048, "2.0 КБ"},
		{5 << 20, "5.0 МБ"},
		{3 << 30, "3.00 ГБ"},
		{2 << 40, "2.00 ТБ"},
	}
	for _, test := range cases {
		if got := bytesText(test.value); got != test.want {
			t.Fatalf("bytesText(%d) = %q, ждали %q", test.value, got, test.want)
		}
	}
}

// Полоса заполненности: цвет зависит от доли, длина всегда одна.
func TestTrafficBar(t *testing.T) {
	bar := trafficBar(0, 100, 10)
	if len([]rune(bar)) != 10 {
		t.Fatalf("полоса = %q, ждали 10 символов", bar)
	}
	if !strings.Contains(bar, "▫") {
		t.Fatalf("пустая полоса = %q", bar)
	}
	half := trafficBar(50, 100, 10)
	if !strings.Contains(half, "🟨") {
		t.Fatalf("на половине полоса жёлтая: %q", half)
	}
	full := trafficBar(150, 100, 10)
	if strings.Contains(full, "▫") {
		t.Fatalf("перерасход должен заполнять полосу: %q", full)
	}
	if !strings.Contains(full, "🟥") {
		t.Fatalf("перерасход должен быть красным: %q", full)
	}
	// Без лимита полоса пустая, но место занимает.
	empty := trafficBar(10, 0, 8)
	if len([]rune(empty)) != 8 {
		t.Fatalf("полоса без лимита = %q", empty)
	}
}

// Горизонт шкалы фиксированный: полугодовой тариф не выглядит «почти истёкшим».
func TestExpiryBar(t *testing.T) {
	fresh := expiryBar(30)
	if strings.Contains(fresh, "🟥") {
		t.Fatalf("свежая подписка не должна быть красной: %q", fresh)
	}
	ending := expiryBar(1)
	if !strings.Contains(ending, "🟥") {
		t.Fatalf("подписка на грани должна быть красной: %q", ending)
	}
	// Отрицательные дни не ломают шкалу.
	if got := expiryBar(-5); got == "" {
		t.Fatal("шкала должна рисоваться всегда")
	}
}

// Подписи статусов и способов оплаты не должны оставаться пустыми.
func TestStatusLabels(t *testing.T) {
	if orderStatusLabel(domain.OrderPending) == "" {
		t.Error("подпись ожидающей оплаты пустая")
	}
	if orderStatusLabel("что-то новое") != "что-то новое" {
		t.Error("неизвестный статус должен показываться как есть")
	}
	if methodLabel("") != "" {
		t.Error("пустой способ оплаты должен оставаться пустым")
	}
	if methodLabel("card") == "" || orderKindLabel(domain.KindSubscription) == "" {
		t.Error("подписи должны быть непустыми")
	}
}

// Пользовательский текст не должен становиться разметкой.
func TestEscapeHTML(t *testing.T) {
	got := escapeHTML(`<b>жирный</b> & "кавычки"`)
	if strings.Contains(got, "<b>") {
		t.Fatalf("тег не экранирован: %q", got)
	}
	if !strings.Contains(got, "&lt;b&gt;") {
		t.Fatalf("экранирование не сработало: %q", got)
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("12345"); got != "12345" {
		t.Fatalf("shortID = %q", got)
	}
	long := "01234567890123456789"
	got := shortID(long)
	if len([]rune(got)) != 13 {
		t.Fatalf("shortID = %q, ждали 12 символов и многоточие", got)
	}
}
