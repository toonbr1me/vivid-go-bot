// Package llm — клиент к языковой модели.
//
// Говорящий API один: OpenAI-совместимый /v1/chat/completions. Его
// понимают и OpenAI, и Ollama (http://хост:11434/v1), и OpenRouter, и
// всё, что притворяется ими. Отдельный провайдер пришлось бы заводить под
// каждую новую модель — вместо этого достаточно адреса и модели.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Message — реплика диалога.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Роли диалога.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Client — клиент модели.
type Client struct {
	endpoint string
	apiKey   string
	model    string
	http     *http.Client
}

// New собирает клиента. Пустая модель или адрес — клиент выключен:
// Enabled вернёт false, и обращения пойдут живому сотруднику.
func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		endpoint: chatEndpoint(baseURL),
		apiKey:   apiKey,
		model:    model,
		http:     &http.Client{Timeout: timeout},
	}
}

// Enabled — можно ли обращаться к модели.
func (c *Client) Enabled() bool {
	return c != nil && c.endpoint != "" && c.model != ""
}

// Model — имя модели.
func (c *Client) Model() string { return c.model }

// chatEndpoint превращает адрес в адрес чата.
func chatEndpoint(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + "/chat/completions"
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
		Text    string  `json:"text"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Chat спрашивает модель и возвращает ответ.
//
// system — правила работы помощника, messages — переписка. Пустой ответ
// или ошибка не должны валить обращение: вызывающий сам решит, что делать.
func (c *Client) Chat(ctx context.Context, system string, messages []Message) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("модель не настроена")
	}
	if system != "" {
		messages = append([]Message{{Role: RoleSystem, Content: system}}, messages...)
	}
	payload := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("сборка запроса: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("сборка запроса: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("модель недоступна: %w", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("модель ответила %d: %s", response.StatusCode, errorText(raw))
	}
	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("модель вернула не JSON: %w", err)
	}
	if decoded.Error != nil && decoded.Error.Message != "" {
		return "", fmt.Errorf("модель вернула ошибку: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("модель не вернула ни одного ответа")
	}
	answer := decoded.Choices[0].Message.Content
	if answer == "" {
		answer = decoded.Choices[0].Text
	}
	return strings.TrimSpace(answer), nil
}

// errorText — короткое сообщение об ошибке от модели.
func errorText(raw []byte) string {
	var decoded struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &decoded); err == nil {
		if decoded.Error.Message != "" {
			return decoded.Error.Message
		}
		if decoded.Message != "" {
			return decoded.Message
		}
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > 300 {
		text = text[:300]
	}
	return text
}
