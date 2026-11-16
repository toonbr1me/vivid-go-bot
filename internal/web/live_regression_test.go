package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoggingPreservesStreamingFlush(t *testing.T) {
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	flushed := make(chan error, 1)
	h := s.withLogging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "first-event\n")
		err := http.NewResponseController(w).Flush()
		flushed <- err
		if err != nil {
			return
		}
		<-r.Context().Done()
	}))
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := <-flushed; err != nil {
		t.Fatal("logging hides the streaming writer", err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "first-event\n" {
		t.Fatalf("stream was buffered: %q %v", line, err)
	}
}

func TestTicketStreamFlushesUpdatesForOwnerAndStaffOnly(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	owner, err := db.EnsureTelegramSubject(ctx, 701)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := db.EnsureTelegramSubject(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := db.EnsureTelegramSubject(ctx, 702)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, owner.ID, owner.TelegramID, "Streaming test", "First message", "human")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	for _, tc := range []struct {
		name      string
		subjectID int64
		allowed   bool
	}{
		{"owner", owner.ID, true}, {"staff", staff.ID, true}, {"stranger", stranger.ID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, fmt.Sprintf("%s/support/tickets/%d/updates", server.URL, ticket.ID), nil)
			req.AddCookie(sessionCookieFor(t, db, tc.subjectID))
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if !tc.allowed {
				if response.StatusCode != http.StatusNotFound {
					t.Fatalf("another client's stream is visible: %d", response.StatusCode)
				}
				return
			}
			if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatalf("SSE not opened: %d %s", response.StatusCode, response.Header.Get("Content-Type"))
			}
			reader := bufio.NewReader(response.Body)
			readMessages := func() []map[string]any {
				t.Helper()
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						t.Fatal("event did not arrive", err)
					}
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var event struct {
						Messages []map[string]any `json:"messages"`
					}
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
						t.Fatal(err)
					}
					return event.Messages
				}
			}
			before := readMessages()
			message := "Live update for " + tc.name
			if _, err := db.AddTicketMessage(ctx, ticket.ID, owner.ID, owner.TelegramID, message, false, "", ""); err != nil {
				t.Fatal(err)
			}
			s.hub.notify(ticket.ID)
			after := readMessages()
			found := false
			for _, item := range after {
				found = found || item["text"] == message
			}
			if len(after) != len(before)+1 || !found {
				t.Fatal("new message is absent from the stream")
			}
		})
	}
}
