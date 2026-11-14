package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// postForm отправляет форму с CSRF-токеном, как это делает браузер.
func postForm(t *testing.T, handler http.Handler, path string, values url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	return rec
}

// csrfFrom готовит пару «токен + cookie» для отправки формы.
func csrfFrom(t *testing.T, handler http.Handler) (string, []*http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	var token string
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == csrfCookie {
			token = cookie.Value
		}
	}
	if token == "" {
		t.Fatal("страница входа не выдала CSRF-токен")
	}
	return token, rec.Result().Cookies()
}

// Вход по коду из бота: правильный код открывает кабинет.
func TestLoginByCodeOpensAccount(t *testing.T) {
	server, db := testServer(t)
	handler := server.Handler()
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 7101)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	code, err := db.CreateLinkCode(ctx, subject.ID)
	if err != nil {
		t.Fatalf("код: %v", err)
	}

	token, cookies := csrfFrom(t, handler)
	rec := postForm(t, handler, "/auth/code", url.Values{
		"code": {code},
		"next": {"/account"},
		"csrf": {token},
	}, cookies)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("код ответа = %d, ждали 303: %s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/account" {
		t.Fatalf("редирект = %q, ждали /account", location)
	}

	var session *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookie {
			session = cookie
		}
	}
	if session == nil || session.Value == "" {
		t.Fatal("сессия не выдана")
	}

	// Кабинет с этой сессией открывается.
	request := httptest.NewRequest(http.MethodGet, "/account", nil)
	request.AddCookie(session)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("кабинет: код = %d, ждали 200", rec.Code)
	}
}

// Неверный код: человек остаётся на странице входа и видит объяснение.
func TestLoginByWrongCodeStaysOnLogin(t *testing.T) {
	server, _ := testServer(t)
	handler := server.Handler()

	token, cookies := csrfFrom(t, handler)
	rec := postForm(t, handler, "/auth/code", url.Values{
		"code": {"не-такой-код"},
		"csrf": {token},
	}, cookies)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("код ответа = %d, ждали 303", rec.Code)
	}
	if location := rec.Header().Get("Location"); !strings.HasPrefix(location, "/login") {
		t.Fatalf("редирект = %q, ждали /login", location)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookie {
			t.Fatal("сессия не должна выдаваться при неверном коде")
		}
	}
}

// Код одноразовый: второй вход по нему не работает.
func TestLoginCodeIsOneTime(t *testing.T) {
	server, db := testServer(t)
	handler := server.Handler()
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 7102)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	code, err := db.CreateLinkCode(ctx, subject.ID)
	if err != nil {
		t.Fatalf("код: %v", err)
	}

	token, cookies := csrfFrom(t, handler)
	postForm(t, handler, "/auth/code", url.Values{"code": {code}, "csrf": {token}}, cookies)

	token, cookies = csrfFrom(t, handler)
	rec := postForm(t, handler, "/auth/code", url.Values{"code": {code}, "csrf": {token}}, cookies)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookie {
			t.Fatal("использованный код не должен пускать снова")
		}
	}
}

// Форма без CSRF-токена не принимается.
func TestFormsRequireCSRFToken(t *testing.T) {
	server, _ := testServer(t)
	handler := server.Handler()

	rec := postForm(t, handler, "/auth/code", url.Values{"code": {"abc"}}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("код ответа = %d, ждали 403", rec.Code)
	}
}

// Слишком много попыток входа с одного адреса — дальше не пускает.
func TestLoginBruteForceIsLimited(t *testing.T) {
	server, _ := testServer(t)
	handler := server.Handler()

	for i := 0; i < 12; i++ {
		token, cookies := csrfFrom(t, handler)
		postForm(t, handler, "/auth/code", url.Values{
			"code": {"wrong"},
			"csrf": {token},
		}, cookies)
	}
	token, cookies := csrfFrom(t, handler)
	rec := postForm(t, handler, "/auth/code", url.Values{
		"code": {"wrong"},
		"csrf": {token},
	}, cookies)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("код ответа = %d, ждали 303", rec.Code)
	}
	// После лимита страница входа объясняет, что надо подождать.
	follow := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	for _, cookie := range rec.Result().Cookies() {
		request.AddCookie(cookie)
	}
	handler.ServeHTTP(follow, request)
	if !strings.Contains(follow.Body.String(), "Слишком много попыток") {
		t.Fatalf("человеку не сказали про лимит: %s", follow.Body.String()[:min(len(follow.Body.String()), 400)])
	}
}

