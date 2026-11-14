package rich

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// payload — тело запроса, которое увидел тестовый сервер.
type payload struct {
	ChatID      int64 `json:"chat_id"`
	MessageID   int   `json:"message_id"`
	RichMessage struct {
		HTML string `json:"html"`
	} `json:"rich_message"`
	ReplyMarkup json.RawMessage `json:"reply_markup"`
}

func newServer(t *testing.T, response string) (*httptest.Server, *payload, *string) {
	t.Helper()
	got := &payload{}
	method := new(string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*method = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	return server, got, method
}

func TestSendPutsRichMessage(t *testing.T) {
	server, got, method := newServer(t, `{"ok":true,"result":{"message_id":42}}`)
	client := New("token", server.URL)

	id, err := client.Send(context.Background(), 7, Message{HTML: "<h1>Заголовок</h1>"}, nil)
	if err != nil {
		t.Fatalf("отправка: %v", err)
	}
	if id != 42 {
		t.Fatalf("message_id = %d, ждали 42", id)
	}
	if *method != "sendRichMessage" {
		t.Fatalf("метод %q, ждали sendRichMessage", *method)
	}
	if got.ChatID != 7 || got.RichMessage.HTML != "<h1>Заголовок</h1>" {
		t.Fatalf("тело запроса: %+v", got)
	}
}

func TestEditPutsMessageID(t *testing.T) {
	server, got, method := newServer(t, `{"ok":true,"result":true}`)
	client := New("token", server.URL)

	if err := client.Edit(context.Background(), 7, 15, Message{HTML: "<p>текст</p>"}, nil); err != nil {
		t.Fatalf("правка: %v", err)
	}
	if *method != "editMessageText" {
		t.Fatalf("метод %q, ждали editMessageText", *method)
	}
	if got.MessageID != 15 || got.ChatID != 7 {
		t.Fatalf("тело запроса: %+v", got)
	}
}

// Старый сервер не знает метод: клиент выключается, чтобы не повторять
// заведомо неудачный запрос на каждое сообщение.
func TestUnsupportedDisablesClient(t *testing.T) {
	server, _, _ := newServer(t, `{"ok":false,"error_code":404,"description":"Not Found: method not found"}`)
	client := New("token", server.URL)

	_, err := client.Send(context.Background(), 7, Message{HTML: "<p>текст</p>"}, nil)
	if err == nil || !IsUnsupported(err) {
		t.Fatalf("ждали неподдерживаемый метод, получили %v", err)
	}
	if client.Available() {
		t.Fatal("после отказа клиент должен быть выключен")
	}
}

// Правка тем же содержимым — не ошибка.
func TestEditIgnoresNotModified(t *testing.T) {
	server, _, _ := newServer(t, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`)
	client := New("token", server.URL)

	if err := client.Edit(context.Background(), 7, 15, Message{HTML: "<p>текст</p>"}, nil); err != nil {
		t.Fatalf("правка тем же текстом: %v", err)
	}
}
