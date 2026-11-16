package web

import (
	"context"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTicketCompensationRequiresStaffAndCSRFAndSurvivesPanelOutage(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	s.cfg.SupportCompensationDailyDays = 3
	owner, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := db.EnsureTelegramSubject(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddStaff(ctx, 200, storage.RoleSupport, "Support", 100); err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Issue", "Client question", "human")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/support/tickets/%d", ticket.ID)
	action := path + "/compensate"
	customerSession := sessionCookieFor(t, db, owner.ID)
	staffSession := sessionCookieFor(t, db, staff.ID)
	csrf, cookies := csrfFrom(t, s.Handler())
	if w := postForm(t, s.Handler(), action, url.Values{"csrf": {csrf}}, append(cookies, customerSession)); w.Code != 403 {
		t.Fatal("customer can self-compensate", w.Code)
	}
	if w := postForm(t, s.Handler(), action, url.Values{}, []*http.Cookie{staffSession}); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	page := func(cookie *http.Cookie) string {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("ticket page", w.Code)
		}
		return w.Body.String()
	}
	if strings.Contains(page(customerSession), action) {
		t.Fatal("compensation visible to customer")
	}
	if !strings.Contains(page(staffSession), action) {
		t.Fatal("compensation missing for staff")
	}
	for i := 0; i < 2; i++ {
		w := postForm(t, s.Handler(), action, url.Values{"csrf": {csrf}}, append(cookies, staffSession))
		if w.Code != 303 {
			t.Fatal("compensation failed", w.Code, w.Body.String())
		}
	}
	if strings.Contains(page(staffSession), action) {
		t.Fatal("completed compensation button still visible")
	}
	body := page(customerSession)
	if !strings.Contains(body, "Поддержка оформила компенсацию") {
		t.Fatal("customer history lost compensation")
	}
	var orders int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE provider='support_comp' AND status='paid'`).Scan(&orders); err != nil || orders != 1 {
		t.Fatal("queued compensation duplicated or lost", orders, err)
	}
	if _, err := db.RemoveStaff(ctx, 200); err != nil {
		t.Fatal(err)
	}
	if w := postForm(t, s.Handler(), action, url.Values{"csrf": {csrf}}, append(cookies, staffSession)); w.Code != 403 {
		t.Fatal("revoked staff accepted", w.Code)
	}
}

func TestUnknownStaffRoleCannotReadForeignTicket(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureTelegramSubject(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddStaff(ctx, 200, "future-role", "", 100); err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Issue", "Private text", "human")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/tickets", fmt.Sprintf("/support/tickets/%d", ticket.ID)} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(sessionCookieFor(t, db, other.ID))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 && w.Code != 404 {
			t.Fatal("unknown role read protected page", path, w.Code)
		}
	}
}
