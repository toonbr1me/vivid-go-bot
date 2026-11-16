// Package web — публичный сайт: витрина, кабинет, поддержка и приём
// уведомлений платёжных шлюзов.
//
// Слой намеренно тонкий: страницы только собирают данные из payments и
// storage и раскладывают их по шаблонам. Никаких денежных решений здесь нет —
// иначе сайт и бот начали бы жить по разным правилам.
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// Server обслуживает сайт и вебхуки.
type Server struct {
	db     *storage.DB
	pay    *payments.Service
	cfg    *config.Settings
	log    *slog.Logger
	render *renderer
	mux    *http.ServeMux
	notify Notifier

	limit   *rateLimiter
	oauth   *oauthStates
	hub     *liveHub
	started time.Time
}

// Notifier доставляет уведомления сотрудникам. Бот реализует интерфейс;
// сайту не нужен весь бот, только отправка текста.
type Notifier interface {
	SendText(ctx context.Context, telegramID int64, text string) error
}

// New собирает сайт. Ошибка здесь — это ошибка темы или шаблона, то есть
// повод не запускаться, а не показать человеку кривую страницу.
func New(db *storage.DB, pay *payments.Service, cfg *config.Settings, logger *slog.Logger, notify Notifier) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	render, err := newRenderer(cfg)
	if err != nil {
		return nil, err
	}
	s := &Server{
		db:      db,
		pay:     pay,
		cfg:     cfg,
		log:     logger,
		render:  render,
		mux:     http.NewServeMux(),
		notify:  notify,
		limit:   newRateLimiter(),
		oauth:   newOAuthStates(),
		hub:     newLiveHub(),
		started: time.Now(),
	}
	s.routes()
	return s, nil
}

// Handler возвращает готовый обработчик: его можно повесить на любой
// http.Server, в том числе за обратный прокси.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.withSession(h)
	h = s.withSecurityHeaders(h)
	h = s.withLogging(h)
	return s.withRecovery(h)
}

