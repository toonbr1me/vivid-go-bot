package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatEndpointBuildsOpenAICompatibleURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:11434":    "http://127.0.0.1:11434/v1/chat/completions",
		"http://127.0.0.1:11434/":   "http://127.0.0.1:11434/v1/chat/completions",
		"https://api.openai.com/v1": "https://api.openai.com/v1/chat/completions",
		"https://api.example/v1/":   "https://api.example/v1/chat/completions",
	}
	for input, want := range cases {
		if got := chatEndpoint(input); got != want {
			t.Errorf("chatEndpoint(%q) = %q, ждали %q", input, got, want)
		}
	}
}

func TestClientDisabledWithoutModel(t *testing.T) {
	if New("http://example.com", "key", "", time.Second).Enabled() {
		t.Error("клиент без модели должен быть выключен")
	}
	if New("", "key", "model", time.Second).Enabled() {
		t.Error("клиент без адреса должен быть выключен")
	}
	if !New("http://example.com", "key", "model", time.Second).Enabled() {
		t.Error("настроенный клиент должен быть включён")
	}
}

func TestChatSendsRequestAndReadsAnswer(t *testing.T) {
	var captured struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Temperature float64 `json:"temperature"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("путь = %q, ждали /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("заголовок авторизации = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("тело запроса: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Тарифы: 100 и 250"}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "secret", "gpt-test", 5*time.Second)
	answer, err := client.Chat(context.Background(), "правила", []Message{{Role: RoleUser, Content: "Сколько стоит?"}})
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	if answer != "Тарифы: 100 и 250" {
		t.Errorf("ответ = %q", answer)
	}
	if captured.Model != "gpt-test" {
		t.Errorf("модель = %q, ждали gpt-test", captured.Model)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("сообщений в запросе %d, ждали 2 (правила и вопрос)", len(captured.Messages))
	}
	if captured.Messages[0].Role != RoleSystem {
		t.Errorf("первое сообщение = %q, ждали system", captured.Messages[0].Role)
	}
	if captured.Temperature != 0 {
		t.Errorf("температура = %v, ждали 0: ответ должен быть повторяемым", captured.Temperature)
	}
}

func TestChatReportsModelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"model is overloaded"}}`))
	}))
	defer server.Close()

	client := New(server.URL, "", "gpt-test", 5*time.Second)
	_, err := client.Chat(context.Background(), "", []Message{{Role: RoleUser, Content: "?"}})
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if !strings.Contains(err.Error(), "overloaded") {
		t.Errorf("ошибка = %q: текст модели должен попасть в сообщение", err)
	}
}
