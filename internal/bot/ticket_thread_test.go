package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestTicketChatKeepsConsecutiveMessagesAndSavedHistory(t *testing.T) {
	for _, clean := range []bool{false, true} {
		t.Run(fmt.Sprint(clean), func(t *testing.T) {
			db := testdb.Open(t)
			caller := &scriptedCaller{}
			api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
			cfg := &config.Settings{BotStyle: "legacy", BotCleanChat: clean, AdminIDs: map[int64]bool{100: true}}
			registry, _ := payments.BuildRegistry(cfg, nil)
			newBot := func() *Bot { return New(api, db, cfg, payments.New(db, nil, registry, cfg)) }
			b := newBot()
			ctx := (&telegohandler.Context{}).WithContext(context.Background())
			b.setInput(7, inputTicket, "")
			for i, text := range []string{"Первый вопрос", "Важное уточнение"} {
				if err := b.handleText(ctx, telego.Message{MessageID: 99 + i, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Text: text}); err != nil {
					t.Fatal(err)
				}
			}
			subject, _ := db.EnsureTelegramSubject(ctx, 7)
			tickets, _ := db.TicketsForSubject(ctx, subject.ID, 10)
			if len(tickets) != 1 {
				t.Fatalf("consecutive messages created %d tickets", len(tickets))
			}
			ticket := tickets[0]
			ref := fmt.Sprint(ticket.ID)
			if err := b.handleSupportAttachment(ctx, telego.Message{MessageID: 102, From: &telego.User{ID: 7}, Chat: telego.Chat{ID: 7, Type: "private"}, Caption: "Скриншот ошибки", Photo: []telego.PhotoSize{{FileID: "saved-photo"}}}); err != nil {
				t.Fatal(err)
			}
			b.beginTicketReply(ctx, telego.ChatID{ID: 100}, 0, 100, ref)
			for _, text := range []string{"Первый ответ", "Ещё одна инструкция"} {
				b.handleTicketReply(ctx, telego.ChatID{ID: 100}, 100, ref, text)
			}
			history, err := db.TicketHistory(ctx, ticket.ID)
			if err != nil || len(history) != 5 || history[2].FileID == nil || !history[4].FromAdmin {
				t.Fatalf("chat content lost: %+v %v", history, err)
			}
			if kind, gotRef := b.input(100); kind != inputTicketReply || gotRef != ref {
				t.Fatal("staff conversation ended after one answer")
			}
			b = newBot() // History survives loss of all in-memory input and view state.
			text, kb, err := b.ticketPageScreen(ctx, 7, ticket.ID, -1)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Первый вопрос", "Важное уточнение", "Скриншот ошибки", "Первый ответ", "Ещё одна инструкция", "Вы", "Поддержка", "<blockquote expandable>"} {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q: %s", want, text)
				}
			}
			if strings.Index(text, "Первый вопрос") > strings.Index(text, "Первый ответ") {
				t.Fatal("chat order reversed")
			}
			if !strings.Contains(fmt.Sprint(kb), "ticket:file:") {
				t.Fatal("attachment action missing")
			}
			if _, _, err = b.ticketPageScreen(ctx, 8, ticket.ID, -1); err == nil {
				t.Fatal("foreign user read conversation")
			}
			if _, err = db.CloseTicket(ctx, ticket.ID); err != nil {
				t.Fatal(err)
			}
			b.handleTicketReply(ctx, telego.ChatID{ID: 100}, 100, ref, "stale answer")
			history, _ = db.TicketHistory(ctx, ticket.ID)
			if len(history) != 5 {
				t.Fatal("closed conversation accepted a stale reply")
			}
			text, _, err = b.ticketPageScreen(ctx, 7, ticket.ID, -1)
			if err != nil || !strings.Contains(text, "Закрыто") || !strings.Contains(text, "Первый вопрос") {
				t.Fatal("closing lost history")
			}
		})
	}
}

