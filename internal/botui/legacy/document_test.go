package legacy

import (
	"strings"
	"testing"
)

func TestDocumentEscapesEveryUserControlledField(t *testing.T) {
	attack := `<script>alert("x")</script>`
	output := Document(Page{Title: attack, Status: attack, Kicker: attack, Value: attack, Note: attack, Body: attack, Updated: attack, Stats: []Stat{{Label: attack, Value: attack}}})
	if strings.Contains(output, "<script>") || strings.Contains(output, "</script>") {
		t.Fatal("untrusted markup escaped into rich document")
	}
	if strings.Count(output, "&lt;script&gt;") != 9 {
		t.Fatal("one of the user-controlled facts was lost or rendered without escaping")
	}
}

func TestDocumentKeepsConnectionStepsAndWarningsReadable(t *testing.T) {
	output := Document(Page{Title: "Подключение", Step: 2, Value: "Добавьте подписку", Body: "Первая проверка\n\nВторая проверка", Updated: "12:00", Stats: []Stat{{Label: "Устройства", Value: "4 / 3", Warn: true}, {Label: "Трафик", Value: "Без лимита", Warn: true}}})
	for _, text := range []string{"Шаг 2 из 2", "Добавьте подписку", "Первая проверка", "Вторая проверка", "Обновлено: 12:00", "4 / 3"} {
		if !strings.Contains(output, text) {
			t.Errorf("missing %q", text)
		}
	}
	if strings.Count(output, "<blockquote>") != 1 {
		t.Fatal("one exceeded limit produced duplicate warnings")
	}
	if strings.Contains(output, "<li></li>") {
		t.Fatal("empty lines created empty checklist items")
	}
}

func TestEmptyDocumentNeverInventsSubscriptionFacts(t *testing.T) {
	output := Document(Page{Title: "Подписка"})
	if strings.Contains(output, "<table") || strings.Contains(output, "<footer") || strings.Contains(output, "Активна") {
		t.Fatal("missing facts were replaced by misleading defaults")
	}
}
