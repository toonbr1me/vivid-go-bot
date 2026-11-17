package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func TestBothThemesRenderLimitedUnlimitedAndInheritedPlanTraffic(t *testing.T) {
	limited := int64(10 << 30)
	unlimited := int64(0)
	for name, dir := range map[string]string{"default": t.TempDir(), "berloga": filepath.Join("..", "..", "web", "themes", "berloga")} {
		t.Run(name, func(t *testing.T) {
			r, err := newRenderer(&config.Settings{WebDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name    string
				traffic *int64
				want    string
			}{{"limited", &limited, "10.0 ГБ"}, {"unlimited", &unlimited, "Безлимитный трафик"}, {"inherited", nil, "Трафик по условиям сервиса"}} {
				t.Run(tc.name, func(t *testing.T) {
					plan := domain.Plan{ID: "limited", Title: "Limited plan", Days: 30, Price: 10000, Devices: 3, TrafficLimitBytes: tc.traffic}
					pages := map[string]any{"buy.html": buyData{Quotes: []buyQuote{{Quote: domain.PlanQuote{Plan: plan, Amount: plan.Price, Price: plan.Price}}}, Selected: plan.ID}, "landing.html": landingData{Plans: []planCard{{Plan: plan, Devices: 3}}}}
					for page, data := range pages {
						response := httptest.NewRecorder()
						if err = r.render(response, 200, page, pageData{Site: r.theme, CSRF: "test", Data: data}); err != nil {
							t.Fatalf("%s failed: %v", page, err)
						}
						if !strings.Contains(response.Body.String(), tc.want) {
							t.Fatalf("%s lost plan limit: want %q", page, tc.want)
						}
					}
				})
			}
		})
	}
}
