package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

type supportProfilePanel struct{ journeyPanel }

func (supportProfilePanel) UsersByTelegramID(context.Context, int64) ([]remnawave.User, error) {
	return []remnawave.User{{ID: 42, UUID: "profile-uuid", Username: "client-profile", Status: "ACTIVE", ExpireAt: remnawave.Timestamp{Time: time.Now().Add(time.Hour)}}}, nil
}

func TestSupportUpdatesSendOnlyLatestAndKeepStaffMetadataPrivate(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	cfg := &config.Settings{BotStyle: "legacy", AdminIDs: map[int64]bool{100: true}}
	b := New(api, db, cfg, payments.New(db, supportProfilePanel{}, nil, cfg))
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Need help", "old-customer-message", "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddTicketMessage(ctx, ticket.ID, owner.ID, nil, "old-support-message", true, "", ""); err != nil {
		t.Fatal(err)
	}
	if err = b.SendTicketUpdate(ctx, 7, ticket.ID, "new-support-message <literal>", "support"); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Text        string                      `json:"text"`
		ReplyMarkup telego.InlineKeyboardMarkup `json:"reply_markup"`
	}
	if len(caller.bodies) != 1 {
		t.Fatal(caller.methods)
	}
	if err = json.Unmarshal([]byte(caller.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent.Text, "new-support-message &lt;literal&gt;") || strings.Contains(sent.Text, "old-") || strings.Contains(sent.Text, "Remnawave") {
		t.Fatal(sent.Text)
	}
	if !strings.Contains(fmt.Sprint(sent.ReplyMarkup), "ticket:reply:") || !strings.Contains(fmt.Sprint(sent.ReplyMarkup), "ticket:page:") {
		t.Fatal("missing reply/history")
	}
	if err = b.SendTicketUpdate(ctx, 8, ticket.ID, "private", "support"); err == nil {
		t.Fatal("foreign notification allowed")
	}
	staff, _, err := b.ticketSummaryScreen(ctx, 100, ticket.ID)
	if err != nil || !strings.Contains(staff, "Remnawave ID: <code>42</code>") || !strings.Contains(staff, "profile-uuid") {
		t.Fatal(staff, err)
	}
	customer, _, err := b.ticketSummaryScreen(ctx, 7, ticket.ID)
	if err != nil || strings.Contains(customer, "Remnawave") || strings.Contains(customer, "old-customer-message") {
		t.Fatal(customer, err)
	}
	if err = b.SendTicketUpdate(ctx, 7, ticket.ID, "Передаю вопрос сотруднику", "handoff"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(caller.bodies[len(caller.bodies)-1], "Статус обращения") {
		t.Fatal("handoff mislabeled as assistant reply")
	}
}