func TestTicketChatPagesExposeHistoryBeyond500AndOldAttachments(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{BotStyle: "legacy"}, nil)
	s, _ := db.EnsureTelegramSubject(ctx, 7)
	ticket, err := db.CreateTicketContent(ctx, s.ID, s.TelegramID, "Long chat", "oldest-visible", "human", "", "old-photo", "photo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO support_messages(ticket_id,message,is_from_admin,created_at) SELECT $1,'entry-'||n,false,now()+n*interval '1 second' FROM generate_series(1,510)n`, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO support_messages(ticket_id,message,is_internal) VALUES($1,'staff-secret',true)`, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.loadTicketConversation(ctx, 7, ticket.ID)
	if err != nil || len(c.messages) != 511 {
		t.Fatalf("truncated history: %d %v", len(c.messages), err)
	}
	pages := b.ticketThreadPages(c)
	var all strings.Builder
	for i := range pages {
		text, _, err := b.ticketPageScreen(ctx, 7, ticket.ID, i)
		if err != nil || ticketTextLength(mdhtml.Plain(text)) > 4096 {
			t.Fatalf("bad page %d: %v size %d", i, err, len(text))
		}
		all.WriteString(text)
	}
	if !strings.Contains(all.String(), "oldest-visible") || !strings.Contains(all.String(), "entry-510") || strings.Contains(all.String(), "staff-secret") {
		t.Fatal("history missing or internal notes leaked")
	}
	b.ticketAttachment(ctx, "file", telego.ChatID{ID: 7}, 7, fmt.Sprint(c.messages[0].ID))
	if !strings.Contains(strings.Join(caller.methods, ","), "sendPhoto") {
		t.Fatal("old attachment became inaccessible")
	}
	text, kb, err := b.ticketPageScreen(ctx, 7, ticket.ID, 0)
	if err != nil || !strings.Contains(text, "страница 1 из") || !strings.Contains(fmt.Sprint(kb), "К последним сообщениям") {
		t.Fatal("missing history navigation")
	}
}

func TestTicketLongMessageSplitsWithoutLosingUnicodeOrMarkup(t *testing.T) {
	source := strings.Repeat("🐻 <tag> & \"строка\"\n", 800)
	parts := ticketTextParts(source, 1500)
	var restored strings.Builder
	for _, part := range parts {
		if ticketTextLength(html.UnescapeString(part)) > 1500 || strings.Contains(part, "<tag>") {
			t.Fatal("invalid escaped chunk")
		}
		restored.WriteString(html.UnescapeString(part))
	}
	if restored.String() != source {
		t.Fatal("message truncated or changed during pagination")
	}
}

func TestTicketChatCannotRenderIntoAGroup(t *testing.T) {
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, nil, &config.Settings{BotStyle: "legacy"}, nil)
	b.ticketCallback(context.Background(), "shared", telego.ChatID{ID: -100}, 0, 7, "1", "")
	if strings.Join(caller.methods, ",") != "answerCallbackQuery" {
		t.Fatal("group callback attempted to read or render history", caller.methods)
	}
}

func TestTicketExpandableTranscriptKeepsAnOrdinaryConversationTogether(t *testing.T) {
	b := &Bot{cfg: &config.Settings{}}
	c := ticketConversation{owner: true}
	fileID, fileType := "attachment", "photo"
	for i := 0; i < 25; i++ {
		m := storage.Message{ID: int64(i + 1), Text: fmt.Sprintf("Сообщение %d: <вопрос> & ответ 🐻", i), FromAdmin: i%2 == 1}
		if i < 8 {
			m.FileID = &fileID
			m.FileType = &fileType
		}
		c.messages = append(c.messages, m)
	}
	pages := b.ticketThreadPages(c)
	if len(pages) != 1 || len(pages[0].attachments) != 8 {
		t.Fatalf("ordinary conversation fragmented: %d pages", len(pages))
	}
	if strings.Count(pages[0].html, "<blockquote expandable>") != 1 || !strings.Contains(pages[0].html, "Сообщение 24") {
		t.Fatal("transcript is not one complete expandable quote")
	}
}

func TestTicketChatDeliveryKeepsNativeExpandableQuoteWhileReplying(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &scriptedCaller{}
	api, _ := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	b := New(api, db, &config.Settings{BotStyle: "legacy", RichMessages: true}, nil)
	stub, _ := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
	b.rich = stub.rich
	owner, _ := db.EnsureTelegramSubject(ctx, 7)
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Chat", "Question <literal>", "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddTicketMessage(ctx, ticket.ID, owner.ID, nil, "Support answer", true, "", ""); err != nil {
		t.Fatal(err)
	}
	b.beginTicketReply(ctx, telego.ChatID{ID: 7}, 0, 7, fmt.Sprint(ticket.ID))
	var request struct {
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}
	if len(caller.bodies) != 1 {
		t.Fatalf("unexpected delivery: %v", caller.methods)
	}
	if err := json.Unmarshal([]byte(caller.bodies[0]), &request); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<b>Обращение #", "<blockquote expandable>", "Question &lt;literal&gt;", "Support answer", "Вы отвечаете в это обращение"} {
		if !strings.Contains(request.Text, want) {
			t.Errorf("native transcript lacks %q: %s", want, request.Text)
		}
	}
	if request.ParseMode != "HTML" || strings.Join(caller.methods, ",") != "sendMessage" {
		t.Fatal("expandable transcript was not sent as native HTML", caller.methods)
	}
	if strings.Count(request.Text, "<blockquote expandable>") != 1 || strings.Contains(request.Text, "страница") {
		t.Fatal("short conversation was fragmented", request.Text)
	}
}
