// Package remnawave — клиент панели управления подписками.
//
// Ответы панели на чтение не зафиксированы контрактом, поэтому поля, которые
// встречаются в двух формах (сквады списком строк и списком объектов),
// разбираются терпимо: молчаливая потеря настройки хуже явной ошибки.
package remnawave

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Error — отказ панели или сети.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errf(format string, args ...any) error { return &Error{msg: fmt.Sprintf(format, args...)} }

// Timestamp принимает ISO-8601 из панели и пустую строку.
type Timestamp struct{ time.Time }

// UnmarshalJSON разбирает время панели, не роняя весь ответ на мусоре.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(string(data), `"`)
	if raw == "" || raw == "null" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := ParseTime(raw)
	if err != nil {
		t.Time = time.Time{}
		return nil
	}
	t.Time = parsed
	return nil
}

// MarshalJSON пишет время в UTC ISO-8601.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.UTC().Format(time.RFC3339))
}

// ParseTime понимает RFC3339 и время без зоны (считаем UTC).
func ParseTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("пустое время")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02",
	} {
		if parsed, err := time.ParseInLocation(layout, raw, time.UTC); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("нечитаемое время %q", raw)
}

// User — пользователь панели в объёме, который нужен боту и сайту.
type User struct {
	ID                   int64          `json:"id"`
	UUID                 string         `json:"uuid"`
	ShortUUID            string         `json:"shortUuid"`
	Username             string         `json:"username"`
	Status               string         `json:"status"`
	Tag                  string         `json:"tag"`
	ExpireAt             Timestamp      `json:"expireAt"`
	TelegramID           *int64         `json:"telegramId"`
	Email                string         `json:"email"`
	SubscriptionURL      string         `json:"subscriptionUrl"`
	TrafficLimitBytes    *int64         `json:"trafficLimitBytes"`
	TrafficUsedBytes     int64          `json:"-"`
	HWIDDeviceLimit      *int           `json:"hwidDeviceLimit"`
	ActiveInternalSquads []string       `json:"-"`
	Raw                  map[string]any `json:"-"`
}

// UnmarshalJSON разбирает ответ и вытаскивает вложенные поля, форма которых
// менялась между версиями панели.
func (u *User) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.Raw = raw
	u.ID = int64Of(raw["id"])
	u.UUID = stringOf(raw["uuid"])
	u.ShortUUID = stringOf(raw["shortUuid"])
	u.Username = stringOf(raw["username"])
	u.Status = stringOf(raw["status"])
	u.Tag = stringOf(raw["tag"])
	u.Email = stringOf(raw["email"])
	u.SubscriptionURL = stringOf(raw["subscriptionUrl"])
	if value, ok := raw["telegramId"]; ok && value != nil {
		id := int64Of(value)
		u.TelegramID = &id
	}
	if value, ok := raw["trafficLimitBytes"]; ok && value != nil {
		limit := int64Of(value)
		u.TrafficLimitBytes = &limit
	}
	if value, ok := raw["hwidDeviceLimit"]; ok && value != nil {
		limit := int(int64Of(value))
		u.HWIDDeviceLimit = &limit
	}
	if expire, ok := raw["expireAt"].(string); ok {
		if parsed, err := ParseTime(expire); err == nil {
			u.ExpireAt = Timestamp{parsed}
		}
	}
	if traffic, ok := raw["userTraffic"].(map[string]any); ok {
		u.TrafficUsedBytes = int64Of(traffic["usedTrafficBytes"])
	}
	switch squads := raw["activeInternalSquads"].(type) {
	case []any:
		for _, item := range squads {
			switch squad := item.(type) {
			case string:
				u.ActiveInternalSquads = append(u.ActiveInternalSquads, squad)
			case map[string]any:
				if uuid := stringOf(squad["uuid"]); uuid != "" {
					u.ActiveInternalSquads = append(u.ActiveInternalSquads, uuid)
				}
			}
		}
	}
	return nil
}

// IsActive — подписка не истекла. Смотрим на срок, а не на status: LIMITED и
// DISABLED тоже встречаются с неизрасходованным остатком.
func (u User) IsActive() bool {
	return !u.ExpireAt.IsZero() && u.ExpireAt.After(time.Now().UTC())
}

