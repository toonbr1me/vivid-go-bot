package support

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestIdleHandoffNotifiesCustomerAndPreservesOpenTicket(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.EnsureTelegramSubject(ctx, 6041)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, s.ID, s.TelegramID, "Help", "Still waiting", storage.SupportModeAssistant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE support_tickets SET updated_at=now()-interval '3 days' WHERE id=$1`, ticket.ID); err != nil {
		t.Fatal(err)
	}
	notifier := &captureNotifier{}
	a := &Assistant{db: db, notify: notifier, log: slog.Default()}
	if n, err := a.CloseIdle(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if len(notifier.texts) != 1 || !strings.Contains(notifier.texts[0], "остаётся открытым") {
		t.Fatal(notifier.texts)
	}
	updated, err := db.TicketByID(ctx, ticket.ID)
	if err != nil || updated.Status != "open" {
		t.Fatal(updated, err)
	}
	if n, err := a.CloseIdle(ctx); err != nil || n != 0 || len(notifier.texts) != 1 {
		t.Fatal("repeat handoff", n, err, notifier.texts)
	}
}
