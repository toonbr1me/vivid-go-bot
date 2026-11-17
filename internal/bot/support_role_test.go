package bot

import "testing"

func TestSupportBotAcceptsOnlySupportCallbacks(t *testing.T) {
	for _, test := range []struct {
		kind, action string
		want         bool
	}{
		{"support", "new", true},
		{"ticket", "reply", true},
		{"queue", "open", true},
		{"staffticket", "answer", true},
		{"nav", "support", true},
		{"nav", "main", true},
		{"nav", "buy", false},
		{"pay", "30d", false},
		{"devices", "slots", false},
	} {
		if got := supportCallback(test.kind, test.action); got != test.want {
			t.Errorf("supportCallback(%q, %q) = %v, want %v", test.kind, test.action, got, test.want)
		}
	}
}
