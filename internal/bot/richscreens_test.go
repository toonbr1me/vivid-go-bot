package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mymmrac/telego"

	richmsg "github.com/toonbr1me/vivid-go-bot/internal/botui/legacy/transport"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

// Обычная клавиатура должна стать кнопками внутри rich-сообщения.
func TestRichButtonRows(t *testing.T) {
	kb := keyboard(
		[]telego.InlineKeyboardButton{
			button("Купить", "nav:buy"),
			urlButton("Сайт", "https://example.com"),
		},
		[]telego.InlineKeyboardButton{
			copyButton("Скопировать", "vless://link"),
		},
	)
	got, ok := richButtonRows(kb)
	if !ok {
		t.Fatal("клавиатура не преобразовалась")
	}
	for _, fragment := range []string{
		`<tg-button-row>`,
		`<tg-button type="callback_data" style="success" data="nav:buy">Купить</tg-button>`,
		`<tg-button type="url" url="https://example.com">Сайт</tg-button>`,
		`<tg-button type="copy_text" text="vless://link">Скопировать</tg-button>`,
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("в разметке нет %q:\n%s", fragment, got)
		}
	}
	if strings.Count(got, "<tg-button-row>") != 2 {
		t.Fatalf("рядов кнопок %d, ждали два:\n%s", strings.Count(got, "<tg-button-row>"), got)
	}
}

// Кнопка, которую нельзя выразить rich-тегом, оставляет клавиатуру обычной:
// иначе действие пропало бы.
func TestRichButtonRowsRejectsWebApp(t *testing.T) {
	kb := keyboard([]telego.InlineKeyboardButton{
		{Text: "Мини-приложение", WebApp: &telego.WebAppInfo{URL: "https://example.com"}},
	})
	if _, ok := richButtonRows(kb); ok {
		t.Fatal("web_app не должен превращаться в кнопку внутри текста")
	}
}

// Экранирование в атрибутах: кавычка не должна разорвать тег.
func TestRichButtonEscapesAttributes(t *testing.T) {
	tag, ok := richButton(button(`Кнопка "цитата"`, `data:"&`))
	if !ok {
		t.Fatal("кнопка не собралась")
	}
	if strings.Contains(tag, `data=""`) || strings.Contains(tag, `"&"`) {
		t.Fatalf("атрибут не экранирован: %s", tag)
	}
	if !strings.Contains(tag, `&quot;`) || !strings.Contains(tag, `&amp;`) {
		t.Fatalf("в атрибуте нет экранирования: %s", tag)
	}
}

type richRequest struct {
	RichMessage struct {
		HTML string `json:"html"`
	} `json:"rich_message"`
	ReplyMarkup json.RawMessage `json:"reply_markup"`
}

func richTestBot(t *testing.T, response string) (*Bot, *richRequest) {
	t.Helper()
	got := &richRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	return &Bot{
		cfg:  &config.Settings{RichMessages: true},
		rich: richmsg.New("token", server.URL),
	}, got
}

// Клавиатура уходит внутрь rich-сообщения, а не отдельным reply_markup.
func TestDeliverRichEmbedsKeyboard(t *testing.T) {
	handler, got := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
	kb := keyboard([]telego.InlineKeyboardButton{
		button("Купить", "nav:buy"),
	})
	ok, err := handler.deliverRich(context.Background(), telego.ChatID{ID: 7}, 0, "<h1>Экран</h1>", kb)
	if err != nil || !ok {
		t.Fatalf("rich не отправлен: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(got.RichMessage.HTML, "<tg-button-row>") ||
		!strings.Contains(got.RichMessage.HTML, `data="nav:buy"`) {
		t.Fatalf("кнопки не попали в текст: %s", got.RichMessage.HTML)
	}
	if len(got.ReplyMarkup) != 0 {
		t.Fatalf("клавиатура продублирована отдельно: %s", got.ReplyMarkup)
	}
}

// Неподдерживаемая кнопка оставляет клавиатуру обычной.
func TestDeliverRichFallsBackToReplyMarkup(t *testing.T) {
	handler, got := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
	kb := keyboard([]telego.InlineKeyboardButton{
		{Text: "Мини-приложение", WebApp: &telego.WebAppInfo{URL: "https://example.com"}},
	})
	ok, err := handler.deliverRich(context.Background(), telego.ChatID{ID: 7}, 0, "<p>Экран</p>", kb)
	if err != nil || !ok {
		t.Fatalf("rich не отправлен: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(string(got.ReplyMarkup), "inline_keyboard") {
		t.Fatalf("обычная клавиатура потерялась: %s", got.ReplyMarkup)
	}
}

// Старый сервер: rich не отправлен, вызывающий берёт обычный путь.
func TestDeliverRichReportsUnsupported(t *testing.T) {
	handler, _ := richTestBot(t, `{"ok":false,"error_code":404,"description":"Not Found: method not found"}`)
	ok, err := handler.deliverRich(context.Background(), telego.ChatID{ID: 7}, 0, "<p>Экран</p>", nil)
	if ok || err != nil {
		t.Fatalf("ждали отказ без ошибки, получили ok=%v err=%v", ok, err)
	}
	if handler.richAvailable() {
		t.Fatal("после неподдерживаемого метода rich должен выключиться")
	}
}
