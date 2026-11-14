package support

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

func TestClassifyAnswersServiceQuestions(t *testing.T) {
	questions := []string{
		"Сколько стоит подписка на месяц?",
		"Какие тарифы у вас есть?",
		"Как подключить устройство?",
		"Не пришла подписка после оплаты",
		"Сколько устройств можно подключить?",
		"Что такое промокод и как его применить?",
		"Когда истекает мой доступ?",
	}
	for _, question := range questions {
		if got := classify(question); got != decisionAnswer {
			t.Errorf("«%s» → %v, ждали ответ помощника", question, got)
		}
	}
}

func TestClassifyHandsOverToHuman(t *testing.T) {
	questions := []string{
		"Свяжитесь с человеком, пожалуйста",
		"Я хочу обжаловать оплату",
		"Нужен оператор поддержки",
		"Это срочно, подписка не работает",
		"Мой пароль от аккаунта 12345",
		"Пришлите код подтверждения",
		"Карта 4276 **** **** 1234",
		"Вчера был в горах",
	}
	for _, question := range questions {
		if got := classify(question); got != decisionToHuman {
			t.Errorf("«%s» → %v, ждали живого сотрудника", question, got)
		}
	}
}

// Разметка модели должна дойти до человека выделением, а не звёздочками.
func TestCleanAnswerKeepsMarkdown(t *testing.T) {
	answer := cleanAnswer("**Важно**\n\n\nПервая строка\n\nВторая строка\n\n`код`")
	if answer == "" {
		t.Fatal("ответ не должен быть пустым")
	}
	for _, wanted := range []string{"**Важно**", "`код`", "Первая строка", "Вторая строка"} {
		if !strings.Contains(answer, wanted) {
			t.Errorf("в ответе нет %q: %s", wanted, answer)
		}
	}
	if strings.Contains(answer, "\n\n\n") {
		t.Errorf("лишние пустые строки не убраны: %q", answer)
	}

	long := strings.Repeat("абв ", 2000)
	short := cleanAnswer(long)
	if len([]rune(short)) > answerLimit+1 {
		t.Errorf("ответ не обрезан: %d символов", len([]rune(short)))
	}
}

// captureNotifier запоминает отправленные сообщения.
type captureNotifier struct {
	texts []string
}

func (c *captureNotifier) SendText(_ context.Context, _ int64, text string) error {
	c.texts = append(c.texts, text)
	return nil
}

func TestNotifyUserRendersMarkdown(t *testing.T) {
	notifier := &captureNotifier{}
	assistant := &Assistant{
		notify: notifier,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	telegramID := int64(42)
	assistant.notifyUser(context.Background(), &storage.AITicket{ID: 7, TelegramID: &telegramID},
		"**Важно**\n\n- первый\n- второй\n\n`код`")

	if len(notifier.texts) != 1 {
		t.Fatalf("сообщений %d, ждали одно", len(notifier.texts))
	}
	text := notifier.texts[0]
	for _, wanted := range []string{"<b>Важно</b>", "• первый", "<code>код</code>", "обращению #7"} {
		if !strings.Contains(text, wanted) {
			t.Errorf("в сообщении нет %q: %s", wanted, text)
		}
	}
	if strings.Contains(text, "**") {
		t.Errorf("в сообщении осталась разметка: %s", text)
	}
}

// Длинный ответ приходит несколькими сообщениями, и каждое влезает в лимит
// Telegram — иначе самое интересное осталось бы неотправленным.
func TestNotifyUserSplitsLongAnswer(t *testing.T) {
	notifier := &captureNotifier{}
	assistant := &Assistant{
		notify: notifier,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	telegramID := int64(42)

	var answer strings.Builder
	for i := 0; i < 200; i++ {
		answer.WriteString("**Пункт** с пояснением\n\n")
	}
	assistant.notifyUser(context.Background(), &storage.AITicket{ID: 9, TelegramID: &telegramID}, answer.String())

	if len(notifier.texts) < 2 {
		t.Fatalf("сообщений %d, ждали несколько", len(notifier.texts))
	}
	for index, text := range notifier.texts {
		if size := utf8.RuneCountInString(text); size > 4096 {
			t.Errorf("сообщение %d длиной %d — Telegram такое не примет", index, size)
		}
	}
	if !strings.Contains(notifier.texts[0], "обращению #9") {
		t.Error("заголовок должен быть в первом сообщении")
	}
}