// Run поднимает сервер и корректно останавливает его по отмене контекста.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.WebhookHost, s.cfg.WebhookPort)
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("сайт запущен", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("остановка сайта: %w", err)
	}
	return nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.landing)
	s.mux.HandleFunc("GET /buy", s.buyPage)
	s.mux.HandleFunc("POST /buy", s.csrf(s.buySubmit))
	s.mux.HandleFunc("POST /trial", s.csrf(s.trialSubmit))
	s.mux.HandleFunc("GET /pay/success", s.paySuccess)
	s.mux.HandleFunc("GET /pay/fail", s.payFail)

	s.mux.HandleFunc("GET /login", s.loginPage)
	s.mux.HandleFunc("POST /auth/code", s.csrf(s.loginByCode))
	s.mux.HandleFunc("GET /auth/google", s.googleLogin)
	s.mux.HandleFunc("GET /auth/google/callback", s.googleCallback)
	s.mux.HandleFunc("POST /logout", s.csrf(s.logout))

	s.mux.HandleFunc("GET /account", s.accountPage)
	s.mux.HandleFunc("POST /account/promo", s.csrf(s.accountPromo))
	s.mux.HandleFunc("POST /account/referral-mode", s.csrf(s.accountReferralMode))
	s.mux.HandleFunc("POST /account/orders/check", s.csrf(s.accountCheckOrder))
	s.mux.HandleFunc("POST /account/recurring/cancel", s.csrf(s.accountCancelRecurring))
	s.mux.HandleFunc("POST /account/devices/detach", s.csrf(s.accountDetachDevice))
	s.mux.HandleFunc("POST /account/devices/buy", s.csrf(s.accountBuyDevices))
	s.mux.HandleFunc("POST /account/devices/renew", s.csrf(s.accountRenewDevices))

	s.mux.HandleFunc("GET /connect", s.connectPage)
	s.mux.HandleFunc("GET /support", s.supportPage)
	s.mux.HandleFunc("POST /support", s.csrf(s.supportSubmit))
	s.mux.HandleFunc("GET /support/tickets/{id}", s.ticketPage)
	s.mux.HandleFunc("GET /support/tickets/{id}/updates", s.ticketStream)
	s.mux.HandleFunc("POST /support/tickets/{id}", s.csrf(s.ticketReply))
	s.mux.HandleFunc("POST /support/tickets/{id}/resolve", s.csrf(s.ticketResolve))
	s.mux.HandleFunc("POST /support/tickets/{id}/compensate", s.csrf(s.ticketCompensate))
	s.mux.HandleFunc("POST /support/tickets/{id}/read", s.csrf(s.ticketRead))
	s.mux.HandleFunc("GET /support/tickets/{id}/files/{message}", s.ticketFile)

	s.mux.HandleFunc("GET /terms", s.termsPage)
	s.mux.HandleFunc("GET /privacy", s.privacyPage)
	s.mux.HandleFunc("GET /happ", s.happBridge)
	s.mux.HandleFunc("GET /app-import/{app}", s.appImport)
	s.mux.HandleFunc("GET /static/bot-mascot/{name}", s.botMascot)
	s.mux.HandleFunc("GET /partner", s.partnerCabinet)
	s.mux.HandleFunc("GET /partner/claim", s.partnerClaim)
	s.mux.HandleFunc("POST /partner/claim", s.csrf(s.partnerClaim))

	// Админка: тот же вход через Google, но доступ — только сотрудникам.
	s.mux.HandleFunc("GET /admin", s.adminHome)
	s.mux.HandleFunc("GET /admin/promos", s.adminPromosPage)
	s.mux.HandleFunc("POST /admin/promos", s.csrf(s.adminPromoCreate))
	s.mux.HandleFunc("POST /admin/promos/{code}/toggle", s.csrf(s.adminPromoToggle))
	s.mux.HandleFunc("POST /admin/promos/{code}/partner", s.csrf(s.adminPromoPartner))
	s.mux.HandleFunc("GET /admin/partners", s.adminPartnersPage)
	s.mux.HandleFunc("POST /admin/partners", s.csrf(s.adminPartnerCreate))
	s.mux.HandleFunc("GET /admin/partners/{id}", s.adminPartnerPage)
	s.mux.HandleFunc("POST /admin/partners/{id}", s.csrf(s.adminPartnerUpdate))
	s.mux.HandleFunc("POST /admin/partners/{id}/invite", s.csrf(s.adminPartnerInvite))
	s.mux.HandleFunc("GET /admin/tickets", s.adminTicketsPage)
	s.mux.HandleFunc("GET /admin/audit", s.adminAuditPage)
	s.mux.HandleFunc("GET /admin/orders", s.adminOrdersPage)
	s.mux.HandleFunc("POST /admin/orders/{id}/retry", s.csrf(s.adminOrderRetry))
	s.mux.HandleFunc("GET /admin/screens", s.adminScreensPage)
	s.mux.HandleFunc("GET /admin/appearance", s.adminAppearancePage)
	s.mux.HandleFunc("POST /admin/appearance", s.csrf(s.adminAppearanceSave))
	s.mux.HandleFunc("GET /static/bot-widget-runtime.js", s.widgetRuntime)

	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "ok %s", time.Since(s.started).Truncate(time.Second))
	})

	s.mux.HandleFunc("POST /webhooks/{provider}", s.paymentWebhook)
	s.mux.HandleFunc("POST /webhooks/remnawave", s.panelWebhook)

	s.mux.Handle("/static/", s.render.staticHandler())
	s.mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon.svg", http.StatusTemporaryRedirect)
	})
	s.mux.HandleFunc("/", s.notFound)
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

type sessionKey struct{}

type requestState struct {
	subject *domain.Subject
}

func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &requestState{}
		if subject, err := s.sessionSubject(r); err == nil && subject != nil {
			state.subject = subject
		}
		ctx := context.WithValue(r.Context(), sessionKey{}, state)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func stateFrom(ctx context.Context) *requestState {
	if state, ok := ctx.Value(sessionKey{}).(*requestState); ok {
		return state
	}
	return &requestState{}
}

