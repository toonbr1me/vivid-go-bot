package bot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

// scriptedCaller отвечает по сценарию: первый вызов может быть отказом.
type scriptedCaller struct {
	bodies    []string
	methods   []string
	failFirst bool
}

func (c *scriptedCaller) Call(_ context.Context, url string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
	c.methods = append(c.methods, url[strings.LastIndex(url, "/")+1:])
	c.bodies = append(c.bodies, string(data.BodyRaw))
	if c.failFirst && len(c.methods) == 1 {
		return &telegoapi.Response{
			Ok: false,
			Error: &telegoapi.Error{
				ErrorCode:   400,
				Description: `Bad Request: can't parse entities: Unsupported start tag "b" at byte offset 0`,
			},
		}, nil
	}
	return &telegoapi.Response{
		Ok:     true,
		Result: json.RawMessage(`{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}`),
	}, nil
}

// Если Telegram не принял разметку, сообщение уходит простым текстом.
//
// Так бывает с текстом из чужих данных: непарный тег в имени или в ответе
// модели — и Telegram отвечает «can't parse entities». Молча потерять
// сообщение в этот момент нельзя: человек ждёт ответа.
func TestSendFallsBackToPlainText(t *testing.T) {
	caller := &scriptedCaller{failFirst: true}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		telego.WithAPICaller(caller))
	if err != nil {
		t.Fatalf("клиент: %v", err)
	}
	handler := New(api, nil, &config.Settings{BrandName: "Vivid", Currency: "RUB"}, nil)

	if err := handler.SendText(context.Background(), 1, "<b>сломанная разметка</b>"); err != nil {
		t.Fatalf("сообщение не доставлено: %v", err)
	}
	if len(caller.methods) != 2 {
		t.Fatalf("запросов %d, ждали два (разметка и запасной текст)", len(caller.methods))
	}
	if strings.Contains(caller.bodies[1], `"parse_mode"`) {
		t.Errorf("повтор ушёл с разметкой: %s", caller.bodies[1])
	}
	if !strings.Contains(caller.bodies[1], "сломанная разметка") {
		t.Errorf("в повторе потерялся текст: %s", caller.bodies[1])
	}
	for _, body := range caller.bodies {
		if strings.Contains(body, `"reply_markup"`) {
			t.Fatalf("отсутствующая клавиатура должна быть пропущена: %s", body)
		}
	}
}

// Удачная отправка не повторяется.
func TestSendDoesNotRetryOnSuccess(t *testing.T) {
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		telego.WithAPICaller(caller))
	if err != nil {
		t.Fatalf("клиент: %v", err)
	}
	handler := New(api, nil, &config.Settings{BrandName: "Vivid", Currency: "RUB"}, nil)

	if err := handler.SendText(context.Background(), 1, "<b>обычный текст</b>"); err != nil {
		t.Fatalf("сообщение не доставлено: %v", err)
	}
	if len(caller.methods) != 1 {
		t.Fatalf("запросов %d, ждали один", len(caller.methods))
	}
	if !strings.Contains(caller.bodies[0], `"parse_mode":"HTML"`) {
		t.Errorf("разметка не запрошена: %s", caller.bodies[0])
	}
	if strings.Contains(caller.bodies[0], `"reply_markup"`) {
		t.Fatalf("отсутствующая клавиатура должна быть пропущена: %s", caller.bodies[0])
	}
}
