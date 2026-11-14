package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
)

func TestUniversalScreenUsesRichDelivery(t *testing.T) {
	handler, got := richTestBot(t, `{"ok":true,"result":{"message_id":3}}`)
	if err := handler.deliver(context.Background(), telego.ChatID{ID: 7}, 0, "<b>Поддержка</b>\n\nОтвет здесь\nВторая строка", keyboard([]telego.InlineKeyboardButton{button("Ответить", "ticket:reply:5")})); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<h3>Поддержка</h3>", "Ответ здесь<br/>Вторая строка", `data="ticket:reply:5"`} {
		if !strings.Contains(got.RichMessage.HTML, want) {
			t.Fatalf("нет %q: %s", want, got.RichMessage.HTML)
		}
	}
}

func TestUniversalScreenPreservesCodeNewlines(t *testing.T) {
	got := screenRichHTML("<b>Инструкция</b>\n\n<pre>one\ntwo</pre>\n\nСкопируйте код")
	if !strings.Contains(got, "<pre>one\ntwo</pre>") || strings.Contains(got, "<p><pre>") {
		t.Fatalf("испорчен код: %s", got)
	}
}

func TestUniversalScreenFallsBackWhenRichUnsupported(t *testing.T) {
	handler, _ := richTestBot(t, `{"ok":false,"error_code":404,"description":"method not found"}`)
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	handler.api = api
	if err := handler.deliver(context.Background(), telego.ChatID{ID: 7}, 0, "<b>Оплата</b>", nil); err != nil {
		t.Fatal(err)
	}
	if len(caller.methods) != 1 || caller.methods[0] != "sendMessage" {
		t.Fatalf("неверный резервный путь: %v", caller.methods)
	}
	if handler.richAvailable() {
		t.Fatal("неподдерживаемый rich не выключен")
	}
}
