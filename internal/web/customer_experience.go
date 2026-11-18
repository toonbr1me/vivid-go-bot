package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Status polling never accepts a return URL as proof of payment. Ownership is
// checked before the existing, rate-limited reconciliation service is called.
func (s *Server) orderStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	subject := s.subjectFrom(r)
	if subject == nil {
		http.Error(w, `{"error":"Войдите в кабинет"}`, 401)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	order, err := s.pay.Order(ctx, r.PathValue("id"))
	if err != nil || !orderBelongsTo(&order, subject) {
		http.NotFound(w, r)
		return
	}
	if !s.limit.allow(fmt.Sprintf("order-status:%d", subject.ID), 30, time.Minute) {
		w.Header().Set("Retry-After", "10")
		http.Error(w, `{"error":"Повторите позже"}`, 429)
		return
	}
	refreshError := false
	if order.Status == domain.OrderPending || order.Status == domain.OrderPaid || order.Status == domain.OrderFailed || order.Status == domain.OrderCancelled {
		if _, err := s.pay.CheckOrder(ctx, order.OrderID); err != nil {
			refreshError = true
		}
		if saved, err := s.pay.Order(ctx, order.OrderID); err == nil {
			order = saved
		}
	}
	p := order.Progress(time.Now())
	next := customerOrderNext(order.Kind)
	_ = json.NewEncoder(w).Encode(struct {
		Status       string `json:"status"`
		Label        string `json:"label"`
		Detail       string `json:"detail"`
		Ready        bool   `json:"ready"`
		CanPay       bool   `json:"can_pay"`
		Poll         bool   `json:"poll"`
		Next         string `json:"next"`
		RefreshError bool   `json:"refresh_error"`
	}{order.Status, p.Label, p.Detail, p.Ready, p.CanPay, order.Status == domain.OrderPaid || order.Status == domain.OrderPending, next, refreshError})
}

func customerOrderNext(kind string) string {
	switch kind {
	case domain.KindDevices:
		return "/account/devices"
	case domain.KindGift:
		return "/gifts"
	case domain.KindBalance:
		return "/account/profile"
	default:
		return "/connect"
	}
}

func (s *Server) journeyEvent(w http.ResponseWriter, r *http.Request) {
	subject := s.subjectFrom(r)
	if subject == nil {
		http.Error(w, "Войдите в кабинет", 401)
		return
	}
	step := r.PostFormValue("step")
	// These are UI intents, never confirmation of an import or a connection.
	switch step {
	case "setup_started", "download_requested", "import_requested", "manual_copy_requested":
	default:
		http.Error(w, "Неизвестный шаг", 400)
		return
	}
	platform := r.PostFormValue("platform")
	if platform != "" {
		if _, ok := domain.ConnectionPlatformFor(platform); !ok {
			http.Error(w, "Неизвестное устройство", 400)
			return
		}
	}
	if !s.limit.allow(fmt.Sprintf("journey:%d", subject.ID), 40, time.Minute) {
		http.Error(w, "Повторите позже", 429)
		return
	}
	if err := s.db.RecordJourney(r.Context(), subject.ID, step, "web", platform); err != nil {
		http.Error(w, "Не удалось сохранить", 503)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) accountNotifications(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account/profile")
	if !ok {
		return
	}
	if subject.TelegramID == nil {
		s.flashRedirect(w, r, "/account/profile", "error", "Сначала привяжите Telegram для уведомлений")
		return
	}
	prefs, err := s.db.NotificationSettingsFor(r.Context(), *subject.TelegramID)
	if err == nil {
		prefs.Expiry = r.PostFormValue("expiry") == "1"
		prefs.Activity = r.PostFormValue("activity") == "1"
		err = s.db.SaveNotificationSettings(r.Context(), *subject.TelegramID, prefs)
	}
	if err != nil {
		s.flashRedirect(w, r, "/account/profile", "error", "Не удалось сохранить уведомления")
		return
	}
	s.flashRedirect(w, r, "/account/profile", "ok", "Настройки уведомлений сохранены")
}

func (s *Server) connectionQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	subject := s.subjectFrom(r)
	if subject == nil {
		http.Error(w, "Войдите в кабинет", 401)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	access, err := s.pay.Access(ctx, *subject, panelUsername(subject))
	if (err != nil && !access.Found) || !access.Active || !isHTTPURL(access.URL) {
		http.Error(w, "Подписка пока недоступна", 404)
		return
	}
	// Encode locally; personal subscription links never reach a QR service.
	png, err := qrcode.Encode(access.URL, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "Не удалось создать QR-код", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func supportChecks(access domain.Access, accessErr error) []string {
	if !access.Found {
		return []string{"Не удалось получить актуальное состояние подписки. Оплаченный срок можно проверить в кабинете."}
	}
	checks := []string{}
	if access.Active {
		checks = append(checks, "Подписка активна")
	} else {
		checks = append(checks, "Доступ не активен: проверьте срок или ограничения подписки")
	}
	if access.URL != "" {
		checks = append(checks, "Ссылка для добавления доступна")
	}
	if accessErr == nil && access.DeviceLimit > 0 {
		checks = append(checks, fmt.Sprintf("Устройств: %d из %d", len(access.Devices), access.DeviceLimit))
	}
	if accessErr != nil {
		checks = append(checks, "Список устройств временно недоступен")
	}
	return checks
}

func connectionImportURL(app, raw string) string {
	return "/app-import/" + app + "?u=" + url.QueryEscape(raw)
}

func safeSupportField(v string) string {
	v = strings.TrimSpace(v)
	if len([]rune(v)) > 100 || strings.ContainsAny(v, "\r\n") {
		return ""
	}
	return v
}
