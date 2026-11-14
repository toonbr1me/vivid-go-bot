package web

import (
	"context"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPaymentRedirectUsesConfirmedOwnedState(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	_, err := db.CreateOrder(ctx, domain.Order{OrderID: "awaiting", TelegramID: owner.TelegramID, AccountID: &owner.ID, Status: domain.OrderPending, Provider: "missing", Kind: domain.KindSubscription, Amount: 100, Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		subject *domain.Subject
		want    string
	}{{"anonymous", nil, "Проверьте результат оплаты"}, {"foreign", &other, "Проверьте результат оплаты"}, {"owner", &owner, "Ожидаем оплату"}} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/pay/success?order=awaiting", nil)
			if tt.subject != nil {
				r.AddCookie(sessionCookieFor(t, db, tt.subject.ID))
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, r)
			body := rec.Body.String()
			if rec.Code != 200 || !strings.Contains(body, tt.want) || strings.Contains(body, "Спасибо! Оплата получена") {
				t.Fatalf("misleading result: %d %s", rec.Code, body)
			}
		})
	}
}

type attachmentNotifier struct{ calls int }

func (*attachmentNotifier) SendText(context.Context, int64, string) error { return nil }
func (n *attachmentNotifier) OpenSupportFile(_ context.Context, id string) (io.ReadCloser, error) {
	n.calls++
	if id != "private-file-id" {
		return nil, fmt.Errorf("wrong file")
	}
	return io.NopCloser(strings.NewReader("file-bytes")), nil
}

func TestSupportAttachmentAndResolutionAreOwnerScoped(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	ticket, err := db.CreateTicketContent(ctx, owner.ID, owner.TelegramID, "Issue", "", "", "Заказ: test-order", "private-file-id", "photo")
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := db.TicketMessages(ctx, ticket.ID, 10)
	if len(messages) != 1 {
		t.Fatal("missing attachment")
	}
	path := fmt.Sprintf("/support/tickets/%d", ticket.ID)
	filePath := fmt.Sprintf("%s/files/%d", path, messages[0].ID)
	notifier := &attachmentNotifier{}
	s.notify = notifier
	for _, tt := range []struct {
		name    string
		subject *domain.Subject
		code    int
	}{{"anonymous", nil, 404}, {"foreign", &other, 404}, {"owner", &owner, 200}} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", filePath, nil)
			if tt.subject != nil {
				r.AddCookie(sessionCookieFor(t, db, tt.subject.ID))
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, r)
			if rec.Code != tt.code || strings.Contains(rec.Body.String(), "private-file-id") {
				t.Fatalf("file access: %d %s", rec.Code, rec.Body.String())
			}
			if rec.Code == 200 && (rec.Body.String() != "file-bytes" || rec.Header().Get("Cache-Control") != "private, no-store") {
				t.Fatal("unsafe file response")
			}
		})
	}
	if notifier.calls != 1 {
		t.Fatal("unauthorised file fetched from Telegram")
	}
	csrf, cookies := csrfFrom(t, s.Handler())
	cookies = append(cookies, sessionCookieFor(t, db, other.ID))
	if rec := postForm(t, s.Handler(), path+"/resolve", url.Values{"csrf": {csrf}}, cookies); rec.Code != 404 {
		t.Fatal("foreign resolution allowed")
	}
	cookies = cookies[:len(cookies)-1]
	cookies = append(cookies, sessionCookieFor(t, db, owner.ID))
	if rec := postForm(t, s.Handler(), path+"/resolve", url.Values{"csrf": {csrf}}, cookies); rec.Code != 303 {
		t.Fatal("owner cannot resolve")
	}
	postForm(t, s.Handler(), path, url.Values{"csrf": {csrf}, "message": {"stale reply"}}, cookies)
	updated, _ := db.TicketByID(ctx, ticket.ID)
	messages, _ = db.TicketMessages(ctx, ticket.ID, 10)
	if updated.Status != "closed" || len(messages) != 1 {
		t.Fatal("stale form reopened resolved ticket")
	}
}

func TestWebSupportPreservesContextAndRejectsForeignOrder(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	_, err := db.CreateOrder(ctx, domain.Order{OrderID: "owned-order", TelegramID: owner.TelegramID, AccountID: &owner.ID, Status: domain.OrderPending, Provider: "test", Kind: domain.KindSubscription, Amount: 100, Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	csrf, cookies := csrfFrom(t, s.Handler())
	cookies = append(cookies, sessionCookieFor(t, db, other.ID))
	values := url.Values{"csrf": {csrf}, "message": {"problem"}, "order": {"owned-order"}, "device": {"iPhone / iOS"}, "app": {"Happ"}}
	if rec := postForm(t, s.Handler(), "/support", values, cookies); rec.Code != 404 {
		t.Fatal("foreign order context accepted")
	}
	cookies = cookies[:len(cookies)-1]
	cookies = append(cookies, sessionCookieFor(t, db, owner.ID))
	if rec := postForm(t, s.Handler(), "/support", values, cookies); rec.Code != 303 {
		t.Fatal("support creation failed")
	}
	tickets, _ := db.TicketsForSubject(ctx, owner.ID, 10)
	if len(tickets) != 1 || !strings.Contains(tickets[0].Context, "owned-order") || !strings.Contains(tickets[0].Context, "iPhone / iOS") || !strings.Contains(tickets[0].Context, "Happ") {
		t.Fatal("context lost")
	}
}

func TestRecurringCancellationIgnoresForgedSubjectAndRequiresCSRF(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	other, _ := db.EnsureTelegramSubject(ctx, 8)
	if err := db.CreateRecurring(ctx, domain.RecurringAgreement{ID: "agreement", SubjectID: owner.ID, Provider: "yookassa", Method: "card", Amount: 10000, Currency: "RUB", Days: 30}); err != nil {
		t.Fatal(err)
	}
	csrf, cookies := csrfFrom(t, s.Handler())
	cookies = append(cookies, sessionCookieFor(t, db, other.ID))
	postForm(t, s.Handler(), "/account/recurring/cancel", url.Values{"csrf": {csrf}, "subject": {fmt.Sprint(owner.ID)}}, cookies)
	a, _ := db.RecurringByID(ctx, "agreement")
	if !a.Enabled {
		t.Fatal("foreign cancellation accepted")
	}
	cookies = cookies[:len(cookies)-1]
	cookies = append(cookies, sessionCookieFor(t, db, owner.ID))
	if rec := postForm(t, s.Handler(), "/account/recurring/cancel", url.Values{}, cookies); rec.Code != http.StatusForbidden {
		t.Fatal("missing csrf allowed")
	}
	postForm(t, s.Handler(), "/account/recurring/cancel", url.Values{"csrf": {csrf}}, cookies)
	a, _ = db.LatestRecurringForSubject(ctx, owner.ID)
	if a.Enabled || a.StopReason != "user" {
		t.Fatal("cancellation not preserved")
	}
}
