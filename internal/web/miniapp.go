package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

type miniAppUser struct {
	ID    int64 `json:"id"`
	IsBot bool  `json:"is_bot"`
}

// Telegram signs the decoded query values, including signature when present.
// Only hash is excluded from the HMAC data-check-string.
// https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
func validateMiniAppData(raw, token string, now time.Time) (miniAppUser, string, error) {
	var user miniAppUser
	invalid := errors.New("недействительные данные Telegram")
	if len(raw) == 0 || len(raw) > 12<<10 || token == "" {
		return user, "", invalid
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return user, "", invalid
	}
	keys := make([]string, 0, len(values))
	for key, entries := range values {
		if key == "" || len(entries) != 1 || strings.ContainsAny(key, "\r\n=") {
			return user, "", invalid
		}
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values.Get(key))
	}
	expected, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(expected) != sha256.Size {
		return user, "", invalid
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	signature := hmac.New(sha256.New, secret.Sum(nil))
	signature.Write([]byte(strings.Join(lines, "\n")))
	if !hmac.Equal(expected, signature.Sum(nil)) {
		return user, "", invalid
	}
	stamp, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return user, "", invalid
	}
	issued := time.Unix(stamp, 0)
	if issued.After(now.Add(30*time.Second)) || issued.Before(now.Add(-time.Hour)) {
		return user, "", invalid
	}
	if json.Unmarshal([]byte(values.Get("user")), &user) != nil || user.ID <= 0 || user.IsBot {
		return miniAppUser{}, "", invalid
	}
	return user, values.Get("start_param"), nil
}

func (s *Server) miniAppPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	s.renderPage(w, r, "miniapp.html", http.StatusOK, pageMeta{Title: "Приложение"}, struct{ Next string }{localPath(r.URL.Query().Get("next"), "/account")})
}

func (s *Server) miniAppCookieMode(r *http.Request) (http.SameSite, bool) {
	_, err := r.Cookie(miniAppSessionCookie)
	if s.isSecure(r) && (err == nil || r.URL.Path == "/app") {
		return http.SameSiteNoneMode, true
	}
	return http.SameSiteLaxMode, false
}

func (s *Server) redirectPayment(w http.ResponseWriter, r *http.Request, checkout domain.Checkout) {
	if _, err := r.Cookie(miniAppSessionCookie); err == nil {
		s.renderPage(w, r, "miniapp_payment.html", http.StatusOK, pageMeta{Title: "Оплата"}, checkout)
		return
	}
	http.Redirect(w, r, checkout.URL, http.StatusSeeOther)
}

func (s *Server) miniAppAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !s.limit.allow("miniapp:"+clientIP(r), 60, time.Minute) {
		http.Error(w, `{"error":"Подождите минуту и попробуйте снова"}`, http.StatusTooManyRequests)
		return
	}
	user, start, err := validateMiniAppData(r.PostFormValue("init_data"), s.cfg.Current().TelegramToken, time.Now())
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Закройте приложение и откройте его заново из меню бота."})
		return
	}
	// Signed Telegram identity selects the existing account. Never attach it to
	// an arbitrary browser session or accept client-supplied account IDs.
	subject, err := s.db.EnsureTelegramSubject(r.Context(), user.ID)
	if err == nil {
		err = s.startSession(w, r, subject.ID)
	}
	if err != nil {
		s.log.Error("mini app: вход не выполнен", "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Не удалось войти. Попробуйте ещё раз."})
		return
	}
	next := localPath(r.PostFormValue("next"), "/account")
	if strings.ContainsAny(next, "\\\x00") {
		next = "/account"
	}
	if strings.HasPrefix(start, "gift_") {
		code := strings.TrimPrefix(start, "gift_")
		if data, e := hex.DecodeString(code); e == nil && len(data) == 16 {
			next = "/gifts?claim=" + url.QueryEscape(code)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"next": next})
}
