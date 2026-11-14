package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// Переписка на сайте показывает разметку так же, как её увидели в боте:
// ответы помощника приходят в Markdown, и «**важно**» не должно висеть
// звёздочками.
func TestTicketShowsMarkdown(t *testing.T) {
	r, err := newRenderer(&config.Settings{WebDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	data := pageData{
		Site: r.theme,
		Meta: pageMeta{Title: "Обращение"},
		Year: now.Year(),
		Now:  now,
		Data: ticketData{
			Ticket: storage.Ticket{ID: 7, Subject: "Доступ", Status: "open", CreatedAt: now},
			Messages: []storage.Message{{
				ID:        1,
				Text:      "**Важно**\n\n- раз\n- два\n\n`код`",
				CreatedAt: now,
			}},
			Open: true,
		},
	}

	rec := httptest.NewRecorder()
	if err := r.render(rec, 200, "ticket.html", data); err != nil {
		t.Fatalf("страница не отрисовалась: %v", err)
	}
	html := rec.Body.String()
	for _, wanted := range []string{"<b>Важно</b>", "• раз", "<code>код</code>"} {
		if !strings.Contains(html, wanted) {
			t.Errorf("в выводе нет %q", wanted)
		}
	}
	if strings.Contains(html, "**Важно**") {
		t.Error("разметка не преобразована, звёздочки остались текстом")
	}
}

// Чужой HTML в сообщении остаётся текстом: разметка разметкой, а скрипт
// скриптом.
func TestTicketEscapesUserHTML(t *testing.T) {
	r, err := newRenderer(&config.Settings{WebDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	data := pageData{
		Site: r.theme,
		Meta: pageMeta{Title: "Обращение"},
		Year: now.Year(),
		Now:  now,
		Data: ticketData{
			Ticket: storage.Ticket{ID: 8, Status: "open", CreatedAt: now},
			Messages: []storage.Message{{
				ID:        1,
				Text:      `<script>alert(1)</script>`,
				CreatedAt: now,
			}},
			Open: true,
		},
	}

	rec := httptest.NewRecorder()
	if err := r.render(rec, 200, "ticket.html", data); err != nil {
		t.Fatalf("страница не отрисовалась: %v", err)
	}
	if html := rec.Body.String(); strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("пользовательский HTML попал на страницу как разметка")
	}
}
