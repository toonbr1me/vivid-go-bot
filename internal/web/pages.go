package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// ---------------------------------------------------------------------------
// Витрина
// ---------------------------------------------------------------------------

type planCard struct {
	Plan    domain.Plan
	Monthly domain.Money
	Best    bool
	Devices int
}

type landingData struct {
	Plans          []planCard
	TrialDays      int
	TrialAvailable bool
	LoggedIn       bool
	StarterPrice   domain.Money
	PaymentOptions int
}

func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	s.captureRef(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	plans, err := s.pay.SellablePlans(ctx)
	if err != nil {
		s.log.Error("витрина: тарифы", "err", err)
	}
	best := domain.BestPriceIDs(plans)
	cards := make([]planCard, 0, len(plans))
	starter := domain.Money(0)
	for _, plan := range plans {
		if starter == 0 || plan.Price < starter {
			starter = plan.Price
		}
		cards = append(cards, planCard{
			Plan:    plan,
			Monthly: domain.Money(monthlyPrice(plan)),
			Best:    best[plan.ID],
			Devices: domain.PlanDevicesTotal(plan, s.cfg.DeviceLimitBase),
		})
	}

	data := landingData{
		Plans:          cards,
		TrialDays:      s.cfg.TrialWebDays,
		LoggedIn:       s.subjectFrom(r) != nil,
		StarterPrice:   starter,
		PaymentOptions: len(s.pay.PaymentOptions(true)),
	}
	if subject := s.subjectFrom(r); subject != nil {
		used, err := s.db.HasTrial(ctx, subject.ID, "web")
		data.TrialAvailable = err == nil && !used
	}
	_ = s.db.FunnelStep(ctx, "landing")
	s.renderPage(w, r, "landing.html", http.StatusOK, pageMeta{}, data)
}

// ---------------------------------------------------------------------------
// Покупка
// ---------------------------------------------------------------------------

type buyQuote struct {
	Quote domain.PlanQuote
	Stars int
}

type buyData struct {
	Quotes         []buyQuote
	Options        []domain.PaymentOption
	Selected       string
	Promo          string
	PromoRejected  string
	TrialPlan      domain.Plan
	TrialAvailable bool
	TrialDays      int
	LoggedIn       bool
	WebPayments    bool
}

func (s *Server) buyPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	subject := s.subjectFrom(r)
	var subjectID int64
	if subject != nil {
		subjectID = subject.ID
	}
	promo := strings.TrimSpace(r.URL.Query().Get("promo"))
	if promo == "" && subject != nil {
		if remembered, ok, err := s.db.PromoIntent(ctx, subject.ID); err == nil && ok {
			promo = remembered
		}
	}
	quotes, err := s.pay.QuotePlans(ctx, subjectID, promo)
	if err != nil {
		s.log.Error("покупка: расчёт цен", "err", err)
		s.renderError(w, r, http.StatusInternalServerError, "Цены недоступны",
			"Не получилось получить каталог. Попробуйте обновить страницу.")
		return
	}

	data := buyData{
		Options:   s.pay.PaymentOptions(true),
		Selected:  r.URL.Query().Get("plan"),
		Promo:     promo,
		LoggedIn:  subject != nil,
		TrialDays: s.cfg.TrialWebDays,
	}
	registry := s.pay.Registry()
	for _, quote := range quotes {
		view := buyQuote{Quote: quote}
		if registry != nil {
			view.Stars = registry.StarsAmount(quote.Amount)
		}
		if quote.Rejection != "" && data.PromoRejected == "" {
			data.PromoRejected = quote.Rejection
		}
		data.Quotes = append(data.Quotes, view)
	}
	if data.Selected == "" && len(data.Quotes) > 0 {
		data.Selected = data.Quotes[0].Quote.Plan.ID
	}
	if subject != nil {
		if used, err := s.db.HasTrial(ctx, subject.ID, "web"); err == nil {
			data.TrialAvailable = !used
		}
	}
	for _, plan := range s.trialPlans(ctx) {
		if plan.TrialKind == "web" {
			data.TrialPlan = plan
		}
	}
	s.renderPage(w, r, "buy.html", http.StatusOK, pageMeta{Title: "Тарифы"}, data)
}

func (s *Server) trialPlans(ctx context.Context) []domain.Plan {
	plans, err := s.pay.Plans(ctx)
	if err != nil {
		return nil
	}
	var out []domain.Plan
	for _, plan := range plans {
		if plan.IsTrial {
			out = append(out, plan)
		}
	}
	return out
}

