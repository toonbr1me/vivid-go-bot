package payments

import (
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// testSettings — настройки по умолчанию для тестов расчёта цен.
func testSettings() *config.Settings {
	return &config.Settings{
		Currency:         "RUB",
		MinPaymentAmount: domain.FromRubles(1),
	}
}

// testPack — пакет устройств ценой 99 рублей на 30 суток.
func testPack() domain.DevicePack {
	return domain.DevicePack{
		ID:       "pack1",
		Title:    "Пакет",
		Devices:  3,
		Price:    domain.FromRubles(99),
		Days:     30,
		IsActive: true,
	}
}
