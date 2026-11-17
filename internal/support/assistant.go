// Package support — помощник, отвечающий на типовые вопросы по обращениям.
//
// Помощник работает по правилам, а не по вдохновению: он отвечает только
// на вопросы о сервисе, не повторяет личные данные и передаёт человека
// сотруднику, если вопрос не про сервис, есть срочность или в сообщении
// видны чувствительные данные.
package support

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/llm"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// assistantChunk — сколько текста отправлять одним сообщением. Меньше
// предела Telegram, потому что первая часть несёт ещё и заголовок.
const assistantChunk = 3000

// FactsProvider отдаёт помощнику сведения о сервисе и о клиенте.
//
// Отдельно от клиента: одно и то же описание тарифов можно говорить всем,
// а состояние подписки — только его владельцу.
type FactsProvider interface {
	// ServiceFacts — что можно рассказать всем: тарифы, пробный период, оплата.
	ServiceFacts(ctx context.Context) (string, error)
	// UserFacts — что можно рассказать конкретному клиенту.
	UserFacts(ctx context.Context, subjectID int64) (string, error)
}

// Notifier отправляет ответ человеку в мессенджер.
type Notifier interface {
	SendText(ctx context.Context, telegramID int64, text string) error
}

// richNotifier — необязательное умение: Markdown богатым сообщением.
//
// Отдельный интерфейс, чтобы не ломать тестовые и сторонние реализации
// Notifier: если умения нет, ответ уходит прежним HTML.
type richNotifier interface {
	SendRichMarkdown(ctx context.Context, telegramID int64, markdown string) error
}

// Assistant — помощник поддержки.
type Assistant struct {
	db        *storage.DB
	llm       *llm.Client
	cfg       *config.Settings
	facts     FactsProvider
	knowledge *Knowledge
	notify    Notifier
	log       *slog.Logger
}

// New собирает помощника.
func New(db *storage.DB, client *llm.Client, cfg *config.Settings, facts FactsProvider, knowledge *Knowledge, notify Notifier, log *slog.Logger) *Assistant {
	if log == nil {
		log = slog.Default()
	}
	return &Assistant{db: db, llm: client, cfg: cfg, facts: facts, knowledge: knowledge, notify: notify, log: log}
}

// Enabled — помощник настроен и включён.
func (a *Assistant) Enabled() bool {
	return a != nil && a.llm != nil && a.llm.Enabled() && a.cfg != nil && a.cfg.Current().LLMEnabled
}

// HandoffText — что видит человек, когда вопрос ушёл сотруднику.
const HandoffText = "Передаю вопрос сотруднику — он ответит здесь и в Telegram."

// escalationRe — случаи, когда нужен живой человек.
//
// Границ слов здесь нет: в Go \b работает только с ASCII, а текст
// русский, и «срочно» с \b просто не находилось.
var escalationRe = regexp.MustCompile(`(?i)(человек|оператор|менеджер|поддержк\w*|жив(ой|ого)|срочн\w*|юрист|обжал\w*|жалоб\w*|вернуть деньги|возврат|диспут\w*|адвокат)`)

// sensitiveRe — данные, которые нельзя пересылать модели и повторять.
var sensitiveRe = regexp.MustCompile(`(?i)(парол\w*|passwo?rd|пин[- ]?код|cvv|cvc|карт\w*|паспорт\w*|секрет\w*|токен\w*|api[- _]?key|код\s+подтвержд\w*|одноразов\w*)`)
var personalDataRe = regexp.MustCompile(`(?i)([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}|(?:[0-9][ -]?){13,19}|\+7[ ()0-9-]{10,})`)

// topicRe — темы, по которым помощник отвечает.
var topicRe = regexp.MustCompile(`(?i)(сколько|цена|стоимост|тариф|подписк|пробн|оплат|платеж|подключ|устройств|лимит|баланс|заказ|промокод|бонус|дн[яеи]|почему|как|не работает|не пришл|перестал|ист[её]к)`)

// decision — что делать с сообщением человека.
type decision int

const (
	// decisionAnswer — помощник отвечает сам.
	decisionAnswer decision = iota
	// decisionToHuman — вопрос живой сотруднику.
	decisionToHuman
)

// classify решает, ответит ли помощник сам.
//
// Помощник отвечает только на вопросы о сервисе. Просьба живого человека и
// любые персональные данные — сразу к сотруднику: переписку с человеком
// модель видеть не должна.
func classify(text string) decision {
	if escalationRe.MatchString(text) {
		return decisionToHuman
	}
	if sensitiveRe.MatchString(text) || personalDataRe.MatchString(text) {
		return decisionToHuman
	}
	if !topicRe.MatchString(text) {
		return decisionToHuman
	}
	return decisionAnswer
}

