package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func TestCustomerAccountRendersLimitedAndUnlimitedTraffic(t *testing.T) {
	limit, unlimited := int64(100<<30), int64(0)
	for _, theme := range []string{"", filepath.Join("..", "..", "web", "themes", "berloga"), filepath.Join("..", "..", "web", "themes", "vyzhivalovo")} {
		r, err := newRenderer(&config.Settings{WebDir: theme})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name  string
			limit *int64
			used  int64
			want  string
		}{
			{"zero usage", &limit, 0, "Использовано 0 Б"},
			{"positive usage", &limit, 1 << 30, "Использовано 1.0 ГБ"},
			{"over quota", &limit, 101 << 30, "Использовано 101.0 ГБ"},
			{"unlimited", &unlimited, 0, "без ограничения"},
			{"unknown limit", nil, 0, "без ограничения"},
		} {
			t.Run(theme+"/"+tc.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				data := accountData{Section: "subscription", Access: domain.Access{Found: true, Active: true, TrafficLimit: tc.limit, TrafficUsedBytes: tc.used}}
				err := r.render(rec, 200, "customer_account.html", pageData{Site: r.theme, Subject: &domain.Subject{ID: 1}, Data: data})
				if err != nil || rec.Code != 200 || !strings.Contains(rec.Body.String(), tc.want) {
					t.Fatalf("traffic account failed: %v status=%d body=%s", err, rec.Code, rec.Body.String())
				}
			})
		}
	}
}
