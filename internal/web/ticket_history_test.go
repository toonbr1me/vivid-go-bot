package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFullTicketHistorySurvivesPageReloadAndStreamReconnect(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Full chat", "first-visible", "human")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO support_messages(ticket_id,message,is_from_admin,created_at) SELECT $1,'saved-message-'||n,false,now()+n*interval '1 second' FROM generate_series(1,510)n`, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO support_messages(ticket_id,message,is_internal) VALUES($1,'private-staff-note',true)`, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, closed := range []bool{false, true} {
		if closed {
			if _, err = db.CloseTicket(ctx, ticket.ID); err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest("GET", fmt.Sprintf("/support/tickets/%d", ticket.ID), nil)
		r.AddCookie(sessionCookieFor(t, db, owner.ID))
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, r)
		body := rec.Body.String()
		if rec.Code != 200 || !strings.Contains(body, "first-visible") || !strings.Contains(body, "saved-message-510") || strings.Contains(body, "private-staff-note") {
			t.Fatal("page omitted or leaked history", rec.Code)
		}
		payload, err := s.ticketPayload(ctx, ticket.ID)
		if err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]map[string]any)
		if len(messages) != 511 || messages[0]["text"] != "first-visible" || messages[510]["text"] != "saved-message-510" {
			t.Fatal("stream reconnect lost history")
		}
	}
}
