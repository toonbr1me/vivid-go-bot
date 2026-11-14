package appearance

import (
	"strings"
	"testing"
)

func TestParsePreservesDefaultsAndRejectsUnsafeTokens(t *testing.T) {
	theme, err := Parse([]byte(`{"accent":"#ffffff","navigation":"top"}`), Default())
	if err != nil || theme.Radius != 8 || !strings.Contains(theme.CSS(), "color:#111827") {
		t.Fatalf("partial theme or contrast failed: %+v %v", theme, err)
	}
	for _, payload := range []string{
		`{"accent":"red;}</style><script>alert(1)</script>"}`,
		`{"accent":"url(https://example.com)"}`, `{"radius":100}`, `{"spacing":0}`,
		`{"button_height":12}`, `{"navigation":"floating"}`, `{"heading":"giant"}`,
		`{"css":"body{}"}`, `{} {}`, `{} trailing`,
	} {
		if _, err := Parse([]byte(payload), Default()); err == nil {
			t.Errorf("accepted invalid theme: %s", payload)
		}
	}
	if contrast("#000000") != "#ffffff" || contrast("#ffffff") != "#111827" {
		t.Fatal("unreadable foreground")
	}
}