// Handle берёт одно обращение, на которое ещё не ответили, и отвечает.
//
// Возвращает число обработанных обращений. Помощник отвечает не на все
// обращения: доля задаётся настройкой SUPPORT_LLM_SHARE, остальные сразу
// уходят живому сотруднику.
func (a *Assistant) Handle(ctx context.Context) (int, error) {
	if !a.Enabled() {
		return 0, nil
	}
	claim := fmt.Sprintf("%d", time.Now().UnixNano())
	ticket, err := a.db.ClaimAITicket(ctx, claim, 2*time.Minute)
	if err != nil {
		return 0, err
	}
	if ticket == nil {
		return 0, nil
	}
	if !a.shareAllows() {
		// Это обращение ведёт сотрудник: так быстрее и без риска.
		if _, err := a.db.HandoffAITicket(ctx, ticket.ID, ""); err != nil {
			return 0, err
		}
		return 1, nil
	}
	messages, err := a.db.TicketMessages(ctx, ticket.ID, 50)
	if err != nil {
		return 0, err
	}
	fresh := a.freshMessages(messages, ticket.AICursor)
	if len(fresh) == 0 {
		return 0, nil
	}
	question := fresh[len(fresh)-1]
	if question.FromAdmin {
		return 0, nil
	}
	if ticket.AIReplies >= a.cfg.Current().LLMMaxReplies {
		// Ответили столько, сколько разрешено: дальше — человек.
		if _, err := a.db.HandoffAITicket(ctx, ticket.ID, ""); err != nil {
			return 0, err
		}
		return 1, nil
	}
	if classify(question.Text) == decisionToHuman {
		return a.handoff(ctx, ticket, question.ID)
	}
	answer, err := a.answer(ctx, ticket, messages)
	if err != nil {
		a.log.Warn("помощник не ответил", "ticket", ticket.ID, "err", err)
		if _, handoffErr := a.db.HandoffAITicket(ctx, ticket.ID, ""); handoffErr != nil {
			return 0, handoffErr
		}
		return 1, nil
	}
	if answer == "" || strings.Contains(strings.ToLower(answer), "передаю вопрос сотруднику") {
		return a.handoff(ctx, ticket, question.ID)
	}
	published, err := a.db.FinishAITicket(ctx, ticket.ID, claim, question.ID, answer, false)
	if err != nil {
		return 0, err
	}
	if !published {
		return 0, nil
	}
	a.notifyUser(ctx, ticket, answer)
	return 1, nil
}

// notifyUser пишет ответ в мессенджер: на сайте человек может быть не каждый
// день, а вопрос ждать не должен.
//
// Ответ модели приходит в Markdown: звёздочки, списки, таблицы. Telegram
// разметку не понимает, поэтому переводим её в HTML, а длинный ответ делим
// на несколько сообщений — иначе Telegram откажется его принять целиком.
func (a *Assistant) notifyUser(ctx context.Context, ticket *storage.AITicket, answer string) {
	if a.notify == nil || ticket.TelegramID == nil {
		return
	}
	if notifier, ok := a.notify.(interface {
		SendTicketUpdate(context.Context, int64, int64, string, string) error
	}); ok {
		kind := "assistant"
		if answer == HandoffText {
			kind = "handoff"
		}
		if err := notifier.SendTicketUpdate(ctx, *ticket.TelegramID, ticket.ID, answer, kind); err != nil {
			a.log.Warn("помощник: сообщение не доставлено", "ticket", ticket.ID, "err", err)
		}
		return
	}
	header := "### 💬 Ответ помощника по обращению #" + fmt.Sprint(ticket.ID)
	if rich, ok := a.notify.(richNotifier); ok {
		if err := rich.SendRichMarkdown(ctx, *ticket.TelegramID, header+"\n\n"+answer); err != nil {
			a.log.Warn("помощник: сообщение не доставлено", "telegram_id", *ticket.TelegramID, "err", err)
		}
		return
	}
	parts := mdhtml.Chunks(answer, assistantChunk)
	if len(parts) == 0 {
		return
	}
	plainHeader := "💬 <b>Ответ помощника по обращению #" + fmt.Sprint(ticket.ID) + "</b>"
	for index, part := range parts {
		text := part
		if index == 0 {
			text = plainHeader + "\n\n" + part
		}
		if err := a.notify.SendText(ctx, *ticket.TelegramID, text); err != nil {
			a.log.Warn("помощник: сообщение не доставлено", "telegram_id", *ticket.TelegramID, "err", err)
			return
		}
	}
}