func (s *Server) buySubmit(w http.ResponseWriter, r *http.Request) {
	planID := strings.TrimSpace(r.PostFormValue("plan"))
	option := strings.TrimSpace(r.PostFormValue("option"))
	promo := strings.TrimSpace(r.PostFormValue("promo"))
	back := "/buy?plan=" + url.QueryEscape(planID)
	if promo != "" {
		back += "&promo=" + url.QueryEscape(promo)
	}
	subject, ok := s.requireSubject(w, r, back)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	checkout, err := s.pay.Checkout(ctx, payments.Purchase{
		TelegramID: subject.TelegramID,
		AccountID:  &subject.ID,
		Username:   panelUsername(subject),
		PlanID:     planID,
		Option:     option,
		PromoCode:  promo,
		Web:        true,
	})
	if err != nil {
		s.log.Warn("покупка не удалась", "plan", planID, "err", err)
		s.flashRedirect(w, r, back, "error", purchaseError(err))
		return
	}
	_ = s.db.FunnelStep(ctx, "checkout")
	s.finishCheckout(w, r, checkout)
}

func (s *Server) trialSubmit(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	checkout, err := s.pay.Checkout(ctx, payments.Purchase{
		TelegramID: subject.TelegramID,
		AccountID:  &subject.ID,
		Username:   panelUsername(subject),
		PlanID:     "trial_web",
		Web:        true,
	})
	if err != nil {
		s.log.Warn("пробный период не выдан", "err", err)
		s.flashRedirect(w, r, "/", "error", trialError(err))
		return
	}
	_ = s.db.FunnelStep(ctx, "trial")
	s.finishCheckout(w, r, checkout)
}

func (s *Server) finishCheckout(w http.ResponseWriter, r *http.Request, checkout domain.Checkout) {
	switch {
	case checkout.URL != "":
		http.Redirect(w, r, checkout.URL, http.StatusSeeOther)
	case checkout.Trial:
		s.flashRedirect(w, r, "/account", "ok", "Пробный доступ выдан. Подключайтесь.")
	case checkout.Free:
		s.flashRedirect(w, r, "/account", "ok", "Готово: доступ выдан без оплаты.")
	default:
		s.flashRedirect(w, r, "/buy", "error", "Платёжный шлюз не вернул ссылку. Попробуйте другой способ оплаты.")
	}
}

func purchaseError(err error) string {
	switch {
	case errors.Is(err, payments.ErrTrialUsed):
		return "Пробный период уже использован."
	case errors.Is(err, payments.ErrTrialWhileActive):
		return "Подписка ещё действует — пробный период не положен."
	case errors.Is(err, payments.ErrNoProviders):
		return "Оплата временно недоступна. Напишите в поддержку."
	case errors.Is(err, payments.ErrPlanUnavailable):
		return "Этот тариф больше не продаётся. Выберите другой."
	case errors.Is(err, payments.ErrPromoRejected):
		return "Промокод не принят: " + strings.TrimPrefix(err.Error(), payments.ErrPromoRejected.Error()+": ")
	case errors.Is(err, payments.ErrInsufficientBalance):
		return "На балансе недостаточно средств."
	default:
		return "Не удалось создать счёт. Попробуйте ещё раз или выберите другой способ оплаты."
	}
}

func trialError(err error) string {
	switch {
	case errors.Is(err, payments.ErrTrialUsed):
		return "Пробный период уже использован."
	case errors.Is(err, payments.ErrTrialWhileActive):
		return "У вас уже есть действующая подписка."
	default:
		return "Не получилось выдать пробный доступ. Попробуйте позже."
	}
}

// ---------------------------------------------------------------------------
// Результат оплаты
// ---------------------------------------------------------------------------

type payResultData struct {
	Order      *domain.Order
	Error      string
	Access     *domain.Access
	OrderOwned bool
}

