package domain

import "sort"

// Plan — тариф подписки. Trial-тарифы живут здесь же, но с нулевой ценой:
// их источник — настройки, а платные приходят из таблицы plans.
type Plan struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Days      int    `json:"days"`
	Price     Money  `json:"price"`
	IsTrial   bool   `json:"is_trial"`
	TrialKind string `json:"trial_kind,omitempty"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
	Devices   int    `json:"devices"`
}

// DevicePack — пакет дополнительных слотов устройств.
type DevicePack struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Devices   int    `json:"devices"`
	Price     Money  `json:"price"`
	Days      int    `json:"days"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
}

const DefaultDeviceLimit = 5

// PaidPlans — стартовый каталог для пустой базы.
func PaidPlans() []Plan {
	return []Plan{
		{ID: "30d", Title: "1 месяц", Days: 30, Price: FromRubles(100), IsActive: true, SortOrder: 10},
		{ID: "90d", Title: "3 месяца", Days: 90, Price: FromRubles(250), IsActive: true, SortOrder: 20},
		{ID: "180d", Title: "6 месяцев", Days: 180, Price: FromRubles(450), IsActive: true, SortOrder: 30},
	}
}

// DefaultDevicePacks — стартовый каталог пакетов.
func DefaultDevicePacks() []DevicePack {
	return []DevicePack{
		{ID: "dev1", Title: "+1 устройство", Devices: 1, Price: FromRubles(30), IsActive: true, SortOrder: 10},
		{ID: "dev3", Title: "+3 устройства", Devices: 3, Price: FromRubles(70), IsActive: true, SortOrder: 20},
		{ID: "dev5", Title: "+5 устройств", Devices: 5, Price: FromRubles(100), IsActive: true, SortOrder: 30},
	}
}

// Catalogue собирает пробные и платные тарифы в один словарь.
func Catalogue(webTrialDays, botTrialDays int, paid []Plan) map[string]Plan {
	out := map[string]Plan{
		"trial_web": {
			ID: "trial_web", Title: "Пробный доступ", Days: webTrialDays,
			IsTrial: true, TrialKind: "web", IsActive: true,
		},
		"trial_bot": {
			ID: "trial_bot", Title: "Пробный доступ", Days: botTrialDays,
			IsTrial: true, TrialKind: "bot", IsActive: true,
		},
	}
	for _, plan := range paid {
		out[plan.ID] = plan
	}
	return out
}

// SortedPlans возвращает активные платные тарифы в порядке витрины.
func SortedPlans(plans []Plan) []Plan {
	out := make([]Plan, 0, len(plans))
	for _, plan := range plans {
		if plan.IsActive && !plan.IsTrial {
			out = append(out, plan)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// SortedDevicePacks возвращает активные пакеты в порядке витрины.
func SortedDevicePacks(packs []DevicePack) []DevicePack {
	out := make([]DevicePack, 0, len(packs))
	for _, pack := range packs {
		if pack.IsActive {
			out = append(out, pack)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// MonthlyPrice — цена за 30 дней, для подписи «выгоднее».
func MonthlyPrice(plan Plan) int64 {
	if plan.Days <= 0 {
		return plan.Price.Rubles()
	}
	return plan.Price.Rubles() * 30 / int64(plan.Days)
}

// BestPriceIDs возвращает тарифы с лучшей ценой за месяц внутри одной
// группы по числу устройств. Один тариф в группе «лучшим» не считается.
func BestPriceIDs(plans []Plan) map[string]bool {
	groups := map[int][]Plan{}
	for _, plan := range plans {
		if plan.IsTrial {
			continue
		}
		groups[plan.Devices] = append(groups[plan.Devices], plan)
	}
	best := map[string]bool{}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		cheapest := MonthlyPrice(group[0])
		for _, plan := range group[1:] {
			if price := MonthlyPrice(plan); price < cheapest {
				cheapest = price
			}
		}
		for _, plan := range group {
			if MonthlyPrice(plan) == cheapest {
				best[plan.ID] = true
			}
		}
	}
	return best
}

// PlanDevicesTotal — итоговый лимит устройств у тарифа.
func PlanDevicesTotal(plan Plan, base int) int {
	if plan.Devices > 0 {
		return plan.Devices
	}
	return base
}