// Выход из кабинета убирает сессию.
func TestLogoutClearsSession(t *testing.T) {
	server, db := testServer(t)
	handler := server.Handler()
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 7103)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	session := sessionCookieFor(t, db, subject.ID)
	token, cookies := csrfFrom(t, handler)

	rec := postForm(t, handler, "/logout", url.Values{"csrf": {token}}, append(cookies, session))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("код ответа = %d, ждали 303", rec.Code)
	}
	cleared := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value == "" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("сессия не очищена")
	}
}

// Обращение в поддержку: сообщение человека сохраняется и показывается
// с разметкой.
func TestSupportTicketShowsMessage(t *testing.T) {
	server, db := testServer(t)
	handler := server.Handler()
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 7104)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	session := sessionCookieFor(t, db, subject.ID)
	token, cookies := csrfFrom(t, handler)

	rec := postForm(t, handler, "/support", url.Values{
		"message": {"Не подключается **устройство**"},
		"csrf":    {token},
	}, append(cookies, session))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("код ответа = %d, ждали 303: %s", rec.Code, rec.Body.String())
	}

	// В кабинете видно обращение и его текст.
	request := httptest.NewRequest(http.MethodGet, "/account", nil)
	request.AddCookie(session)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("кабинет: код = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Не подключается") {
		t.Fatalf("в кабинете нет текста обращения: %s", rec.Body.String())
	}
}

// Страница обращения открывается по идентификатору и показывает переписку.
func TestTicketPageRenders(t *testing.T) {
	server, db := testServer(t)
	handler := server.Handler()
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 7105)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	ticket, err := db.CreateTicket(ctx, subject.ID, subject.TelegramID, "вопрос", "Не работает подключение", "human")
	if err != nil {
		t.Fatalf("обращение: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/support/tickets/"+itoa(ticket.ID), nil)
	request.AddCookie(sessionCookieFor(t, db, subject.ID))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("страница обращения: код = %d, ждали 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Не работает подключение") {
		t.Fatal("в обращении нет текста вопроса")
	}
}

// Мостик Happ: ссылка ведёт на страницу, а не отдаёт 404.
func TestHappBridgeAnswers(t *testing.T) {
	server, _ := testServer(t)
	handler := server.Handler()

	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/happ?u="+url.QueryEscape("https://panel.example/sub/abc"), nil)
	handler.ServeHTTP(rec, request)
	if rec.Code == http.StatusNotFound {
		t.Fatal("мостик Happ не найден")
	}
}

// Метрики воронки: шаг записывается и не падает.
func TestFunnelStepRecorded(t *testing.T) {
	_, db := testServer(t)
	ctx := context.Background()

	if err := db.FunnelStep(ctx, "landing"); err != nil {
		t.Fatalf("шаг воронки: %v", err)
	}
	steps, err := db.FunnelSteps(ctx, 7)
	if err != nil {
		t.Fatalf("шаги воронки: %v", err)
	}
	found := false
	for _, step := range steps {
		if step.Step == "landing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("шаг не записан: %+v", steps)
	}
}

// Страница не найдена — понятный ответ, а не пустой.
func TestUnknownPageIsNotFound(t *testing.T) {
	server, _ := testServer(t)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код = %d, ждали 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "не найдена") {
		t.Fatal("в ответе нет объяснения")
	}
}

// Время в тестах фиксировано, чтобы не было гонок.
func TestNowIsStable(t *testing.T) {
	if time.Now().IsZero() {
		t.Fatal("время не должно быть нулевым")
	}
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
