package domain

import "testing"

// Витрина тарифов: порядок, «лучшая цена», лимит устройств.

func TestSortedPlans(t *testing.T) {
	plans := []Plan{
		{ID: "b", SortOrder: 20, IsActive: true},
		{ID: "a", SortOrder: 10, IsActive: true},
		{ID: "off", SortOrder: 5, IsActive: false},
		{ID: "trial", SortOrder: 1, IsActive: true, IsTrial: true},
	}
	got := SortedPlans(plans)
	if len(got) != 2 {
		t.Fatalf("в витрине %d тарифов, ждали 2: %+v", len(got), got)
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("порядок витрины: %s, %s", got[0].ID, got[1].ID)
	}
}

// При одинаковом порядке решает идентификатор — витрина не должна «прыгать».
func TestSortedPlansBreaksTieByID(t *testing.T) {
	plans := []Plan{
		{ID: "z", SortOrder: 10, IsActive: true},
		{ID: "a", SortOrder: 10, IsActive: true},
	}
	got := SortedPlans(plans)
	if got[0].ID != "a" {
		t.Fatalf("первым пошёл %s, ждали a", got[0].ID)
	}
}

func TestSortedDevicePacks(t *testing.T) {
	packs := []DevicePack{
		{ID: "b", SortOrder: 20, IsActive: true},
		{ID: "a", SortOrder: 10, IsActive: true},
		{ID: "off", SortOrder: 1, IsActive: false},
	}
	got := SortedDevicePacks(packs)
	if len(got) != 2 || got[0].ID != "a" {
		t.Fatalf("пакеты: %+v", got)
	}
}

// Цена за месяц: чем длиннее тариф, тем дешевле месяц.
func TestMonthlyPrice(t *testing.T) {
	if got := MonthlyPrice(Plan{Days: 30, Price: 30000}); got != 300 {
		t.Fatalf("MonthlyPrice = %d, ждали 300", got)
	}
	if got := MonthlyPrice(Plan{Days: 90, Price: 27000}); got != 90 {
		t.Fatalf("MonthlyPrice = %d, ждали 90", got)
	}
	// Тариф без срока — цена как есть.
	if got := MonthlyPrice(Plan{Days: 0, Price: 30000}); got != 300 {
		t.Fatalf("MonthlyPrice без срока = %d, ждали 300", got)
	}
}

// «Лучший» тариф — самый дешёвый по месяцу внутри группы устройств.
func TestBestPriceIDs(t *testing.T) {
	plans := []Plan{
		{ID: "month", Days: 30, Price: 30000, Devices: 1},
		{ID: "quarter", Days: 90, Price: 81000, Devices: 1},
		{ID: "other", Days: 30, Price: 10000, Devices: 3},
	}
	best := BestPriceIDs(plans)
	if !best["quarter"] {
		t.Fatalf("квартальный тариф должен быть лучшим: %+v", best)
	}
	if best["month"] {
		t.Fatalf("месячный тариф не должен быть лучшим: %+v", best)
	}
	if best["other"] {
		t.Fatalf("тариф другой группы не считается: %+v", best)
	}
}

// Один тариф в группе «лучшим» не помечается: сравнивать не с чем.
func TestBestPriceIDsNeedsCompany(t *testing.T) {
	best := BestPriceIDs([]Plan{{ID: "only", Days: 30, Price: 30000}})
	if len(best) != 0 {
		t.Fatalf("одиночный тариф помечен как лучший: %+v", best)
	}
}

// Одинаковая цена за месяц — оба лучшие.
func TestBestPriceIDsMarksEqualPrices(t *testing.T) {
	plans := []Plan{
		{ID: "a", Days: 30, Price: 30000},
		{ID: "b", Days: 60, Price: 60000},
	}
	best := BestPriceIDs(plans)
	if !best["a"] || !best["b"] {
		t.Fatalf("оба тарифа должны быть лучшими: %+v", best)
	}
}

// Пробные тарифы в сравнении не участвуют.
func TestBestPriceIDsIgnoresTrials(t *testing.T) {
	plans := []Plan{
		{ID: "trial", Days: 7, Price: 0, IsTrial: true},
		{ID: "a", Days: 30, Price: 30000},
	}
	best := BestPriceIDs(plans)
	if best["trial"] {
		t.Fatalf("пробный тариф помечен как лучший: %+v", best)
	}
}

// Тариф задаёт свой лимит устройств, иначе берётся базовый.
func TestPlanDevicesTotal(t *testing.T) {
	if got := PlanDevicesTotal(Plan{Devices: 7}, 5); got != 7 {
		t.Fatalf("лимит устройств = %d, ждали 7", got)
	}
	if got := PlanDevicesTotal(Plan{}, 5); got != 5 {
		t.Fatalf("лимит устройств = %d, ждали 5", got)
	}
}

// Каталог собирает пробные и платные тарифы вместе.
func TestCatalogue(t *testing.T) {
	catalogue := Catalogue(1, 7, PaidPlans())
	if len(catalogue) != 5 {
		t.Fatalf("в каталоге %d тарифов, ждали 5", len(catalogue))
	}
	bot := catalogue["trial_bot"]
	if !bot.IsTrial || bot.Days != 7 || bot.TrialKind != "bot" {
		t.Fatalf("пробный тариф бота: %+v", bot)
	}
	web := catalogue["trial_web"]
	if !web.IsTrial || web.Days != 1 || web.TrialKind != "web" {
		t.Fatalf("пробный тариф сайта: %+v", web)
	}
	if catalogue["30d"].Price != FromRubles(100) {
		t.Fatalf("платный тариф: %+v", catalogue["30d"])
	}
}
