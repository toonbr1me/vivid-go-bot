package remnawave

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Панель отвечает по-разному в зависимости от запроса, поэтому тест
// поднимает настоящий сервер и проверяет разбор ответов и ошибок.

// recorded — последний запрос к поддельной панели.
type recorded struct {
	request *http.Request
}

// testServer — поддельная панель: отдаёт заданный JSON и запоминает запрос.
func testServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *recorded) {
	t.Helper()
	last := &recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last.request = r
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server, last
}

// Разбор пользователя: срок, трафик, squad-ы — всё это приходит по-разному.
func TestUserUnmarshal(t *testing.T) {
	raw := `{
		"id": 42,
		"uuid": "uuid-1",
		"shortUuid": "short-1",
		"username": "user_42",
		"status": "ACTIVE",
		"telegramId": 1315903018,
		"email": "user@example.com",
		"subscriptionUrl": "https://panel.example/sub/short-1",
		"trafficLimitBytes": 107374182400,
		"hwidDeviceLimit": 5,
		"expireAt": "2030-01-02T03:04:05.000Z",
		"userTraffic": {"usedTrafficBytes": 1024},
		"activeInternalSquads": [{"uuid": "squad-a"}, "squad-b"]
	}`
	var user User
	if err := json.Unmarshal([]byte(raw), &user); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if user.ID != 42 || user.Username != "user_42" {
		t.Fatalf("пользователь: %+v", user)
	}
	if user.TelegramID == nil || *user.TelegramID != 1315903018 {
		t.Fatalf("telegramId: %v", user.TelegramID)
	}
	if user.TrafficLimitBytes == nil || *user.TrafficLimitBytes != 107374182400 {
		t.Fatalf("лимит трафика: %v", user.TrafficLimitBytes)
	}
	if user.HWIDDeviceLimit == nil || *user.HWIDDeviceLimit != 5 {
		t.Fatalf("лимит устройств: %v", user.HWIDDeviceLimit)
	}
	if user.TrafficUsedBytes != 1024 {
		t.Fatalf("израсходовано = %d, ждали 1024", user.TrafficUsedBytes)
	}
	if len(user.ActiveInternalSquads) != 2 ||
		user.ActiveInternalSquads[0] != "squad-a" ||
		user.ActiveInternalSquads[1] != "squad-b" {
		t.Fatalf("squad-ы: %+v", user.ActiveInternalSquads)
	}
	if user.ExpireAt.IsZero() {
		t.Fatal("срок не разобран")
	}
}

// Пользователь без срока не считается активным.
func TestUserIsActive(t *testing.T) {
	var user User
	if user.IsActive() {
		t.Fatal("пользователь без срока не активен")
	}
	user.ExpireAt = Timestamp{Time: time.Now().UTC().Add(time.Hour)}
	if !user.IsActive() {
		t.Fatal("пользователь с будущим сроком активен")
	}
	user.ExpireAt = Timestamp{Time: time.Now().UTC().Add(-time.Hour)}
	if user.IsActive() {
		t.Fatal("истёкший пользователь не активен")
	}
}

// Из ответа выбирается действующий пользователь, а не первый попавшийся.
func TestPickUser(t *testing.T) {
	if PickUser(nil) != nil {
		t.Fatal("пустой ответ — нет пользователя")
	}
	users := []User{
		{ID: 1, Status: "DISABLED"},
		{ID: 2, Status: "ACTIVE"},
	}
	picked := PickUser(users)
	if picked == nil || picked.ID != 2 {
		t.Fatalf("выбран %+v, ждали второго", picked)
	}
	// Если активных нет — берём первого, чтобы показать хоть что-то.
	only := []User{{ID: 3, Status: "LIMITED"}}
	if picked := PickUser(only); picked == nil || picked.ID != 3 {
		t.Fatalf("выбран %+v", picked)
	}
}

