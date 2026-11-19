package web

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func TestBerlogaWorkspaceModes(t *testing.T) {
	r, err := newRenderer(&config.Settings{WebDir: filepath.Join("..", "..", "web", "themes", "berloga")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expiry := now.Add(18 * 24 * time.Hour)
	limit := int64(100 << 30)
	subject := domain.Subject{ID: 1}
	access := domain.Access{Found: true, Active: true, ExpireAt: &expiry, TrafficLimit: &limit, TrafficUsedBytes: 1 << 30, DeviceLimit: 3}
	for _, mini := range []bool{false, true} {
		mode := "site"
		if mini {
			mode = "miniapp"
		}
		for _, section := range []string{"subscription", "devices", "profile"} {
			rec := httptest.NewRecorder()
			err := r.render(rec, 200, "customer_account.html", pageData{Site: r.theme, MiniApp: mini, Subject: &subject, Path: "/account", Now: now, Data: accountData{Section: section, Access: access, DaysLeft: 18, DeviceLimit: 3}})
			if err != nil {
				t.Fatal(err)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "berloga customer-app customer-"+mode) || !strings.Contains(body, "/static/berloga/workspace.css") || !strings.Contains(body, "/static/berloga/fonts.css") {
				t.Fatal("brand shell not inherited", mode, section)
			}
			if strings.Contains(body, `class="topbar"`) == mini || strings.Contains(body, `class="foot"`) == mini {
				t.Fatal("site chrome leaked or missing", mode)
			}
			exportWorkspace(t, mode+"-"+section, rec.Body.Bytes())
		}
	}
	rec := httptest.NewRecorder()
	if err := r.render(rec, 200, "admin.html", pageData{Site: r.theme, Subject: &subject, Staff: adminSession{Owner: true}, Now: now, Data: adminDashboard{}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), "berloga berloga-staff admin-shell") || !strings.Contains(rec.Body.String(), "/admin/settings") {
		t.Fatal("admin shell or owner navigation missing")
	}
	exportWorkspace(t, "admin", rec.Body.Bytes())
}

func exportWorkspace(t *testing.T, name string, body []byte) {
	t.Helper()
	if out := os.Getenv("BERLOGA_WORKSPACE_EXPORT"); out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, name+".html"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCustomerShellFollowsSessionType(t *testing.T) {
	s, db := testServer(t)
	subject, err := db.EnsureTelegramSubject(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, mini := range []bool{false, true} {
		for _, path := range []string{"/account", "/account/devices", "/account/profile", "/buy", "/support", "/connect"} {
			cookie := sessionCookieFor(t, db, subject.ID)
			mode := "site"
			if mini {
				cookie.Name = miniAppSessionCookie
				mode = "miniapp"
			}
			req := httptest.NewRequest("GET", path, nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "customer-"+mode) {
				t.Fatalf("%s %s: status %d, wrong shell", mode, path, rec.Code)
			}
		}
	}
}