// PickUser выбирает действующего пользователя из ответа поиска.
func PickUser(users []User) *User {
	for i := range users {
		if users[i].Status == "ACTIVE" {
			return &users[i]
		}
	}
	if len(users) > 0 {
		return &users[0]
	}
	return nil
}

// Client — HTTP-клиент панели.
type Client struct {
	baseURL     string
	token       string
	requiredTag string
	http        *http.Client
}

// NewTagged creates a client that only manages users carrying tag. The tag is
// attached to new profiles and checked before any user-specific operation.
func NewTagged(baseURL, token string, timeout time.Duration, tag string) *Client {
	c := New(baseURL, token, timeout)
	c.requiredTag = strings.TrimSpace(tag)
	return c
}

// New создаёт клиент. Таймаут в секундах.
func New(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}
}

// UsersByTelegramID ищет пользователей по telegramId.
func (c *Client) UsersByTelegramID(ctx context.Context, telegramID int64) ([]User, error) {
	query := url.Values{}
	query.Set("telegramId", strconv.FormatInt(telegramID, 10))
	query.Set("size", "250")
	data, err := c.do(ctx, http.MethodGet, "/api/users/stream", query, nil, nil)
	if err != nil {
		return nil, err
	}
	response, _ := data["response"].(map[string]any)
	rawUsers, _ := response["users"].([]any)
	users, err := decodeUsers(rawUsers)
	if err != nil || c.requiredTag == "" {
		return users, err
	}
	filtered := users[:0]
	for _, user := range users {
		if user.Tag == c.requiredTag {
			filtered = append(filtered, user)
		}
	}
	return filtered, nil
}

// UserByUsername ищет пользователя по имени; отсутствие — не ошибка.
func (c *Client) UserByUsername(ctx context.Context, username string) (*User, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/users/by-username/"+url.PathEscape(username), nil, nil, map[int]bool{404: true})
	if err != nil || data == nil {
		return nil, err
	}
	response, _ := data["response"].(map[string]any)
	if response == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	var user User
	if err := json.Unmarshal(encoded, &user); err != nil {
		return nil, err
	}
	if c.requiredTag != "" && user.Tag != c.requiredTag {
		return nil, nil
	}
	return &user, nil
}

// AllUsers выгружает всех пользователей постранично.
func (c *Client) AllUsers(ctx context.Context, pageSize int) ([]User, error) {
	if pageSize <= 0 {
		pageSize = 500
	}
	var out []User
	start := 0
	for {
		query := url.Values{}
		query.Set("size", strconv.Itoa(pageSize))
		query.Set("start", strconv.Itoa(start))
		data, err := c.do(ctx, http.MethodGet, "/api/users", query, nil, nil)
		if err != nil {
			return nil, err
		}
		response, _ := data["response"].(map[string]any)
		rawUsers, _ := response["users"].([]any)
		users, err := decodeUsers(rawUsers)
		if err != nil {
			return nil, err
		}
		if len(users) == 0 {
			break
		}
		out = append(out, users...)
		total := int(int64Of(response["total"]))
		start += len(users)
		if len(users) < pageSize || start >= total {
			break
		}
	}
	return out, nil
}

// ExternalSquads возвращает внешние сквады.
func (c *Client) ExternalSquads(ctx context.Context) ([]map[string]any, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/external-squads", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	response, _ := data["response"].(map[string]any)
	items, _ := response["externalSquads"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if squad, ok := item.(map[string]any); ok {
			out = append(out, squad)
		}
	}
	return out, nil
}

// ResolveExternalSquadUUID ищет сквад по имени без учёта регистра и пробелов.
func (c *Client) ResolveExternalSquadUUID(ctx context.Context, name string) (string, error) {
	target := normalizeSquadName(name)
	if target == "" {
		return "", nil
	}
	squads, err := c.ExternalSquads(ctx)
	if err != nil {
		return "", err
	}
	for _, squad := range squads {
		if normalizeSquadName(stringOf(squad["name"])) == target {
			return stringOf(squad["uuid"]), nil
		}
	}
	return "", nil
}

// CreateUserParams — параметры создания пользователя панели.
type CreateUserParams struct {
	Username          string
	Tag               string
	TelegramID        *int64
	ExpireAt          time.Time
	TrafficLimitBytes *int64
	ExternalSquadUUID string
	InternalSquads    []string
	HWIDDeviceLimit   *int
}

