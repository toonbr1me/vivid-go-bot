// Package rich отправляет «богатые» сообщения Telegram: заголовки, таблицы,
// списки и кнопки внутри текста.
//
// Это не замена telego, а дополнение к нему. Rich Messages появились в Bot
// API позже версии, которую понимает telego, и вызывать их приходится
// напрямую. Клиент намеренно узкий: два метода и разбор ответа. Всё
// остальное — обычные сообщения — идёт через telego.
package rich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultBaseURL — облачный Bot API. Локальный сервер можно передать явно.
const DefaultBaseURL = "https://api.telegram.org"

// Message — содержимое rich-сообщения.
//
// Поля html и markdown взаимоисключающие: Telegram принимает ровно одно.
// Используем html, потому что его собирает наш рендерер, который
// экранирует всё незнакомое. Markdown от модели пропускать напрямую нельзя:
// он допускает произвольный HTML, включая кнопки с callback-действиями.
type Message struct {
	HTML string
}

// Client отправляет rich-сообщения от имени бота.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
	// unsupported выключает rich после ответа «метод не найден»: нет смысла
	// повторять заведомо неудачный запрос на каждое сообщение.
	unsupported atomic.Bool
}

// New создаёт клиент. Пустой baseURL — облачный Bot API.
func New(token, baseURL string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		token:   token,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 25 * time.Second},
	}
}

// Available сообщает, стоит ли пробовать rich-сообщения.
func (c *Client) Available() bool {
	return c != nil && c.token != "" && !c.unsupported.Load()
}

// APIError — отказ Bot API с кодом и описанием.
type APIError struct {
	Code        int
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%d %q", e.Code, e.Description)
}

// IsUnsupported сообщает, что сервер не знает метод rich-сообщений.
//
// Так выглядит старый Bot API или локальный сервер без обновления. В этом
// случае клиент выключается, а бот продолжает отправлять обычный HTML.
func IsUnsupported(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	description := strings.ToLower(apiErr.Description)
	for _, fragment := range []string{
		"unknown method",
		"method not found",
		"not implemented",
		"unsupported method",
	} {
		if strings.Contains(description, fragment) {
			return true
		}
	}
	return false
}

// IsNotModified — правка тем же содержимым. Это не ошибка.
func IsNotModified(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "message is not modified")
}

// Send отправляет новое rich-сообщение и возвращает его идентификатор.
func (c *Client) Send(ctx context.Context, chatID int64, message Message, markup any) (int64, error) {
	payload := map[string]any{
		"chat_id":      chatID,
		"rich_message": messagePayload(message),
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := c.call(ctx, "sendRichMessage", payload, &result); err != nil {
		return 0, err
	}
	return result.MessageID, nil
}

// Edit заменяет содержимое сообщения на rich.
func (c *Client) Edit(ctx context.Context, chatID int64, messageID int, message Message, markup any) error {
	payload := map[string]any{
		"chat_id":      chatID,
		"message_id":   messageID,
		"rich_message": messagePayload(message),
	}
	if markup != nil {
		payload["reply_markup"] = markup
	} else {
		// При переходе с обычного экрана удаляем старую inline-клавиатуру.
		payload["reply_markup"] = map[string]any{"inline_keyboard": [][]any{}}
	}
	var result json.RawMessage
	err := c.call(ctx, "editMessageText", payload, &result)
	if IsNotModified(err) {
		return nil
	}
	return err
}

func messagePayload(message Message) map[string]any {
	payload := map[string]any{"html": message.HTML}
	return payload
}

// call выполняет запрос и разбирает ответ Bot API.
func (c *Client) call(ctx context.Context, method string, payload map[string]any, target any) error {
	if !c.Available() {
		return errors.New("rich messages недоступны")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("rich: тело запроса: %w", err)
	}
	url := c.baseURL + "/bot" + c.token + "/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("rich: запрос: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("rich: %s: %w", method, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("rich: ответ %s: %w", method, err)
	}

	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("rich: разбор ответа %s: %w", method, err)
	}
	if !envelope.OK {
		apiErr := &APIError{Code: envelope.ErrorCode, Description: envelope.Description}
		if IsUnsupported(apiErr) {
			c.unsupported.Store(true)
		}
		return apiErr
	}
	if target == nil || len(envelope.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return fmt.Errorf("rich: результат %s: %w", method, err)
	}
	return nil
}
