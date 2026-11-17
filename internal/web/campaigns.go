package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

func (s *Server) campaignLocation() (*time.Location, error) {
	if zone := s.cfg.Current().Timezone; zone != "" {
		return time.LoadLocation(zone)
	}
	return time.Local, nil
}
func (s *Server) campaignInput(r *http.Request) (c storage.Campaign, err error) {
	c.ID = r.PathValue("id")
	c.Title = strings.TrimSpace(r.PostFormValue("title"))
	c.Audience = r.PostFormValue("audience")
	c.Text = strings.TrimSpace(r.PostFormValue("message"))
	c.PromoCode = strings.TrimSpace(r.PostFormValue("promo"))
	parse := func(key string) int {
		value, e := strconv.Atoi(r.PostFormValue(key))
		if e != nil {
			err = errors.New("укажите числовые лимиты и часы")
		}
		return value
	}
	c.Revision = parse("revision")
	c.DailyLimit = parse("daily_limit")
	c.CooldownDays = parse("cooldown_days")
	c.SendFrom = parse("send_from")
	c.SendTo = parse("send_to")
	location, e := s.campaignLocation()
	if e != nil {
		return c, e
	}
	c.StartsAt = time.Now()
	if raw := r.PostFormValue("starts_at"); raw != "" {
		c.StartsAt, e = time.ParseInLocation("2006-01-02T15:04", raw, location)
		if e != nil {
			return c, errors.New("проверьте дату начала")
		}
	}
	if c.PromoCode != "" {
		normalized, ok := domain.NormalizePromoCode(c.PromoCode)
		if !ok {
			return c, errors.New("неверный промокод")
		}
		c.PromoCode = normalized
	}
	return c, err
}
func (s *Server) campaignPromoAvailable(r *http.Request, code string) bool {
	if code == "" {
		return true
	}
	promo, err := s.db.PromoCode(r.Context(), code)
	return err == nil && promo.IsActive && promo.Live(time.Now()) && !promo.Exhausted()
}
func (s *Server) adminCampaigns(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requirePermission(w, r, "broadcast.send")
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		c, err := s.campaignInput(r)
		if err != nil {
			s.renderError(w, r, 400, "Кампания не сохранена", err.Error())
			return
		}
		if !s.campaignPromoAvailable(r, c.PromoCode) {
			s.renderError(w, r, 400, "Промокод недоступен", "Проверьте код, его срок и лимит применений.")
			return
		}
		c, err = s.db.SaveCampaign(r.Context(), c, telegramIDOf(actor))
		if err != nil {
			s.renderError(w, r, 400, "Кампания не сохранена", err.Error())
			return
		}
		s.flashRedirect(w, r, "/admin/campaigns/"+c.ID, "ok", "Черновик сохранён. Проверьте аудиторию и текст перед включением.")
		return
	}
	campaigns, err := s.db.Campaigns(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	location, err := s.campaignLocation()
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_campaigns.html", 200, pageMeta{Title: "Поведенческие кампании"}, struct {
		Campaigns []storage.Campaign
		Timezone  string
	}{campaigns, location.String()})
}
func (s *Server) adminCampaign(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requirePermission(w, r, "broadcast.send")
	if !ok {
		return
	}
	id := r.PathValue("id")
	if r.Method == http.MethodPost {
		revision, err := strconv.Atoi(r.PostFormValue("revision"))
		if err != nil {
			s.renderError(w, r, 400, "Обновите страницу", "Не найдена версия предпросмотра.")
			return
		}
		switch r.PostFormValue("action") {
		case "save":
			c, e := s.campaignInput(r)
			if e != nil {
				s.renderError(w, r, 400, "Кампания не сохранена", e.Error())
				return
			}
			if !s.campaignPromoAvailable(r, c.PromoCode) {
				s.renderError(w, r, 400, "Промокод недоступен", "Проверьте код перед сохранением.")
				return
			}
			_, err = s.db.SaveCampaign(r.Context(), c, telegramIDOf(actor))
		case "enable":
			c, e := s.db.Campaign(r.Context(), id)
			if e != nil {
				s.notFound(w, r)
				return
			}
			if !s.campaignPromoAvailable(r, c.PromoCode) {
				s.renderError(w, r, 409, "Промокод недоступен", "Исправьте кампанию перед включением.")
				return
			}
			err = s.db.SetCampaignEnabled(r.Context(), id, revision, telegramIDOf(actor), true)
		case "pause":
			err = s.db.SetCampaignEnabled(r.Context(), id, revision, telegramIDOf(actor), false)
		default:
			s.renderError(w, r, 400, "Неизвестное действие", "Обновите страницу кампании.")
			return
		}
		if err != nil {
			s.renderError(w, r, 409, "Кампания не изменена", err.Error())
			return
		}
		message := "Кампания поставлена на паузу."
		if r.PostFormValue("action") == "enable" {
			message = "Автоматическая отправка включена с выбранными условиями."
		}
		if r.PostFormValue("action") == "save" {
			message = "Изменения сохранены. Проверьте предпросмотр и включите новую версию."
		}
		s.flashRedirect(w, r, "/admin/campaigns/"+id, "ok", message)
		return
	}
	preview, err := s.db.CampaignPreview(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	location, err := s.campaignLocation()
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_campaign.html", 200, pageMeta{Title: preview.Campaign.Title}, struct {
		Preview               storage.CampaignPreview
		StartsLocal, Timezone string
	}{preview, preview.Campaign.StartsAt.In(location).Format("2006-01-02T15:04"), location.String()})
}

func campaignAudienceLabel(s string) string {
	switch s {
	case "trial_only":
		return "Пробовали без покупки"
	case "abandoned":
		return "Бросили оплату"
	case "expired":
		return "Подписка истекла"
	case "not_connected":
		return "Не подключились"
	}
	return s
}
