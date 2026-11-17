package web

import (
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"net/http"
	"strings"
)

func (s *Server) giftsPage(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, r.URL.RequestURI())
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		if r.PostFormValue("action") == "claim" {
			code := strings.TrimSpace(r.PostFormValue("code"))
			code = strings.TrimPrefix(code, "gift_")
			order, err := s.pay.ClaimGift(r.Context(), *subject, code)
			if err != nil {
				s.renderError(w, r, 409, "Подарок не активирован", err.Error())
				return
			}
			message := "Подарок активирован."
			if order.Status != domain.OrderProvisioned {
				message = "Подарок принят. Доступ будет выдан автоматически после восстановления связи с панелью."
			}
			s.flashRedirect(w, r, "/account", "ok", message)
			return
		}
		purchase := payments.Purchase{AccountID: &subject.ID, PlanID: r.PostFormValue("plan"), Option: r.PostFormValue("option"), Web: true, Gift: true, RequestID: r.PostFormValue("request_id")}
		var checkout domain.Checkout
		var err error
		if r.PostFormValue("option") == "balance" {
			checkout, err = s.pay.PurchaseWithBalance(r.Context(), purchase)
		} else {
			checkout, err = s.pay.Checkout(r.Context(), purchase)
		}
		if err != nil {
			s.renderError(w, r, 400, "Подарок не оформлен", err.Error())
			return
		}
		if checkout.URL != "" {
			s.redirectPayment(w, r, checkout)
			return
		}
		s.flashRedirect(w, r, "/gifts", "ok", "Подарок приобретён. Ссылка доступна в истории.")
		return
	}
	plans, err := s.pay.SellablePlans(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	eligible := []domain.Plan{}
	for _, p := range plans {
		if p.GiftEnabled {
			eligible = append(eligible, p)
		}
	}
	gifts, err := s.db.Gifts(r.Context(), subject.ID)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "gifts.html", 200, pageMeta{Title: "Подарки"}, struct {
		Plans                                  []domain.Plan
		Gifts                                  []storage.Gift
		Options                                []domain.PaymentOption
		Claim, BotUsername, SiteURL, RequestID string
	}{eligible, gifts, s.pay.PaymentOptions(true), r.URL.Query().Get("claim"), s.cfg.Current().BotUsername, s.siteURL(r), randomToken(16)})
}

func (s *Server) historyPage(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/history")
	if !ok {
		return
	}
	f, err := parseOrderFilter(r)
	if err != nil {
		s.renderError(w, r, 400, "Фильтр", err.Error())
		return
	}
	f.SubjectID = &subject.ID
	page, err := s.db.SearchOrders(r.Context(), f)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "history.html", 200, pageMeta{Title: "История покупок"}, page)
}
