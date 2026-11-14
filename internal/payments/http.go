package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// apiClient — минимальная обёртка над net/http для JSON API шлюзов.
// Ошибка всегда содержит человекочитаемое сообщение провайдера: разбирать
// «HTTP 400» в поддержке невозможно.
type apiClient struct {
	base    string
	client  *http.Client
	headers map[string]string
}

func newAPIClient(base string, timeout time.Duration) *apiClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &apiClient{
		base:    strings.TrimRight(base, "/"),
		client:  &http.Client{Timeout: timeout},
		headers: map[string]string{},
	}
}

func (c *apiClient) do(ctx context.Context, method, path string, body any, headers map[string]string) (map[string]any, error) {
	endpoint := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		endpoint = c.base + "/" + strings.TrimLeft(path, "/")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("сборка запроса: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("сборка запроса: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	for key, value := range c.headers {
		request.Header.Set(key, value)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("шлюз недоступен: %w", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("шлюз ответил %d: %s", response.StatusCode, errorText(raw))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("шлюз вернул не JSON: %w", err)
	}
	return data, nil
}

func errorText(raw []byte) string {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		text := strings.TrimSpace(string(raw))
		if text == "" {
			return "пустой ответ"
		}
		return text
	}
	for _, key := range []string{"description", "message", "error", "detail"} {
		switch value := data[key].(type) {
		case string:
			if value != "" {
				return value
			}
		case map[string]any:
			if message, ok := value["message"].(string); ok && message != "" {
				return message
			}
		}
	}
	if code, ok := data["code"].(string); ok && code != "" {
		return code
	}
	return strings.TrimSpace(string(raw))
}

// jsonString достаёт строку из произвольного JSON-значения.
func jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		// Копейки терять нельзя: «%.0f» превратил бы 123.45 в «123».
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

// jsonFloat достаёт число.
func jsonFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		var parsed float64
		if _, err := fmt.Sscan(typed, &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// moneyFromMajor переводит "123.45" в копейки, не теряя точность на float.
func moneyFromMajor(value any) *int64 {
	text := jsonString(value)
	if text == "" {
		return nil
	}
	cents, err := parseMajorCents(text)
	if err != nil {
		return nil
	}
	return &cents
}

func parseMajorCents(text string) (int64, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, ",", "."))
	if text == "" {
		return 0, fmt.Errorf("пустая сумма")
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	parts := strings.SplitN(text, ".", 2)
	whole := int64(0)
	for _, digit := range parts[0] {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("сумма %q", text)
		}
		whole = whole*10 + int64(digit-'0')
	}
	cents := int64(0)
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) > 2 {
			frac = frac[:2]
		}
		for len(frac) < 2 {
			frac += "0"
		}
		for _, digit := range frac {
			if digit < '0' || digit > '9' {
				return 0, fmt.Errorf("сумма %q", text)
			}
			cents = cents*10 + int64(digit-'0')
		}
	}
	total := whole*100 + cents
	if negative {
		total = -total
	}
	return total, nil
}

func parseISOTime(value any) *time.Time {
	text := jsonString(value)
	if text == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		utc := parsed.UTC()
		return &utc
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		utc := parsed.UTC()
		return &utc
	}
	return nil
}

// majorString форматирует сумму для API.
func majorString(amount int64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s%d.%02d", sign, amount/100, amount%100)
}

// headerValue ищет заголовок без учёта регистра: прокси иногда меняет
// написание, и подпись не должна из-за этого считаться отсутствующей.
func headerValue(headers http.Header, name string) string {
	if value := headers.Get(name); value != "" {
		return value
	}
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func decodeJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	return decoder.Decode(target)
}