// CreateUser заводит пользователя. Пользователь может быть без Telegram:
// сайт продаёт подписку и до привязки к мессенджеру.
func (c *Client) CreateUser(ctx context.Context, params CreateUserParams) (User, error) {
	if c.requiredTag != "" {
		params.Tag = c.requiredTag
	}
	payload := map[string]any{
		"username": params.Username,
		"expireAt": params.ExpireAt.UTC().Format(time.RFC3339),
	}
	if params.Tag != "" {
		payload["tag"] = params.Tag
	}
	if params.TelegramID != nil {
		payload["telegramId"] = *params.TelegramID
	}
	if params.TrafficLimitBytes != nil {
		payload["trafficLimitBytes"] = *params.TrafficLimitBytes
		payload["trafficLimitStrategy"] = "NO_RESET"
	}
	if params.ExternalSquadUUID != "" {
		payload["externalSquadUuid"] = params.ExternalSquadUUID
	}
	if len(params.InternalSquads) > 0 {
		payload["activeInternalSquads"] = params.InternalSquads
	}
	if params.HWIDDeviceLimit != nil {
		payload["hwidDeviceLimit"] = *params.HWIDDeviceLimit
	}
	data, err := c.do(ctx, http.MethodPost, "/api/users", nil, payload, nil)
	if err != nil {
		return User{}, err
	}
	return decodeUser(data["response"])
}

// UpdateUserParams — частичное обновление. nil-поля не отправляются:
// незаданный лимит не должен стирать уже назначенный.
type UpdateUserParams struct {
	UserID            int64
	ExpireAt          *time.Time
	TelegramID        *int64
	Status            *string
	TrafficLimitBytes **int64
	ExternalSquadUUID *string
	InternalSquads    *[]string
	HWIDDeviceLimit   **int
}

// UpdateUser отправляет только заданные поля.
func (c *Client) UpdateUser(ctx context.Context, params UpdateUserParams) (User, error) {
	if err := c.requireTag(ctx, params.UserID); err != nil {
		return User{}, err
	}
	payload := map[string]any{"id": params.UserID}
	if params.ExpireAt != nil {
		payload["expireAt"] = params.ExpireAt.UTC().Format(time.RFC3339)
	}
	if params.TelegramID != nil {
		payload["telegramId"] = *params.TelegramID
	}
	if params.Status != nil {
		payload["status"] = *params.Status
	}
	if params.TrafficLimitBytes != nil {
		if *params.TrafficLimitBytes != nil {
			payload["trafficLimitBytes"] = **params.TrafficLimitBytes
			payload["trafficLimitStrategy"] = "NO_RESET"
		}
	}
	if params.ExternalSquadUUID != nil {
		payload["externalSquadUuid"] = *params.ExternalSquadUUID
	}
	if params.InternalSquads != nil {
		payload["activeInternalSquads"] = *params.InternalSquads
	}
	if params.HWIDDeviceLimit != nil && *params.HWIDDeviceLimit != nil {
		payload["hwidDeviceLimit"] = **params.HWIDDeviceLimit
	}
	data, err := c.do(ctx, http.MethodPatch, "/api/users", nil, payload, nil)
	if err != nil {
		return User{}, err
	}
	return decodeUser(data["response"])
}

// requireTag prevents a service bot from reading or changing another service's
// profile by ID. With no configured tag, legacy single-panel behavior applies.
func (c *Client) requireTag(ctx context.Context, userID int64) error {
	if c.requiredTag == "" {
		return nil
	}
	if userID <= 0 {
		return errf("invalid panel user id")
	}
	data, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/users/%d", userID), nil, nil, nil)
	if err != nil {
		return err
	}
	user, err := decodeUser(data["response"])
	if err != nil {
		return err
	}
	if user.Tag != c.requiredTag {
		return errf("panel user is outside configured tag")
	}
	return nil
}

// UpdateExpireAt продлевает подписку.
func (c *Client) UpdateExpireAt(ctx context.Context, userID int64, expireAt time.Time, squads []string) (User, error) {
	if err := c.requireTag(ctx, userID); err != nil {
		return User{}, err
	}
	payload := map[string]any{
		"id":       userID,
		"expireAt": expireAt.UTC().Format(time.RFC3339),
	}
	if len(squads) > 0 {
		payload["activeInternalSquads"] = squads
	}
	data, err := c.do(ctx, http.MethodPatch, "/api/users", nil, payload, nil)
	if err != nil {
		return User{}, err
	}
	return decodeUser(data["response"])
}

