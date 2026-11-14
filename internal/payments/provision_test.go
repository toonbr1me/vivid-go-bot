package payments

import (
	"testing"
	"time"
)

func TestExtendExpireAt(t *testing.T) {
	now := time.Now().UTC()

	// Остаток не сгорает: продление считается от конца текущей подписки.
	current := now.Add(10 * 24 * time.Hour)
	target := extendExpireAt(current, 30)
	want := current.AddDate(0, 0, 30)
	if !target.Equal(want) {
		t.Errorf("продление = %v, хотели %v", target, want)
	}

	// Истёкший срок не тянется в прошлое: продление начинается с сейчас.
	target = extendExpireAt(now.Add(-48*time.Hour), 30)
	if target.Before(now.Add(29 * 24 * time.Hour)) {
		t.Errorf("продление истёкшей подписки = %v, оно должно начинаться с сейчас", target)
	}

	// Человека нет: срок считается от сейчас.
	target = extendExpireAt(time.Time{}, 7)
	if target.Before(now.Add(6 * 24 * time.Hour)) {
		t.Errorf("срок нового пользователя = %v, хотели примерно неделю от сейчас", target)
	}
}

func TestQuoteDevicePackRoundsUpToRuble(t *testing.T) {
	service := &Service{cfg: testSettings()}
	pack := testPack()

	now := time.Now().UTC()
	expires := now.Add(30 * 24 * time.Hour)
	quote, err := service.quoteDevicePack(pack, expires, nil, now)
	if err != nil {
		t.Fatalf("quoteDevicePack: %v", err)
	}
	if quote.Amount != 9900 {
		t.Errorf("цена пакета = %s, хотели 99.00", quote.Amount)
	}
	if quote.BilledDays != 30 {
		t.Errorf("оплаченных дней = %d, хотели 30", quote.BilledDays)
	}

	// Половина срока — половина цены.
	quote, err = service.quoteDevicePack(pack, now.Add(15*24*time.Hour), nil, now)
	if err != nil {
		t.Fatalf("quoteDevicePack: %v", err)
	}
	if quote.Amount != 5000 {
		t.Errorf("цена за половину срока = %s, хотели 50.00", quote.Amount)
	}

	// Покрытый срок повторно не оплачивается: остаётся только остаток.
	covered := now.Add(20 * 24 * time.Hour)
	quote, err = service.quoteDevicePack(pack, now.Add(30*24*time.Hour), &covered, now)
	if err != nil {
		t.Fatalf("quoteDevicePack: %v", err)
	}
	if quote.Amount != 3300 {
		t.Errorf("цена за остаток = %s, хотели 33.00", quote.Amount)
	}

	// Слоты уже покрывают срок — покупать нечего.
	if _, err := service.quoteDevicePack(pack, now.Add(24*time.Hour), &covered, now); err == nil {
		t.Error("покупка сверх покрытого срока должна возвращать ошибку")
	}
}
