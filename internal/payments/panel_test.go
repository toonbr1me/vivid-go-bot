package payments

import (
	"context"
	"testing"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

// Без панели сервис должен говорить «панель недоступна», а не падать.
//
// Панель приходит интерфейсом, и nil-указатель внутри него — обычное
// дело: переменная объявлена как *Client и не создана. Такая панель
// проходит проверку «интерфейс не nil», а вызов падает. Раньше это роняло
// бота на главном экране.
func TestAccessWithoutPanelDoesNotPanic(t *testing.T) {
	db := testdb.Open(t)

	var panel *remnawave.Client
	svc := New(db, panel, &Registry{providers: map[string]Provider{}}, &config.Settings{
		Currency: "RUB",
	})

	subject, err := db.EnsureTelegramSubject(context.Background(), 7001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	access, _ := svc.Access(context.Background(), subject, "web_1")
	if access.Found {
		t.Error("без панели доступа быть не может")
	}
}

// Настоящий nil-интерфейс — то же самое.
func TestAccessWithNilPanelDoesNotPanic(t *testing.T) {
	db := testdb.Open(t)
	svc := New(db, nil, &Registry{providers: map[string]Provider{}}, &config.Settings{
		Currency: "RUB",
	})
	subject, err := db.EnsureTelegramSubject(context.Background(), 7002)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	access, _ := svc.Access(context.Background(), subject, "web_1")
	if access.Found {
		t.Error("без панели доступа быть не может")
	}
}
