package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

const sessionTTL = 30 * 24 * time.Hour

// randomToken — криптослучайная строка в hex. Токен сессии и код привязки
// угадывать нельзя, поэтому rand.Read, а не время.
func randomToken(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand в Go не отказывает без причины; если это случилось,
		// продолжать безопаснее всего отказом, но интерфейс уже не вернуть.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Сессия
// ---------------------------------------------------------------------------

func (s *Server) sessionSubject(r *http.Request) (*domain.Subject, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, nil
	}
	subject, err := s.db.SubjectBySession(r.Context(), hashToken(cookie.Value))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &subject, nil
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, subjectID int64) error {
	token := randomToken(32)
	if err := s.db.CreateWebSession(r.Context(), hashToken(token), subjectID, sessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
	return nil
}

func (s *Server) clearSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		_ = s.db.DeleteWebSession(r.Context(), hashToken(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.isSecure(r), SameSite: http.SameSiteLaxMode,
	})
}

// requireSubject пускает дальше только вошедшего человека. next — адрес, на
// который надо вернуться после входа.
func (s *Server) requireSubject(w http.ResponseWriter, r *http.Request, next string) (*domain.Subject, bool) {
	subject := s.subjectFrom(r)
	if subject != nil {
		return subject, true
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(localPath(next, "/account")), http.StatusSeeOther)
	return nil, false
}

// ---------------------------------------------------------------------------
// Вход по коду из бота
// ---------------------------------------------------------------------------

type loginData struct {
	Next          string
	Code          string
	GoogleEnabled bool
	GoogleError   string
	SupportURL    string
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.subjectFrom(r) != nil {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	data := loginData{
		Next:          localPath(r.URL.Query().Get("next"), "/account"),
		Code:          strings.TrimSpace(r.URL.Query().Get("code")),
		GoogleEnabled: s.cfg.GoogleClientID != "" && s.cfg.GoogleSecret != "",
	}
	if r.URL.Query().Get("error") == "google" {
		data.GoogleError = "Не удалось войти через Google. Попробуйте ещё раз или используйте код из бота."
	}
	s.renderPage(w, r, "login.html", http.StatusOK, pageMeta{Title: "Вход"}, data)
}

func (s *Server) loginByCode(w http.ResponseWriter, r *http.Request) {
	next := localPath(r.PostFormValue("next"), "/account")
	code := normalizeCode(r.PostFormValue("code"))
	if code == "" {
		s.flashRedirect(w, r, "/login?next="+url.QueryEscape(next), "error", "Введите код из бота.")
		return
	}
	if !s.limit.allow("login:"+clientIP(r), 10, 10*time.Minute) {
		s.flashRedirect(w, r, "/login?next="+url.QueryEscape(next), "error",
			"Слишком много попыток. Подождите несколько минут.")
		return
	}

	accountID, err := s.db.ConsumeLinkCode(r.Context(), code)
	if err != nil {
		s.log.Error("не удалось проверить код входа", "err", err)
		s.flashRedirect(w, r, "/login?next="+url.QueryEscape(next), "error",
			"Не получилось войти. Попробуйте позже.")
		return
	}
	if accountID == nil {
		s.flashRedirect(w, r, "/login?next="+url.QueryEscape(next), "error",
			"Код не найден или уже истёк. Получите новый командой /weblogin в боте.")
		return
	}
	subject, err := s.db.SubjectByID(r.Context(), *accountID)
	if err != nil {
		s.log.Error("не удалось прочитать аккаунт по коду", "err", err)
		s.flashRedirect(w, r, "/login?next="+url.QueryEscape(next), "error",
			"Аккаунт недоступен. Напишите в поддержку.")
		return
	}
	if err := s.startSession(w, r, subject.ID); err != nil {
		s.log.Error("не удалось создать сессию", "err", err)
		s.flashRedirect(w, r, "/login?next="+url.QueryEscape(next), "error",
			"Не получилось войти. Попробуйте позже.")
		return
	}
	_ = s.db.FunnelStep(r.Context(), "signin")
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func normalizeCode(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearSession(w, r)
	s.flashRedirect(w, r, "/", "ok", "Вы вышли из кабинета.")
}

// ---------------------------------------------------------------------------
// Google OAuth
// ---------------------------------------------------------------------------

type oauthPending struct {
	verifier string
	next     string
	at       time.Time
}

type oauthStates struct {
	mu     sync.Mutex
	states map[string]oauthPending
}

func newOAuthStates() *oauthStates {
	return &oauthStates{states: map[string]oauthPending{}}
}

func (o *oauthStates) put(state string, pending oauthPending) {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	for key, item := range o.states {
		if now.Sub(item.at) > 15*time.Minute {
			delete(o.states, key)
		}
	}
	o.states[state] = pending
}

func (o *oauthStates) take(state string) (oauthPending, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	pending, ok := o.states[state]
	if ok {
		delete(o.states, state)
	}
	if !ok || time.Since(pending.at) > 15*time.Minute {
		return oauthPending{}, false
	}
	return pending, true
}

func (s *Server) googleEnabled() bool {
	return s.cfg.GoogleClientID != "" && s.cfg.GoogleSecret != ""
}

func (s *Server) googleRedirectURL(r *http.Request) string {
	return s.siteURL(r) + "/auth/google/callback"
}

func (s *Server) googleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.googleEnabled() {
		s.loginPage(w, r)
		return
	}
	next := localPath(r.URL.Query().Get("next"), "/account")
	verifier := base64.RawURLEncoding.EncodeToString(randomBytes(48))
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomToken(24)
	s.oauth.put(state, oauthPending{verifier: verifier, next: next, at: time.Now()})

	params := url.Values{}
	params.Set("client_id", s.cfg.GoogleClientID)
	params.Set("redirect_uri", s.googleRedirectURL(r))
	params.Set("response_type", "code")
	params.Set("scope", "openid email profile")
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "online")
	params.Set("prompt", "select_account")
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+params.Encode(), http.StatusSeeOther)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if !s.googleEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.URL.Query().Get("error") != "" {
		s.flashRedirect(w, r, "/login", "error", "Вход через Google отменён.")
		return
	}
	pending, ok := s.oauth.take(r.URL.Query().Get("state"))
	code := r.URL.Query().Get("code")
	if !ok || code == "" {
		s.flashRedirect(w, r, "/login", "error", "Ссылка входа устарела. Начните заново.")
		return
	}
	redirectURL := s.googleRedirectURL(r)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	identity, err := s.exchangeGoogle(ctx, code, pending.verifier, redirectURL)
	if err != nil {
		s.log.Warn("google: обмен кода не удался", "err", err)
		s.flashRedirect(w, r, "/login", "error", "Не удалось войти через Google.")
		return
	}

	_, lookupErr := s.db.SubjectByGoogleSub(ctx, identity.Sub)
	isNew := errors.Is(lookupErr, storage.ErrNotFound)
	if lookupErr != nil && !isNew {
		s.log.Error("google: чтение аккаунта", "err", lookupErr)
		s.flashRedirect(w, r, "/login", "error", "Не получилось войти. Попробуйте позже.")
		return
	}
	subject, err := s.db.UpsertGoogleSubject(ctx, identity.Sub, identity.Email, identity.Name)
	if err != nil {
		s.log.Error("google: сохранение аккаунта", "err", err)
		s.flashRedirect(w, r, "/login", "error", "Не получилось войти. Попробуйте позже.")
		return
	}
	if isNew {
		_ = s.db.FunnelStep(ctx, "signup")
		s.recordReferral(r, subject)
	}
	_ = s.db.FunnelStep(ctx, "signin")
	if err := s.startSession(w, r, subject.ID); err != nil {
		s.log.Error("google: сессия", "err", err)
		s.flashRedirect(w, r, "/login", "error", "Не получилось войти. Попробуйте позже.")
		return
	}
	http.Redirect(w, r, pending.next, http.StatusSeeOther)
}

