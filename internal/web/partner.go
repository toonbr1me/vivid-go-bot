package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// partnerData — кабинет партнёра.
type partnerData struct {
	Partner     *domain.Partner
	Codes       []domain.PromoCode
	Rewards     []domain.PartnerReward
	Balance     domain.Money
	Rewarded    domain.Money
	Accrued     domain.Money
	Credited    domain.Money
	Cancelled   domain.Money
	Invite      *domain.Partner
	InviteToken string
}

// partnerClaim — привязка аккаунта к партнёрству.
//
// Партнёр получает ссылку-приглашение, открывает её и входит тем же
// Google-аккаунтом, которым будет заходить в кабинет. Аккаунт привязывается
// к партнёру один раз, и начисления с этого момента идут на его баланс.
func (s *Server) partnerClaim(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodPost {
		token = strings.TrimSpace(r.PostFormValue("token"))
	}
	if token == "" {
		s.renderError(w, r, http.StatusNotFound, "Ссылка не найдена",
			"Ссылка-приглашение выглядит как /partner/claim?token=…")
		return
	}
	subject := s.subjectFrom(r)
	if subject == nil {
		next := "/partner/claim?token=" + url.QueryEscape(token)
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if r.Method == http.MethodGet {
		partner, err := s.db.PartnerInvite(ctx, token)
		if err != nil || partner == nil {
			s.flashRedirect(w, r, "/partner", "error", "Приглашение недоступно. Попросите владельца выдать новую ссылку.")
			return
		}
		s.renderPage(w, r, "partner.html", http.StatusOK, pageMeta{Title: "Подтверждение партнёрства"}, partnerData{Invite: partner, InviteToken: token})
		return
	}
	partner, err := s.db.ClaimPartnerInvite(ctx, token, subject.ID)
	if err != nil {
		s.log.Error("партнёр: привязка аккаунта", "err", err)
		s.flashRedirect(w, r, "/partner", "error", "Не удалось привязать аккаунт.")
		return
	}
	if partner == nil {
		s.flashRedirect(w, r, "/partner", "error", "Партнёр не найден.")
		return
	}
	s.flashRedirect(w, r, "/partner", "ok",
		"Партнёрство с «"+partner.Name+"» подтверждено. Начисления будут приходить на баланс.")
}

// partnerCabinet — кабинет партнёра: его коды, клиенты и начисления.
func (s *Server) partnerCabinet(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/partner")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	partner, err := s.db.GetPartnerBySubject(ctx, subject.ID)
	if err != nil {
		s.log.Error("партнёр: кабинет", "err", err)
	}
	data := partnerData{}
	if partner != nil {
		data.Partner = partner
		totals, err := s.db.PartnerRewardTotals(ctx, partner.ID)
		if err != nil {
			s.renderError(w, r, http.StatusServiceUnavailable, "Начисления недоступны", "Не удалось получить суммы начислений. Попробуйте позже.")
			return
		}
		data.Rewarded, data.Accrued = totals.Accrued+totals.Credited, totals.Accrued
		data.Credited, data.Cancelled = totals.Credited, totals.Cancelled
		if codes, err := s.db.PartnerCodes(ctx, partner.ID); err == nil {
			data.Codes = codes
		}
		if rewards, err := s.db.PartnerRewards(ctx, partner.ID, 50); err == nil {
			data.Rewards = rewards
		}
	}
	if balance, err := s.db.Balance(ctx, subject.ID); err == nil {
		data.Balance = balance
	}
	s.renderPage(w, r, "partner.html", http.StatusOK, pageMeta{Title: "Партнёрство"}, data)
}
