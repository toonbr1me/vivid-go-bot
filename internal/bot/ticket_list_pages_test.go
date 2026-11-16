package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestTicketListsReachEveryPageAndRememberReturn(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner, err := db.EnsureTelegramSubject(ctx, 4411)
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureTelegramSubject(ctx, 4412)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 23; i++ {
		if _, err = db.CreateTicket(ctx, owner.ID, owner.TelegramID, fmt.Sprintf("Issue %d", i), "message", ""); err != nil {
			t.Fatal(err)
		}
	}
	foreign, err := db.CreateTicket(ctx, other.ID, other.TelegramID, "Foreign issue", "message", "")
	if err != nil {
		t.Fatal(err)
	}
	// Identical timestamps used to make LIMIT-only pages unstable.
	_, err = db.Pool.Exec(ctx, `UPDATE support_tickets SET created_at='2026-01-01T00:00:00Z'`)
	if err != nil {
		t.Fatal(err)
	}
	b := &Bot{db: db, cfg: &config.Settings{AdminIDs: map[int64]bool{4411: true}}}
	seen := map[string]bool{}
	for page := 0; page < 3; page++ {
		text, kb := b.ticketsScreen(ctx, 4411, false, page)
		if !strings.Contains(text, fmt.Sprintf("Страница %d из 3", page+1)) {
			t.Fatal(text)
		}
		for _, row := range kb.InlineKeyboard {
			for _, button := range row {
				if strings.HasPrefix(button.CallbackData, "ticket:") {
					if button.CallbackData == fmt.Sprintf("ticket:%d", foreign.ID) || seen[button.CallbackData] {
						t.Fatalf("foreign or duplicate ticket: %s", button.CallbackData)
					}
					seen[button.CallbackData] = true
				}
			}
		}
	}
	if len(seen) != 23 {
		t.Fatalf("hidden tickets: %d", len(seen))
	}
	last, err := db.TicketsForSubjectPage(ctx, owner.ID, -1, 10)
	if err != nil {
		t.Fatal(err)
	}
	text, kb := b.ticketsScreen(ctx, 4411, false, 1000000)
	if !strings.Contains(text, "Страница 3 из 3") {
		t.Fatal(text)
	}
	for _, row := range kb.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == "support:list:3" {
				t.Fatal("invalid next page")
			}
		}
	}
	conversation, err := b.loadTicketConversation(ctx, 4411, last.Tickets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.actions[len(conversation.actions)-1].CallbackData != "support:list:2" {
		t.Fatal("return lost page")
	}
	text, _ = b.adminTicketsScreen(ctx, 4411, -1)
	if !strings.Contains(text, "Страница 3 из 3") {
		t.Fatal(text)
	}
	// Closing the last-page items must clamp an old button to the new last page.
	for _, ticket := range last.Tickets {
		if _, err = db.CloseTicket(ctx, ticket.ID); err != nil {
			t.Fatal(err)
		}
	}
	page, err := db.TicketsForSubjectPage(ctx, other.ID, 100, 10)
	if err != nil || page.Total != 1 || len(page.Tickets) != 1 {
		t.Fatalf("scope: %+v %v", page, err)
	}
	queue, err := db.QueueTicketsPage(ctx, 4411, 100, 10)
	if err != nil || queue.Total != 21 || queue.Page != 2 || len(queue.Tickets) != 1 {
		t.Fatalf("queue: %+v %v", queue, err)
	}
}

func TestEmptyTicketPageHasNoInvalidArrows(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner, err := db.EnsureTelegramSubject(ctx, 4488)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.TicketsForSubjectPage(ctx, owner.ID, -20, 10)
	if err != nil || page.Page != 0 || page.Pages != 1 || page.Total != 0 || len(ticketListPager(page, "support:list")) != 0 {
		t.Fatalf("empty: %+v %v", page, err)
	}
}