type googleIdentity struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (s *Server) exchangeGoogle(ctx context.Context, code, verifier, redirectURL string) (googleIdentity, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", s.cfg.GoogleClientID)
	form.Set("client_secret", s.cfg.GoogleSecret)
	form.Set("redirect_uri", redirectURL)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return googleIdentity{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return googleIdentity{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return googleIdentity{}, fmt.Errorf("google token: %s: %s", resp.Status, string(body))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return googleIdentity{}, err
	}
	if token.AccessToken == "" {
		return googleIdentity{}, errors.New("google не вернул access_token")
	}

	infoReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return googleIdentity{}, err
	}
	infoReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	infoResp, err := http.DefaultClient.Do(infoReq)
	if err != nil {
		return googleIdentity{}, err
	}
	defer infoResp.Body.Close()
	infoBody, _ := io.ReadAll(io.LimitReader(infoResp.Body, 1<<20))
	if infoResp.StatusCode != http.StatusOK {
		return googleIdentity{}, fmt.Errorf("google userinfo: %s", infoResp.Status)
	}
	var identity googleIdentity
	if err := json.Unmarshal(infoBody, &identity); err != nil {
		return googleIdentity{}, err
	}
	if identity.Sub == "" {
		return googleIdentity{}, errors.New("google не вернул sub")
	}
	return identity, nil
}

func randomBytes(size int) []byte {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	return buf
}

// ---------------------------------------------------------------------------
// Реферальные ссылки
// ---------------------------------------------------------------------------

func (s *Server) captureRef(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("ref"))
	if code == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     refCookie,
		Value:    code,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
	if err := s.db.RecordPromoVisit(r.Context(), strings.ToUpper(code)); err != nil {
		s.log.Debug("не удалось посчитать переход по коду", "err", err)
	}
}

func (s *Server) recordReferral(r *http.Request, subject domain.Subject) {
	cookie, err := r.Cookie(refCookie)
	if err != nil || cookie.Value == "" {
		return
	}
	referrer, err := s.db.SubjectByRefCode(r.Context(), cookie.Value)
	if err != nil || referrer.ID == 0 || referrer.ID == subject.ID {
		return
	}
	if err := s.db.CreateReferral(r.Context(), referrer.ID, subject.ID); err != nil {
		s.log.Warn("не удалось записать реферала", "err", err)
	}
}

// constantTimeEqual — сравнение секретов без подсказки по времени.
func constantTimeEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