func (s *Server) subjectFrom(r *http.Request) *domain.Subject {
	return stateFrom(r.Context()).subject
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' https://fonts.gstatic.com; script-src 'self'; form-action 'self'; "+
				"frame-ancestors 'self'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

// ResponseController needs to reach the actual writer for streaming responses.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/static/") {
			return
		}
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		s.log.Info("запрос", "method", r.Method, "path", r.URL.Path,
			"status", sw.status, "duration", time.Since(start).Truncate(time.Millisecond))
	})
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.log.Error("паника обработчика", "path", r.URL.Path, "panic", recovered)
				s.renderError(w, r, http.StatusInternalServerError,
					"Что-то сломалось", "Мы уже знаем о проблеме. Загляните через пару минут.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// CSRF и одноразовые сообщения
// ---------------------------------------------------------------------------

const (
	sessionCookie = "vivid_session"
	csrfCookie    = "vivid_csrf"
	flashCookie   = "vivid_flash"
	refCookie     = "vivid_ref"
)

func (s *Server) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			s.renderError(w, r, http.StatusBadRequest, "Форма не разобрана", "Обновите страницу и попробуйте снова.")
			return
		}
		cookie, err := r.Cookie(csrfCookie)
		if err != nil || cookie.Value == "" || !constantTimeEqual(r.PostFormValue("csrf"), cookie.Value) {
			s.renderError(w, r, http.StatusForbidden, "Страница устарела",
				"Обновите страницу и отправьте форму ещё раз.")
			return
		}
		next(w, r)
	}
}

func (s *Server) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookie); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	token := randomToken(24)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   24 * 60 * 60,
	})
	return token
}

// flashRedirect показывает одноразовое сообщение после POST и уводит на GET,
// чтобы обновление страницы не повторяло действие.
func (s *Server) flashRedirect(w http.ResponseWriter, r *http.Request, path, kind, text string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    url.QueryEscape(kind + "|" + text),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60,
	})
	http.Redirect(w, r, path, http.StatusSeeOther)
}

type flashMessage struct {
	Kind string
	Text string
}

func (s *Server) takeFlash(w http.ResponseWriter, r *http.Request) *flashMessage {
	cookie, err := r.Cookie(flashCookie)
	if err != nil || cookie.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.isSecure(r), SameSite: http.SameSiteLaxMode,
	})
	decoded, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return nil
	}
	kind, text, ok := strings.Cut(decoded, "|")
	if !ok {
		return nil
	}
	return &flashMessage{Kind: kind, Text: text}
}

// ---------------------------------------------------------------------------
// Мелкие помощники
// ---------------------------------------------------------------------------

func (s *Server) isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// siteURL — адрес сайта без хвостового слэша. Для писем и ссылок берём
// SITE_BASE_URL, а если он не задан — адрес запроса.
func (s *Server) siteURL(r *http.Request) string {
	if s.cfg.SiteBaseURL != "" {
		return s.cfg.SiteBaseURL
	}
	if s.cfg.PublicBaseURL != "" {
		return s.cfg.PublicBaseURL
	}
	scheme := "http"
	if s.isSecure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) botURL() string {
	if s.cfg.BotUsername == "" {
		return ""
	}
	return "https://t.me/" + s.cfg.BotUsername
}

// localPath пускает только относительные адреса: адрес возврата после входа
// не должен уводить человека на чужой сайт.
func localPath(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return fallback
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fallback
	}
	return raw
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimiter — грубое ограничение на формы: без него один скрипт может
// засыпать поддержку и базу за минуту.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	lastGC time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{hits: map[string][]time.Time{}, lastGC: time.Now()}
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastGC) > time.Hour {
		for k, times := range l.hits {
			if len(times) == 0 || now.Sub(times[len(times)-1]) > window {
				delete(l.hits, k)
			}
		}
		l.lastGC = now
	}
	cutoff := now.Add(-window)
	times := l.hits[key]
	kept := times[:0]
	for _, at := range times {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
