package storage_test

import (
	"context"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestTicketMessageKeepsAttachmentBot(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7001)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, subject.ID, subject.TelegramID, "Attachment", "Need help", "human")
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AddTicketMessageFromBot(ctx, ticket.ID, subject.ID, subject.TelegramID, "photo", false, "support-file", "photo", "support")
	if err != nil {
		t.Fatal(err)
	}
	if message.FileBot != "support" {
		t.Fatalf("file bot = %q", message.FileBot)
	}
	history, err := db.TicketHistory(ctx, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].FileBot != "support" {
		t.Fatalf("history lost attachment origin: %+v", history)
	}
}
