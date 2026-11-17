package jobs

import (
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"testing"
)

func TestConnectionReminderRequiresAnExplicitPanelFact(t *testing.T) {
	for _, c := range []struct {
		raw              map[string]any
		known, connected bool
	}{
		{nil, false, false},
		{map[string]any{"userTraffic": map[string]any{"usedTrafficBytes": 100}}, false, false},
		{map[string]any{"userTraffic": map[string]any{"firstConnectedAt": nil}}, true, false},
		{map[string]any{"firstConnectedAt": "2026-11-17T00:00:00Z"}, true, true},
	} {
		k, v := connectedState([]remnawave.User{{Raw: c.raw}})
		if k != c.known || v != c.connected {
			t.Fatalf("%+v: %v %v", c.raw, k, v)
		}
	}
}