// Ссылка подписки: из shortUuid, если панель не отдала готовую.
func TestSubscriptionURL(t *testing.T) {
	if got := SubscriptionURL("https://panel.example", User{ShortUUID: "abc"}); got != "https://panel.example/api/sub/abc" {
		t.Fatalf("ссылка = %q", got)
	}
	if got := SubscriptionURL("https://panel.example/", User{ShortUUID: "abc"}); got != "https://panel.example/api/sub/abc" {
		t.Fatalf("ссылка = %q", got)
	}
	ready := "https://panel.example/sub/xyz"
	if got := SubscriptionURL("https://panel.example", User{ShortUUID: "abc", SubscriptionURL: ready}); got != ready {
		t.Fatalf("ссылка = %q, ждали %q", got, ready)
	}
	if got := SubscriptionURL("https://panel.example", User{}); got != "" {
		t.Fatalf("ссылка = %q, ждали пусто", got)
	}
}

// Поиск пользователя по Telegram: заголовок авторизации и разбор ответа.
func TestUsersByTelegramID(t *testing.T) {
	server, request := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"users":[
			{"id":1,"username":"a","status":"ACTIVE","expireAt":"2030-01-01T00:00:00.000Z"}
		]}}`))
	})
	client := New(server.URL, "secret", 5*time.Second)

	users, err := client.UsersByTelegramID(context.Background(), 42)
	if err != nil {
		t.Fatalf("поиск: %v", err)
	}
	if len(users) != 1 || users[0].Username != "a" {
		t.Fatalf("пользователи: %+v", users)
	}
	if request.request.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("авторизация = %q", request.request.Header.Get("Authorization"))
	}
	if request.request.URL.Query().Get("telegramId") != "42" {
		t.Fatalf("в запросе нет telegramId: %s", request.request.URL)
	}
}

// Ошибка панели не должна выглядеть как пустой успех.
func TestClientReportsPanelError(t *testing.T) {
	server, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"пользователь не найден"}`))
	})
	client := New(server.URL, "secret", 5*time.Second)

	_, err := client.UsersByTelegramID(context.Background(), 42)
	if err == nil {
		t.Fatal("ошибка панели должна возвращаться")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("в ошибке нет кода: %v", err)
	}
}

// Не-JSON от панели — тоже ошибка, а не пустой список.
func TestClientRejectsGarbage(t *testing.T) {
	server, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>nginx</html>`))
	})
	client := New(server.URL, "secret", 5*time.Second)

	if _, err := client.UsersByTelegramID(context.Background(), 42); err == nil {
		t.Fatal("не-JSON должен считаться ошибкой")
	}
}

// Удаление уже удалённого пользователя — не ошибка.
func TestDeleteUserHandlesMissing(t *testing.T) {
	server, _ := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	client := New(server.URL, "secret", 5*time.Second)

	deleted, err := client.DeleteUser(context.Background(), 7)
	if err != nil {
		t.Fatalf("удаление: %v", err)
	}
	if deleted {
		t.Fatal("удалённого пользователя не должно быть")
	}
}

// Создание пользователя: тело запроса уходит JSON-ом.
func TestCreateUserSendsBody(t *testing.T) {
	server, request := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"id":9,"username":"created"}}`))
	})
	client := New(server.URL, "secret", 5*time.Second)

	expire := time.Now().UTC().Add(24 * time.Hour)
	user, err := client.CreateUser(context.Background(), CreateUserParams{
		Username:   "created",
		ExpireAt:   expire,
		TelegramID: ptr(int64(42)),
	})
	if err != nil {
		t.Fatalf("создание: %v", err)
	}
	if user.Username != "created" {
		t.Fatalf("пользователь: %+v", user)
	}
	if request.request.Method != http.MethodPost {
		t.Fatalf("метод = %s", request.request.Method)
	}
	if request.request.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q", request.request.Header.Get("Content-Type"))
	}
}

func ptr[T any](value T) *T { return &value }
