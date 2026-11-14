package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

func TestExperiencePagesRespectRoles(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	if err := db.AddStaff(ctx, 200, storage.RoleSupport, "Support", 100); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{0, 100, 200, 555} {
		var cookie *http.Cookie
		if id != 0 {
			subject, err := db.EnsureTelegramSubject(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			cookie = sessionCookieFor(t, db, subject.ID)
		}
		for _, path := range []string{"/admin/screens", "/admin/appearance", "/admin/orders"} {
			r := httptest.NewRequest("GET", path, nil)
			if cookie != nil {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			want := 200
			if id == 0 {
				want = 303
			}
			if id == 555 || (id == 200 && path == "/admin/orders") {
				want = 403
			}
			if w.Code != want {
				t.Errorf("%d %s: %d, want %d", id, path, w.Code, want)
			}
		}
	}
}

func TestAppearanceSaveRequiresOwnerCSRFAndValidTokens(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	if err := db.AddStaff(ctx, 200, storage.RoleSupport, "Support", 100); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id     int64
		csrf   bool
		accent string
		want   int
		save   bool
	}{
		{100, false, "#225588", 403, false}, {200, true, "#225588", 403, false},
		{100, true, "red;}</style>", 303, false}, {100, true, "#225588", 303, true},
	} {
		subject, err := db.EnsureTelegramSubject(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		form := url.Values{"name": {"Test"}, "accent": {tc.accent}, "radius": {"12"}, "spacing": {"16"}, "button_height": {"44"}, "heading": {"compact"}, "navigation": {"top"}}
		if tc.csrf {
			form.Set("csrf", "valid-token")
		}
		r := httptest.NewRequest("POST", "/admin/appearance", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(sessionCookieFor(t, db, subject.ID))
		if tc.csrf {
			r.AddCookie(&http.Cookie{Name: csrfCookie, Value: "valid-token"})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("status %d, want %d", w.Code, tc.want)
		}
		value, err := db.AppSetting(ctx, appearance.SettingKey)
		if err != nil {
			t.Fatal(err)
		}
		if (value != "") != tc.save {
			t.Fatalf("unexpected persistence: %q", value)
		}
	}
}

func TestUnpaidOrderCannotBeRetried(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	id := int64(100)
	subject, err := db.EnsureTelegramSubject(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateOrder(ctx, domain.Order{OrderID: "unpaid", Provider: "demo", TelegramID: &id, AccountID: &subject.ID, PlanID: "test", Kind: domain.KindSubscription, Amount: domain.FromRubles(299), Currency: "RUB", Status: domain.OrderPending})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/admin/orders/unpaid/retry", strings.NewReader("csrf=valid-token"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(sessionCookieFor(t, db, subject.ID))
	r.AddCookie(&http.Cookie{Name: csrfCookie, Value: "valid-token"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatalf("unpaid retry returned %d", w.Code)
	}
	order, err := db.OrderByID(ctx, "unpaid")
	if err != nil || order.Status != domain.OrderPending || order.FulfillmentTries != 0 {
		t.Fatalf("unpaid order changed: %+v %v", order, err)
	}
}
