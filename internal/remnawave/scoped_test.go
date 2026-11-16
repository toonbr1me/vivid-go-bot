package remnawave

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScopedPanelIsolatesTelegramAndNames(t *testing.T) {
	var client *ScopedClient
	var requests int
	tg := int64(1315903018)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		mapped := client.offset + tg
		u := map[string]any{"id": 7, "username": "devgo_tg_1315903018", "telegramId": mapped, "subscriptionUrl": "https://main.example/sub/token"}
		switch {
		case r.Method == "POST":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["username"] != u["username"] || int64(body["telegramId"].(float64)) != mapped {
				t.Errorf("production identity sent: %#v", body)
			}
		case r.URL.Path == "/api/users/stream":
			if r.URL.Query().Get("telegramId") != fmt.Sprint(mapped) {
				t.Errorf("production Telegram lookup: %s", r.URL)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"users": []any{u, map[string]any{"id": 1, "username": "tg_1315903018", "telegramId": tg}}}})
			return
		case r.URL.Path != "/api/users/by-username/devgo_tg_1315903018":
			t.Errorf("unexpected request: %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": u})
	}))
	defer srv.Close()
	var err error
	client, err = NewScoped(New(srv.URL, "test", time.Second), "devgo", "https://test2.example")
	if err != nil {
		t.Fatal(err)
	}
	check := func(u User) {
		t.Helper()
		if u.Username != "tg_1315903018" || u.TelegramID == nil || *u.TelegramID != tg || u.SubscriptionURL != "https://test2.example/sub/token" {
			t.Fatalf("identity not restored: %+v", u)
		}
	}
	u, err := client.CreateUser(context.Background(), CreateUserParams{Username: "tg_1315903018", TelegramID: &tg, ExpireAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	check(u)
	users, err := client.UsersByTelegramID(context.Background(), tg)
	if err != nil || len(users) != 1 {
		t.Fatalf("lookup: %+v %v", users, err)
	}
	check(users[0])
	found, err := client.UserByUsername(context.Background(), "tg_1315903018")
	if err != nil || found == nil {
		t.Fatal(err)
	}
	check(*found)
	if requests != 3 {
		t.Fatal(requests)
	}
}

func TestScopedPanelRejectsProductionIDsBeforeAnyAction(t *testing.T) {
	var writes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/users/42" {
			writes++
			t.Errorf("escaped namespace: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"response":{"id":42,"username":"tg_123"}}`))
	}))
	defer srv.Close()
	c, err := NewScoped(New(srv.URL, "test", time.Second), "devgo", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, e1 := c.UpdateUser(ctx, UpdateUserParams{UserID: 42})
	_, e2 := c.UpdateExpireAt(ctx, 42, time.Now(), nil)
	_, e3 := c.HWIDDevices(ctx, 42)
	_, e4 := c.DeleteHWIDDevice(ctx, 42, "hwid")
	e5 := c.ResetTraffic(ctx, 42)
	for _, e := range []error{e1, e2, e3, e4, e5} {
		if e == nil {
			t.Fatal("production profile accepted")
		}
	}
	if writes != 0 {
		t.Fatal(writes)
	}
}

func TestScopedPanelUpdatesOwnProfileAndMapsLinkedTelegram(t *testing.T) {
	var c *ScopedClient
	tg := int64(123)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if int64(body["telegramId"].(float64)) != c.offset+tg {
				t.Errorf("unmapped Telegram: %v", body)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"id": 7, "username": "devgo_web_1", "telegramId": c.offset + tg}})
	}))
	defer srv.Close()
	c, _ = NewScoped(New(srv.URL, "test", time.Second), "devgo", "")
	u, err := c.UpdateUser(context.Background(), UpdateUserParams{UserID: 7, TelegramID: &tg})
	if err != nil || u.Username != "web_1" || *u.TelegramID != tg {
		t.Fatalf("%+v %v", u, err)
	}
	for _, bad := range []string{"", "../prod", "Production", "namespace_too_long"} {
		if _, err := NewScoped(c.client, bad, ""); err == nil {
			t.Fatalf("invalid namespace accepted: %q", bad)
		}
	}
	if _, err := NewScoped(c.client, "devgo", "http://example.com"); err == nil {
		t.Fatal("insecure origin accepted")
	}
}
