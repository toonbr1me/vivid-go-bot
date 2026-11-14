package support

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/llm"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type privacyTestFacts struct{}

func (privacyTestFacts) ServiceFacts(context.Context) (string, error) {
	return "Тариф: 100 рублей", nil
}
func (privacyTestFacts) UserFacts(context.Context, int64) (string, error) { return "", nil }

func TestAssistantRejectsSensitiveHistoryBeforeCallingModel(t *testing.T) {
	a := &Assistant{}
	for _, history := range [][]storage.Message{
		{{Text: "мой пароль abc"}, {Text: "Почему не работает подписка?"}},
		{{Text: "user@example.com"}, {Text: "Как подключить устройство?"}},
		{{FromAdmin: true, Text: "Ответ сотрудника"}, {Text: "Как оплатить?"}},
	} {
		// facts и llm намеренно отсутствуют: до них нельзя доходить.
		answer, err := a.answer(context.Background(), &storage.AITicket{}, history)
		if err != nil || answer != "" {
			t.Fatalf("история не отсеяна: %q %v", answer, err)
		}
	}
}

func TestAssistantSendsRulesAsSystemMessage(t *testing.T) {
	var got []llm.Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		got = request.Messages
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"100 рублей"}}]}`))
	}))
	defer server.Close()
	a := New(nil, llm.New(server.URL, "", "test", time.Second), &config.Settings{BrandName: "Тест"}, privacyTestFacts{}, LoadKnowledge(t.TempDir()), nil, nil)
	if _, err := a.answer(context.Background(), &storage.AITicket{}, []storage.Message{{Text: "Сколько стоит тариф?"}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Role != llm.RoleSystem || got[1].Role != llm.RoleUser {
		t.Fatalf("неверные роли: %+v", got)
	}
}
