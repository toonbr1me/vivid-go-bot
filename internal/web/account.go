package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type accountData struct {
	Guidance      *domain.CustomerGuidance
	GuidanceURL   string
	Recurring     *domain.RecurringAgreement
	Access        domain.Access
	AccessError   string
	Orders        []domain.Order
	Referral      storage.ReferralStats
	RefLink       string
	ShowLink      bool
	LinkCode      string
	LinkURL       string
	DeviceSlots   []domain.DeviceSlotView
	DeviceQuotes  []domain.DeviceQuote
	DeviceLimit   int
	DeviceEnabled bool
	Options       []domain.PaymentOption
	Tickets       []storage.Ticket
	Promo         string
	ReferralMode  string
	HasTelegram   bool
	HasGoogle     bool
	BotUsername   string
	TrialEnabled  bool
}

func (s *Server) accountPage(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	data := accountData{
		DeviceEnabled: s.cfg.DeviceLimitEnabled,
		Options:       s.pay.PaymentOptions(true),
		Promo:         strings.TrimSpace(r.URL.Query().Get("promo")),
		HasTelegram:   subject.TelegramID != nil,
		HasGoogle:     subject.GoogleSub != nil,
		BotUsername:   s.cfg.BotUsername,
		TrialEnabled:  s.cfg.TrialWebDays > 0,
	}
	access, err := s.pay.Access(ctx, *subject, panelUsername(subject))
	data.Access = access
	if guidance, err := s.pay.CustomerGuidance(ctx, *subject, access, "web"); err == nil {
		data.Guidance = &guidance
		switch guidance.Action {
		case "connect":
			data.GuidanceURL = "/connect"
		case "order":
			data.GuidanceURL = "/pay/success?order=" + url.QueryEscape(guidance.OrderID)
		case "recurring":
			data.GuidanceURL = "/account#recurring"
		case "support":
			data.GuidanceURL = "/support"
		default:
			data.GuidanceURL = "/buy"
		}
	}
	if err != nil {
		data.AccessError = "Панель не отвечает — показываем последние известные данные."
		if access.Found {
			data.AccessError = "Подписка доступна. Не удалось обновить часть сведений об устройствах."
		}
	}
	data.DeviceLimit = access.DeviceLimit
	if a, err := s.db.LatestRecurringForSubject(ctx, subject.ID); err == nil {
		data.Recurring = &a
	}
	if data.DeviceLimit == 0 && s.cfg.DeviceLimitEnabled {
		if managed, err := s.pay.ManagedDeviceLimit(ctx, subject.ID); err == nil {
			data.DeviceLimit = managed
		}
	}
	if orders, err := s.pay.OrderHistory(ctx, *subject, 10); err == nil {
		data.Orders = orders
	} else {
		s.log.Warn("кабинет: история заказов", "err", err)
	}
	if stats, err := s.db.ReferralStatsFor(ctx, subject.ID); err == nil {
		data.Referral = stats
	}
	if mode, err := s.db.ReferralMode(ctx, subject.ID); err == nil && mode != "" {
		data.ReferralMode = mode
	} else if s.cfg.ReferralMode != "" {
		data.ReferralMode = s.cfg.ReferralMode
	}
	if subject.RefCode != nil && s.cfg.BotUsername != "" {
		data.RefLink = "https://t.me/" + s.cfg.BotUsername + "?start=ref_" + *subject.RefCode
	}
	if topic := r.URL.Query().Get("link"); topic == "1" && subject.TelegramID == nil {
		data.ShowLink = true
		code, err := s.db.CreateLinkCode(ctx, subject.ID)
		if err != nil {
			s.log.Error("кабинет: код привязки", "err", err)
		} else {
			data.LinkCode = code
			if s.cfg.BotUsername != "" {
				data.LinkURL = "https://t.me/" + s.cfg.BotUsername + "?start=link_" + code
			}
		}
	}
	if s.cfg.DeviceLimitEnabled && access.ExpireAt != nil {
		slots, err := s.pay.DeviceSlots(ctx, subject.ID, access.ExpireAt)
		if err != nil {
			s.log.Warn("кабинет: слоты устройств", "err", err)
		}
		data.DeviceSlots = slots
		quotes, err := s.pay.DeviceQuotes(ctx, access.ExpireAt, time.Now().UTC())
		if err != nil {
			s.log.Warn("кабинет: цены устройств", "err", err)
		}
		data.DeviceQuotes = quotes
	}
	if tickets, err := s.db.TicketsForSubject(ctx, subject.ID, 5); err == nil {
		data.Tickets = tickets
	}
	s.renderPage(w, r, "account.html", http.StatusOK, pageMeta{Title: "Кабинет"}, data)
}

