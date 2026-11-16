package bot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

type unavailableDeviceListPanel struct{ journeyPanel }

func (unavailableDeviceListPanel) HWIDDevices(context.Context, int64) ([]remnawave.HWIDDevice, error) {
	return nil, errors.New("device endpoint unavailable")
}

func TestConnectionSurvivesDeviceListFailure(t *testing.T) {
	db := testdb.Open(t)
	cfg := &config.Settings{BrandName: "Vivid", Currency: "RUB", SiteEnabled: true, SiteBaseURL: "https://example.com"}
	registry, err := payments.BuildRegistry(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := New(nil, db, cfg, payments.New(db, unavailableDeviceListPanel{}, registry, cfg))
	card := b.connectHubCard(context.Background(), 7)
	if len(card.Pages) == 0 || card.Pages[0].Value == "Нужен действующий доступ" || !strings.Contains(card.Pages[0].Note, "Список устройств") {
		t.Fatal(card)
	}
	if len(card.Actions) == 0 {
		t.Fatal("connection actions hidden", card)
	}
}

func TestExpiredMenuUsesInactiveLayout(t *testing.T) {
	if menuState("EXPIRED") != menuState("NONE") {
		t.Fatal("expired subscription mapped to unknown")
	}
}
