package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

type viewCaller struct {
	methods    []string
	failWidget bool
}

func (c *viewCaller) Call(_ context.Context, url string, _ *telegoapi.RequestData) (*telegoapi.Response, error) {
	method := url[strings.LastIndex(url, "/")+1:]
	c.methods = append(c.methods, method)
	if c.failWidget && method == "sendWidget" {
		return &telegoapi.Response{Ok: false, Error: &telegoapi.Error{ErrorCode: 404, Description: "method not found"}}, nil
	}
	result, _ := json.Marshal(map[string]any{"message_id": len(c.methods), "date": 0, "chat": map[string]any{"id": 7, "type": "private"}})
	return &telegoapi.Response{Ok: true, Result: result}, nil
}

func TestViewSelectionPersistsAndRoutesAllText(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	caller := &viewCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil)
	if err := db.SetBotView(ctx, 7, "cards"); err != nil {
		t.Fatal(err)
	}
	if err := b.deliver(ctx, telego.ChatID{ID: 7}, 42, "<b>История</b>\n\nЗаказ", nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.methods, ",") != "sendWidget,deleteMessage" {
		t.Fatalf("не заменён экран карточкой: %v", caller.methods)
	}
	if err := db.SetBotView(ctx, 7, "classic"); err != nil {
		t.Fatal(err)
	}
	// Новый экземпляр бота читает выбор из базы после перезапуска.
	b = New(api, db, &config.Settings{}, nil)
	if err := b.deliver(ctx, telego.ChatID{ID: 7}, 0, "Обычный экран", nil); err != nil {
		t.Fatal(err)
	}
	if caller.methods[2] != "sendMessage" {
		t.Fatalf("не восстановлен обычный вид: %v", caller.methods)
	}
}

func TestViewSelectorReusesPinnedMessage(t *testing.T) {
	db := testdb.Open(t)
	caller := &viewCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil)
	if err := b.showViewSelector(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if err := b.showViewSelector(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(caller.methods, ","); got != "sendMessage,pinChatMessage,editMessageText,pinChatMessage" {
		t.Fatalf("выбор режима дублируется: %s", got)
	}
}

func TestCardFallbackKeepsOldMessageUntilDelivery(t *testing.T) {
	db := testdb.Open(t)
	caller := &viewCaller{failWidget: true}
	ctx := context.Background()
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil)
	if err := db.SetBotView(ctx, 7, "cards"); err != nil {
		t.Fatal(err)
	}
	if err := b.deliver(ctx, telego.ChatID{ID: 7}, 42, "<b>Оплата</b>", nil); err == nil {
		t.Fatal("rejected HTML update must preserve the selected transport and report its error")
	}
	if got := strings.Join(caller.methods, ","); got != "sendWidget" {
		t.Fatalf("опасный резервный путь: %s", got)
	}
}

func TestCardRejectsOversizeContent(t *testing.T) {
	if _, err := cardHTML(strings.Repeat("длинный текст ", 4000)); err == nil {
		t.Fatal("не проверен лимит карточки")
	}
}

func TestDeviceRemovalAsksBeforeCallingPanel(t *testing.T) {
	db := testdb.Open(t)
	caller := &viewCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := New(api, db, &config.Settings{}, nil) // Панель отсутствует: первое нажатие не должно её вызывать.
	b.deviceCallback(context.Background(), "q", telego.ChatID{ID: 7}, 0, 7, "rm", "device-tail")
	if got := strings.Join(caller.methods, ","); got != "answerCallbackQuery,sendMessage" {
		t.Fatalf("не показано подтверждение: %s", got)
	}
}
