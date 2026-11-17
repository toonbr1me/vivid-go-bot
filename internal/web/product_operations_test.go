package web

import (
	"context"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestProductPagesRespectRolesAndRenderTheirForms(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	if err := db.AddStaff(ctx, 200, "support", "Agent", 100); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Month", Days: 30, Price: 10000, IsActive: true, GiftEnabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{100, 200, 300} {
		subject, err := db.EnsureTelegramSubject(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		cookie := sessionCookieFor(t, db, subject.ID)
		for _, path := range []string{"/admin/promos", "/admin/partners", "/admin/plans", "/admin/settings", "/admin/staff", "/admin/withdrawals", "/admin/operations", "/admin/refunds", "/admin/customers/" + intText(subject.ID), "/gifts", "/history"} {
			r := httptest.NewRequest("GET", path, nil)
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			want := 200
			if strings.HasPrefix(path, "/admin") && id != 100 {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("%d %s: %d want %d: %s", id, path, w.Code, want, w.Body.String())
			}
		}
	}
}

func TestWebStaffNoteNeverAppearsInCustomerHistoryOrStream(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	customer, err := db.EnsureTelegramSubject(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, customer.ID, customer.TelegramID, "Issue", "Help", "human")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.EnsureTelegramSubject(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	token, cookies := csrfFrom(t, h)
	cookies = append(cookies, sessionCookieFor(t, db, owner.ID))
	path := "/support/tickets/" + intText(ticket.ID)
	w := postForm(t, h, path+"/staff", url.Values{"csrf": {token}, "action": {"note"}, "message": {"private-staff-note"}}, cookies)
	if w.Code != 303 {
		t.Fatalf("note: %d %s", w.Code, w.Body.String())
	}
	payload, err := s.ticketPayload(ctx, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(payload), "private-staff-note") {
		t.Fatal("note leaked through SSE")
	}
	for _, subject := range []domain.Subject{customer, owner} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(sessionCookieFor(t, db, subject.ID))
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		has := strings.Contains(w.Body.String(), "private-staff-note")
		if has != (subject.ID == owner.ID) {
			t.Fatal("note visibility is wrong")
		}
	}
}

func TestReadinessAndMetricsAuthentication(t *testing.T) {
	s, db := testServer(t)
	h := s.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	t.Setenv("METRICS_TOKEN", "test-metrics-token")
	for _, token := range []string{"", "wrong", "test-metrics-token"} {
		r := httptest.NewRequest("GET", "/metrics", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 404
		if token == "test-metrics-token" {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("metrics: %d %s", w.Code, w.Body.String())
		}
	}
	db.Close()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("unavailable database reported ready")
	}
}

func intText(id int64) string { return strconv.FormatInt(id, 10) }

func TestSettingsSaveUpdatesPaymentRulesWithoutRestart(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, err := db.EnsureTelegramSubject(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOT_TOKEN", "123:test")
	t.Setenv("DATABASE_URL", "postgres://localhost/vivid_test")
	t.Setenv("ADMIN_IDS", "100")
	t.Setenv("API_BASE_URL", "")
	t.Setenv("API_TOKEN", "")
	h := s.Handler()
	token, cookies := csrfFrom(t, h)
	cookies = append(cookies, sessionCookieFor(t, db, owner.ID))
	w := postForm(t, h, "/admin/settings", url.Values{"csrf": {token}, "TRIAL_BOT_DAYS": {"4"}, "BRAND_NAME": {"Updated service"}, "ADMIN_IDS": {"999"}}, cookies)
	if w.Code != 303 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	plans, err := s.pay.Plans(ctx)
	if err != nil || plans["trial_bot"].Days != 4 {
		t.Fatalf("payment rules stale: %+v %v", plans, err)
	}
	if !s.cfg.Current().AdminIDs[100] || s.cfg.Current().AdminIDs[999] {
		t.Fatal("settings escalated privileges")
	}
	r := httptest.NewRequest("GET", "/admin/settings", nil)
	r.AddCookie(cookies[len(cookies)-1])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Updated service") {
		t.Fatal("new configuration not visible")
	}
}