func (s *Server) paySuccess(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	data := payResultData{}
	subject := s.subjectFrom(r)
	if orderID := strings.TrimSpace(r.URL.Query().Get("order")); orderID != "" && subject != nil {
		// Authorize before refreshing: a redirect must not operate on somebody else's order.
		order, err := s.pay.Order(ctx, orderID)
		if err == nil && orderBelongsTo(&order, subject) {
			if updated, _ := s.pay.CheckOrder(ctx, orderID); updated.OrderID != "" {
				order = updated
			}
			if saved, e := s.pay.Order(ctx, orderID); e == nil {
				order = saved
			}
			data.Order = &order
			data.OrderOwned = true
		}
	}
	if subject != nil {
		if access, err := s.pay.Access(ctx, *subject, panelUsername(subject)); err == nil {
			data.Access = &access
		}
	}
	s.renderPage(w, r, "pay_result.html", http.StatusOK, pageMeta{Title: "Оплата"}, data)
}

func (s *Server) payFail(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, r, "pay_result.html", http.StatusOK, pageMeta{Title: "Оплата не прошла"},
		payResultData{Error: "Платёж не завершён. Проверьте статус заказа в кабинете. Если деньги списались, напишите в поддержку."})
}

func orderBelongsTo(order *domain.Order, subject *domain.Subject) bool {
	if order == nil || subject == nil {
		return false
	}
	if order.AccountID != nil && *order.AccountID == subject.ID {
		return true
	}
	if order.TelegramID != nil && subject.TelegramID != nil && *order.TelegramID == *subject.TelegramID {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Подключение и документы
// ---------------------------------------------------------------------------

type connectData struct {
	Access      *domain.Access
	AccessError string
	LoggedIn    bool
	GuideURL    string
}

func (s *Server) connectPage(w http.ResponseWriter, r *http.Request) {
	data := connectData{GuideURL: s.render.theme.ConnectGuideURL}
	if subject := s.subjectFrom(r); subject != nil {
		data.LoggedIn = true
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		access, err := s.pay.Access(ctx, *subject, panelUsername(subject))
		data.Access = &access
		if err != nil {
			data.AccessError = "Панель не отвечает: показываем последние известные данные."
		}
	}
	s.renderPage(w, r, "connect.html", http.StatusOK, pageMeta{Title: "Подключение"}, data)
}

type documentData struct {
	Title      string
	Paragraphs []string
	External   string
}

func (s *Server) termsPage(w http.ResponseWriter, r *http.Request) {
	s.documentPage(w, r, "Пользовательское соглашение", s.render.theme.TermsURL,
		s.render.theme.TermsText, "Документ ещё не опубликован. Напишите в поддержку, чтобы получить его.")
}

func (s *Server) privacyPage(w http.ResponseWriter, r *http.Request) {
	s.documentPage(w, r, "Политика конфиденциальности", s.render.theme.PrivacyURL,
		s.render.theme.PrivacyText, "Документ ещё не опубликован. Напишите в поддержку, чтобы получить его.")
}

func (s *Server) documentPage(w http.ResponseWriter, r *http.Request, title, external, text, fallback string) {
	if external != "" {
		http.Redirect(w, r, external, http.StatusSeeOther)
		return
	}
	chunks := paragraphs(text)
	if len(chunks) == 0 {
		chunks = []string{fallback}
	}
	s.renderPage(w, r, "documents.html", http.StatusOK, pageMeta{Title: title},
		documentData{Title: title, Paragraphs: chunks})
}

// ---------------------------------------------------------------------------
// Общее
// ---------------------------------------------------------------------------

type noticeData struct {
	Title string
	Text  string
	Link  string
	Label string
}

// panelUsername — имя человека в панели. Для Telegram им управляет служба,
// а веб-аккаунту нужно стабильное имя, иначе панель создаст второго юзера.
func panelUsername(subject *domain.Subject) string {
	if subject.TelegramID != nil {
		return ""
	}
	if subject.RemnawaveUser != nil && strings.TrimSpace(*subject.RemnawaveUser) != "" {
		return *subject.RemnawaveUser
	}
	return fmt.Sprintf("web_%d", subject.ID)
}

func orderStatusTone(status string) string {
	switch status {
	case domain.OrderProvisioned:
		return "ok"
	case domain.OrderFailed, domain.OrderCancelled:
		return "bad"
	default:
		return "wait"
	}
}

// ticketOwned — обращение принадлежит этому человеку.
func ticketOwned(ticket storage.Ticket, subject *domain.Subject) bool {
	if subject == nil {
		return false
	}
	if ticket.SubjectID != nil && *ticket.SubjectID == subject.ID {
		return true
	}
	if ticket.TelegramID != nil && subject.TelegramID != nil && *ticket.TelegramID == *subject.TelegramID {
		return true
	}
	return false
}
