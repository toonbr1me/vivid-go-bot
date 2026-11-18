package remnawave

import "testing"

func TestFirstConnectionDoesNotGuessFromMissingOrMalformedData(t *testing.T) {
	for _, tc := range []struct {
		raw              map[string]any
		known, connected bool
	}{
		{nil, false, false}, {map[string]any{"firstConnectedAt": nil}, true, false},
		{map[string]any{"firstConnectedAt": "broken"}, false, false},
		{map[string]any{"firstConnectedAt": true}, false, false},
		{map[string]any{"userTraffic": map[string]any{"firstConnectedAt": "2026-11-18T10:30:00Z"}}, true, true},
	} {
		known, first := FirstConnection(tc.raw)
		if known != tc.known || (first != nil) != tc.connected {
			t.Fatal(tc, known, first)
		}
	}
}