// accountReferralMode переключает, чем начислять пригласившему: днями
// подписки или деньгами на внутренний баланс.
func (s *Server) accountReferralMode(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	mode := strings.TrimSpace(r.PostFormValue("mode"))
	switch mode {
	case "days", "balance", "both":
	default:
		s.flashRedirect(w, r, "/account", "error", "Неизвестный режим награды.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.db.SetReferralMode(ctx, subject.ID, mode); err != nil {
		s.log.Error("кабинет: режим награды", "err", err)
		s.flashRedirect(w, r, "/account", "error", "Не удалось сохранить режим.")
		return
	}
	s.flashRedirect(w, r, "/account", "ok", "Режим награды сохранён.")
}

func (s *Server) accountPromo(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("code"))
	code, valid := domain.NormalizePromoCode(raw)
	if !valid {
		s.flashRedirect(w, r, "/account", "error", "Код не похож на промокод: латинские буквы и цифры, от 2 до 32 символов.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if _, err := s.db.GiftPromoByCode(ctx, code); err == nil {
		days, err := s.pay.RedeemGiftPromo(ctx, *subject, code)
		if err != nil {
			s.flashRedirect(w, r, "/account", "error", promoError(err))
			return
		}
		s.flashRedirect(w, r, "/account", "ok",
			"Подарок активирован: +"+strconv.Itoa(days)+" "+daysWord(days)+" доступа.")
		return
	}

	quotes, err := s.pay.QuotePlans(ctx, subject.ID, code)
	if err != nil {
		s.flashRedirect(w, r, "/account", "error", "Не удалось проверить промокод. Попробуйте позже.")
		return
	}
	accepted, rejection := false, ""
	for _, quote := range quotes {
		if quote.Rejection == "" {
			accepted = true
			break
		}
		if rejection == "" {
			rejection = quote.Rejection
		}
	}
	if !accepted {
		if rejection == "" {
			rejection = "код не подходит ни к одному тарифу"
		}
		s.flashRedirect(w, r, "/account", "error", "Промокод не принят: "+rejection)
		return
	}
	_ = s.db.RememberPromoIntent(ctx, subject.ID, code)
	s.flashRedirect(w, r, "/buy?promo="+url.QueryEscape(code), "ok",
		"Промокод принят — цены на странице тарифов уже со скидкой.")
}

func (s *Server) accountCheckOrder(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	orderID := strings.TrimSpace(r.PostFormValue("order"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	order, err := s.db.OrderByID(ctx, orderID)
	if err != nil || !orderBelongsTo(&order, subject) {
		s.flashRedirect(w, r, "/account", "error", "Заказ не найден.")
		return
	}
	updated, err := s.pay.CheckOrder(ctx, orderID)
	if err != nil {
		s.log.Warn("проверка оплаты не удалась", "order", orderID, "err", err)
		if saved, e := s.pay.Order(ctx, orderID); e == nil {
			order = saved
		}
		p := order.Progress(time.Now())
		s.flashRedirect(w, r, "/account", "error", p.Label+". "+p.Detail+" Обновление статуса временно недоступно.")
		return
	}
	switch updated.Status {
	case domain.OrderProvisioned:
		s.flashRedirect(w, r, "/account", "ok", "Оплата подтверждена, доступ выдан.")
	case domain.OrderPaid:
		s.flashRedirect(w, r, "/account", "ok", "Оплата получена. Выдача занимает несколько секунд.")
	default:
		s.flashRedirect(w, r, "/account", "error", "Оплата пока не поступила. Если деньги списаны, напишите в поддержку.")
	}
}

func (s *Server) accountDetachDevice(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	hwid := strings.TrimSpace(r.PostFormValue("hwid"))
	if hwid == "" {
		s.flashRedirect(w, r, "/account", "error", "Устройство не указано.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	access, err := s.pay.Access(ctx, *subject, panelUsername(subject))
	if err != nil || access.PanelUserID == 0 {
		s.flashRedirect(w, r, "/account", "error", "Подписка не найдена в панели.")
		return
	}
	removed, err := s.pay.RemoveAccessDevice(ctx, access.PanelUserID, hwid)
	if err != nil || !removed {
		s.flashRedirect(w, r, "/account", "error", "Не удалось отвязать устройство. Попробуйте позже.")
		return
	}
	s.flashRedirect(w, r, "/account", "ok", "Устройство отвязано. Освободившийся слот можно занять снова.")
}

func (s *Server) accountBuyDevices(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	packID := strings.TrimSpace(r.PostFormValue("pack"))
	option := strings.TrimSpace(r.PostFormValue("option"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	checkout, err := s.pay.CheckoutDevices(ctx, payments.DevicePurchase{
		TelegramID: subject.TelegramID,
		AccountID:  &subject.ID,
		Username:   panelUsername(subject),
		PackID:     packID,
		Option:     option,
		Web:        true,
	})
	if err != nil {
		s.log.Warn("докупка устройств не удалась", "pack", packID, "err", err)
		s.flashRedirect(w, r, "/account", "error", deviceError(err))
		return
	}
	s.finishCheckout(w, r, checkout)
}

func (s *Server) accountRenewDevices(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	packID := strings.TrimSpace(r.PostFormValue("pack"))
	grantID := strings.TrimSpace(r.PostFormValue("grant"))
	option := strings.TrimSpace(r.PostFormValue("option"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	checkout, err := s.pay.CheckoutDevices(ctx, payments.DevicePurchase{
		TelegramID:   subject.TelegramID,
		AccountID:    &subject.ID,
		Username:     panelUsername(subject),
		PackID:       packID,
		Option:       option,
		GrantOrderID: grantID,
		Web:          true,
	})
	if err != nil {
		s.log.Warn("продление устройств не удалось", "pack", packID, "err", err)
		s.flashRedirect(w, r, "/account", "error", deviceError(err))
		return
	}
	s.finishCheckout(w, r, checkout)
}

func deviceError(err error) string {
	switch {
	case errors.Is(err, payments.ErrDevicesDisabled):
		return "Докупка устройств выключена."
	case errors.Is(err, payments.ErrDeviceTermTooShort):
		return "Докупить устройства можно при сроке подписки больше суток."
	case errors.Is(err, payments.ErrGrantNotRenewable):
		return "Этот пакет нельзя продлить."
	case errors.Is(err, payments.ErrGrantNotOwned):
		return "Пакет принадлежит другому аккаунту."
	case errors.Is(err, payments.ErrNoProviders):
		return "Оплата временно недоступна."
	case errors.Is(err, payments.ErrNoSubscription):
		return "Сначала оформите подписку — устройства докупаются к ней."
	case errors.Is(err, payments.ErrPackUnavailable):
		return "Этот пакет больше не продаётся."
	default:
		return "Не удалось создать счёт на устройства. Попробуйте позже."
	}
}

func promoError(err error) string {
	if errors.Is(err, payments.ErrPromoRejected) {
		return "Промокод не принят: " + strings.TrimPrefix(err.Error(), payments.ErrPromoRejected.Error()+": ")
	}
	return "Не удалось применить промокод. Попробуйте позже."
}