// handoff — передача обращения сотруднику.
//
// Человеку честно говорим, что его вопрос ушёл живому сотруднику: молчание
// помощника выглядело бы так, будто его не заметили.
func (a *Assistant) handoff(ctx context.Context, ticket *storage.AITicket, cursor int64) (int, error) {
	changed, err := a.db.HandoffAITicket(ctx, ticket.ID, HandoffText)
	if err != nil {
		return 0, err
	}
	if changed {
		a.notifyUser(ctx, ticket, HandoffText)
	}
	return 1, nil
}

// CloseIdle hands stale assistant conversations to staff without closing them.
func (a *Assistant) CloseIdle(ctx context.Context) (int, error) {
	const text = "Обращение остаётся открытым. Передаю его сотруднику, чтобы помочь с решением."
	tickets, err := a.db.HandoffIdleAITickets(ctx, 48, text)
	if err != nil {
		return 0, err
	}
	for _, ticket := range tickets {
		a.notifyUser(ctx, &ticket, text)
	}
	return len(tickets), nil
}

// shareAllows — доля обращений, на которые помощник отвечает сам.
func (a *Assistant) shareAllows() bool {
	share := a.cfg.Current().LLMShare
	if share >= 100 {
		return true
	}
	if share <= 0 {
		return false
	}
	return rand.Intn(100) < share
}

// freshMessages — сообщения, на которые ещё не отвечали.
func (a *Assistant) freshMessages(messages []storage.Message, cursor int64) []storage.Message {
	out := make([]storage.Message, 0, len(messages))
	for _, message := range messages {
		if message.ID <= cursor {
			continue
		}
		if message.Internal || message.FromAdmin {
			continue
		}
		out = append(out, message)
	}
	return out
}

// answer спрашивает модель и чистит ответ.
func (a *Assistant) answer(ctx context.Context, ticket *storage.AITicket, messages []storage.Message) (string, error) {
	// Проверяем весь контекст до любого обращения к модели. Несколько
	// сообщений подряд не должны обходить фильтр последнего вопроса.
	for _, message := range messages {
		if !message.Internal && (sensitiveRe.MatchString(message.Text) || personalDataRe.MatchString(message.Text) ||
			(message.FromAdmin && !message.Assistant) ||
			(!message.FromAdmin && escalationRe.MatchString(message.Text))) {
			return "", nil
		}
	}
	system := a.systemPrompt()
	service, err := a.facts.ServiceFacts(ctx)
	if err != nil {
		return "", err
	}
	var personal string
	if ticket.SubjectID != nil {
		personal, _ = a.facts.UserFacts(ctx, *ticket.SubjectID)
	}
	history := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		if message.Internal {
			continue
		}
		role := llm.RoleUser
		switch {
		case message.FromAdmin && message.Assistant:
			role = llm.RoleAssistant
		case message.FromAdmin:
			role = llm.RoleAssistant
		}
		history = append(history, llm.Message{Role: role, Content: message.Text})
	}
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	prompt := fmt.Sprintf("%s\n\nДанные сервиса:\n%s", system, service)
	if personal != "" {
		prompt += "\n\nДанные клиента:\n" + personal
	}
	// База знаний: то, что сервис рассказывает о себе самому.
	prompt = a.knowledge.Append(prompt)
	answer, err := a.llm.Chat(ctx, prompt, history)
	if err != nil {
		return "", err
	}
	return cleanAnswer(answer), nil
}

// systemPrompt — правила работы помощника.
func (a *Assistant) systemPrompt() string {
	brand := a.cfg.Current().BrandName
	if brand == "" {
		brand = "сервиса"
	}
	return "Ты — помощник " + brand + ", сервиса подписок на приложения и сайты.\n" +
		"Отвечай по-русски, коротко и по делу, только по данным из блока «Данные сервиса».\n" +
		"Не выдумывай цены, сроки, промокоды и правила: если нужного нет — так и напиши.\n" +
		"Не повторяй и не выдумывай персональные данные клиента.\n" +
		"Если вопрос не про сервис или данных недостаточно — ответь одной фразой: «Передаю вопрос сотруднику»."
}

// answerLimit — сколько символов ответа хранить. Длинный ответ делят на
// части при отправке, а не режут посреди списка здесь.
const answerLimit = 4000

// cleanAnswer — приводит ответ к виду, годному для чата.
//
// Разметку не срезаем: модель пишет списки и выделения, а Telegram и сайт
// показывают их после перевода в HTML. Убираем только мусор: лишние пустые
// строки, хвостовые пробелы и чрезмерную длину.
func cleanAnswer(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, line)
	}
	text = strings.TrimSpace(strings.Join(out, "\n"))

	if runes := []rune(text); len(runes) > answerLimit {
		text = strings.TrimSpace(string(runes[:answerLimit])) + "…"
	}
	return text
}