// ResetTraffic обнуляет израсходованное. 404 не прощается: у пользователя без
// трафика эндпоинт отвечает 200, значит 404 — неверный путь или пользователь.
func (c *Client) ResetTraffic(ctx context.Context, userID int64) error {
	if err := c.requireTag(ctx, userID); err != nil {
		return err
	}
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/users/%d/actions/reset-traffic", userID), nil, nil, nil)
	return err
}

// HWIDDevice — одно привязанное устройство.
type HWIDDevice struct {
	HWID        string    `json:"hwid"`
	Platform    string    `json:"platform"`
	OSVersion   string    `json:"osVersion"`
	DeviceModel string    `json:"deviceModel"`
	UserAgent   string    `json:"userAgent"`
	CreatedAt   Timestamp `json:"createdAt"`
}

// HWIDDevices возвращает устройства пользователя.
func (c *Client) HWIDDevices(ctx context.Context, userID int64) ([]HWIDDevice, error) {
	if err := c.requireTag(ctx, userID); err != nil {
		return nil, err
	}
	data, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/hwid/devices/%d", userID), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	response, _ := data["response"].(map[string]any)
	items, _ := response["devices"].([]any)
	out := make([]HWIDDevice, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var device HWIDDevice
		if err := json.Unmarshal(encoded, &device); err != nil {
			continue
		}
		out = append(out, device)
	}
	return out, nil
}

// DeleteHWIDDevice отвязывает устройство. false — его уже не было.
func (c *Client) DeleteHWIDDevice(ctx context.Context, userID int64, hwid string) (bool, error) {
	if err := c.requireTag(ctx, userID); err != nil {
		return false, err
	}
	data, err := c.do(ctx, http.MethodPost, "/api/hwid/devices/delete",
		nil, map[string]any{"userId": userID, "hwid": hwid}, map[int]bool{404: true})
	if err != nil {
		return false, err
	}
	return data != nil, nil
}

// DeleteUser удаляет пользователя панели. false — его уже нет.
func (c *Client) DeleteUser(ctx context.Context, userID int64) (bool, error) {
	if err := c.requireTag(ctx, userID); err != nil {
		return false, err
	}
	data, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/users/%d", userID), nil, nil, map[int]bool{404: true})
	if err != nil {
		return false, err
	}
	return data != nil, nil
}

// SubscriptionURL достаёт ссылку подписки, собирая её из shortUuid при
// необходимости.
func SubscriptionURL(baseURL string, user User) string {
	if user.SubscriptionURL != "" {
		return user.SubscriptionURL
	}
	if user.ShortUUID != "" {
		return strings.TrimRight(baseURL, "/") + "/api/sub/" + user.ShortUUID
	}
	return ""
}

func (c *Client) do(
	ctx context.Context,
	method, path string,
	query url.Values,
	body any,
	allow map[int]bool,
) (map[string]any, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errf("сериализация запроса: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, errf("сборка запроса: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, errf("панель недоступна: %v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if allow != nil && allow[response.StatusCode] {
		return nil, nil
	}
	if response.StatusCode >= 400 {
		return nil, errf("панель ответила %d: %s", response.StatusCode, extractError(raw))
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, errf("панель вернула не JSON: %v", err)
	}
	return data, nil
}

func decodeUsers(items []any) ([]User, error) {
	out := make([]User, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		var user User
		if err := json.Unmarshal(encoded, &user); err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, nil
}

func decodeUser(value any) (User, error) {
	if value == nil {
		return User{}, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return User{}, err
	}
	var user User
	if err := json.Unmarshal(encoded, &user); err != nil {
		return User{}, err
	}
	return user, nil
}

func extractError(raw []byte) string {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return strings.TrimSpace(string(raw))
	}
	message := stringOf(data["message"])
	if message == "" {
		message = stringOf(data["error"])
	}
	if message == "" {
		message = "неизвестная ошибка"
	}
	if code := stringOf(data["errorCode"]); code != "" {
		return message + " [" + code + "]"
	}
	return message
}

func normalizeSquadName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func stringOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func int64Of(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}
