package web

import (
	"strings"
	"testing"
)

func TestImportBridgeKeepsFixedSchemesAndEscapesURL(t *testing.T) {
	raw := "https://sub.example/path?a=1&b=2"
	for _, app := range []string{"happ", "incy", "clashmi", "flclashx"} {
		link, ok := importDeepLink(app, raw)
		if !ok || !strings.HasPrefix(link, app+"://") {
			t.Fatalf("missing scheme for %s", app)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://"} {
		if _, ok := importDeepLink("happ", raw); ok {
			t.Fatal("unsafe input accepted")
		}
	}
	if _, ok := importDeepLink("javascript", "https://example.com"); ok {
		t.Fatal("arbitrary scheme accepted")
	}
}
