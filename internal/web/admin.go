package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// Роли в админке. Владелец — список ADMIN_IDS, остальные — таблица staff.
const (
	adminRoleOwner   = "owner"
	adminRoleSupport = "support"
)

// adminSession — кто открыл админку и с каким правом.
type adminSession struct {
	Subject *domain.Subject
	Role    string
	Owner   bool
}

// Can — есть ли право на действие.
func (a adminSession) Can(action string) bool {
	if a.Owner {
		return storage.RoleCan(storage.RoleOwner, action)
	}
	return storage.RoleCan(a.Role, action)
}

// staffAccess определяет роль человека: владелец, сотрудник или никто.
//
// Владелец — список ADMIN_IDS, остальные — таблица staff. Роль нужна и
// админке, и странице обращения: сотрудник должен видеть чужие тикеты.
func (s *Server) staffAccess(ctx context.Context, subject *domain.Subject) adminSession {
	session := adminSession{Subject: subject}
	if subject == nil || subject.TelegramID == nil {
		return session
	}
	if s.cfg.Current().AdminIDs[*subject.TelegramID] {
		session.Role = adminRoleOwner
		session.Owner = true
		return session
	}
	member, err := s.db.StaffMemberByID(ctx, *subject.TelegramID)
	if err != nil || member == nil || !member.IsActive {
		return session
	}
	if !storage.RoleCan(member.Role, "tickets.view") {
		return session
	}
	session.Role = member.Role
	if member.Role == storage.RoleOwner || member.Role == storage.RoleAdmin {
		session.Owner = true
	}
	return session
}

// adminAccess пускает в админку только сотрудника.
//
// Вход тот же, что на сайте: Google-аккаунт. Но страница доступна лишь тем,
// чей Telegram в списке владельцев или в таблице staff: иначе любой
// зарегистрировавшийся через Google смотрел бы чужие заказы.
func (s *Server) adminAccess(w http.ResponseWriter, r *http.Request) (adminSession, bool) {
	subject := s.subjectFrom(r)
	if subject == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		return adminSession{}, false
	}
	session := s.staffAccess(r.Context(), subject)
	if session.Role == "" {
		s.renderError(w, r, http.StatusForbidden, "Нет доступа",
			"Админка открыта только сотрудникам: войдите тем же Google-аккаунтом, "+
				"который привязан к Telegram из списка владельцев.")
		return adminSession{}, false
	}
	return session, true
}

// ---------------------------------------------------------------------------
// Сводка
// ---------------------------------------------------------------------------

type adminDashboard struct {
	Stats           storage.StatsSummary
	Funnel          []storage.FunnelRow
	Orders          []domain.Order
	WebSupport      []storage.WebSupportRequest
	Tickets         int
	Owner           bool
	AwaitingAccess  int
	AwaitingPayment int
	RecurringErrors int
	OperationsError bool
}

// adminHome — главная страница админки.
func (s *Server) adminHome(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Can("stats.view") {
		http.Redirect(w, r, "/admin/tickets", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	data := adminDashboard{Owner: session.Owner}
	if stats, err := s.db.Stats(ctx, 30); err == nil {
		data.Stats = stats
	}
	if funnel, err := s.db.FunnelSteps(ctx, 30); err == nil {
		data.Funnel = funnel
	}
	if session.Owner {
		if ops, err := s.db.Operations(ctx); err == nil {
			data.AwaitingAccess = ops.AwaitingAccess
			data.AwaitingPayment = ops.StuckPayments
			data.RecurringErrors = ops.RecurringErrors
		} else {
			data.OperationsError = true
			s.log.Error("сводка операций", "err", err)
		}
		if recent, err := s.db.RecentOrders(ctx, 15); err == nil {
			data.Orders = recent
		}
	}
	if requests, err := s.db.WebSupportRequests(ctx, "new", 10); err == nil {
		data.WebSupport = requests
	}
	if count, err := s.db.OpenTicketCount(ctx); err == nil {
		data.Tickets = count
	}
	s.renderPage(w, r, "admin.html", http.StatusOK, pageMeta{Title: "Админка"}, data)
}

// ---------------------------------------------------------------------------
// Промокоды
// ---------------------------------------------------------------------------

type adminPromos struct {
	Codes    []domain.PromoCode
	Partners []domain.PartnerSummary
}

// adminPromosPage — список промокодов.
func (s *Server) adminPromosPage(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Can("promo.manage") {
		s.renderError(w, r, 403, "Нет доступа", "Недостаточно прав.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	data := adminPromos{}
	if codes, err := s.db.ListPromos(ctx, 100); err == nil {
		data.Codes = codes
	}
	if partners, err := s.db.ListPartners(ctx); err == nil {
		data.Partners = partners
	}
	s.renderPage(w, r, "admin_promos.html", http.StatusOK, pageMeta{Title: "Промокоды"}, data)
}

// adminPromoCreate — создание промокода.
func (s *Server) adminPromoCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.flashRedirect(w, r, "/admin/promos", "error", "Создавать промокоды может только владелец.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	code, valid := domain.NormalizePromoCode(r.PostFormValue("code"))
	if !valid {
		s.flashRedirect(w, r, "/admin/promos", "error",
			"Код должен состоять из латинских букв и цифр, от 2 до 32 символов.")
		return
	}
	discount, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("discount")))
	bonusDays, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("bonus_days")))
	grantDays, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("grant_days")))
	maxUses, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("max_uses")))
	perUser, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("per_user_limit")))
	if discount < 0 || discount > 100 {
		discount = 0
	}
	if bonusDays < 0 {
		bonusDays = 0
	}
	if grantDays < 0 {
		grantDays = 0
	}
	promo := domain.PromoCode{
		Code:            code,
		DiscountPercent: discount,
		BonusDays:       bonusDays,
		GrantDays:       grantDays,
		MaxUses:         maxUses,
		PerUserLimit:    perUser,
		IsActive:        true,
		RewardScope:     domain.RewardScopeAll,
	}
	if err := s.db.CreatePromo(ctx, promo); err != nil {
		s.log.Error("админка: промокод", "code", code, "err", err)
		s.flashRedirect(w, r, "/admin/promos", "error", "Не удалось создать промокод: "+err.Error())
		return
	}
	_ = s.db.LogAction(ctx, telegramIDOf(session), "promo.create", code, 0, "")
	s.flashRedirect(w, r, "/admin/promos", "ok", "Промокод "+code+" создан.")
}

// adminPromoToggle — включает и выключает промокод.
func (s *Server) adminPromoToggle(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.flashRedirect(w, r, "/admin/promos", "error", "Это может сделать только владелец.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	code := strings.TrimSpace(r.PathValue("code"))
	active := r.PostFormValue("state") != "off"
	if err := s.db.SetPromoActive(ctx, code, active); err != nil {
		s.log.Error("админка: переключение промокода", "code", code, "err", err)
		s.flashRedirect(w, r, "/admin/promos", "error", "Не удалось изменить промокод.")
		return
	}
	_ = s.db.LogAction(ctx, telegramIDOf(session), "promo.toggle", code, 0, "")
	s.flashRedirect(w, r, "/admin/promos", "ok", "Промокод "+code+" обновлён.")
}

// adminPromoPartner — вешает промокод на партнёра и задаёт его награду.
func (s *Server) adminPromoPartner(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.flashRedirect(w, r, "/admin/promos", "error", "Это может сделать только владелец.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	code := strings.TrimSpace(r.PathValue("code"))
	partnerID, _ := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("partner_id")), 10, 64)
	rewardType := strings.TrimSpace(r.PostFormValue("reward_type"))
	rewardScope := strings.TrimSpace(r.PostFormValue("reward_scope"))
	rewardValue := domain.RewardValueFromInput(rewardType, r.PostFormValue("reward_value"))
	if rewardScope == "" {
		rewardScope = domain.RewardScopeAll
	}
	var partner *int64
	if partnerID > 0 {
		partner = &partnerID
	}
	if err := s.db.UpdatePromoPartner(ctx, code, partner, rewardType, rewardValue, rewardScope); err != nil {
		s.log.Error("админка: партнёр промокода", "code", code, "err", err)
		s.flashRedirect(w, r, "/admin/promos", "error", "Не удалось сохранить партнёра.")
		return
	}
	_ = s.db.LogAction(ctx, telegramIDOf(session), "promo.partner", code, rewardValue, rewardType)
	s.flashRedirect(w, r, "/admin/promos", "ok", "Промокод "+code+" обновлён.")
}

// ---------------------------------------------------------------------------
// Партнёры
// ---------------------------------------------------------------------------

type adminPartners struct {
	Partners []domain.PartnerSummary
}

// adminPartnersPage — список партнёров.
func (s *Server) adminPartnersPage(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Can("partners.manage") {
		s.renderError(w, r, 403, "Нет доступа", "Недостаточно прав.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	data := adminPartners{}
	if partners, err := s.db.ListPartners(ctx); err == nil {
		data.Partners = partners
	}
	s.renderPage(w, r, "admin_partners.html", http.StatusOK, pageMeta{Title: "Партнёры"}, data)
}

// adminPartnerCreate — заводит партнёра.
func (s *Server) adminPartnerCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.flashRedirect(w, r, "/admin/partners", "error", "Это может сделать только владелец.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		s.flashRedirect(w, r, "/admin/partners", "error", "Укажите имя партнёра.")
		return
	}
	contact := strings.TrimSpace(r.PostFormValue("contact"))
	payout := strings.TrimSpace(r.PostFormValue("payout_details"))
	notes := strings.TrimSpace(r.PostFormValue("notes"))
	partner, err := s.db.CreatePartner(ctx, name, contact, payout, notes)
	if err != nil {
		s.log.Error("админка: партнёр", "name", name, "err", err)
		s.flashRedirect(w, r, "/admin/partners", "error", "Не удалось создать партнёра.")
		return
	}
	_ = s.db.LogAction(ctx, telegramIDOf(session), "partner.create", partner.Name, 0, "")
	s.flashRedirect(w, r, "/admin/partners", "ok", "Партнёр "+partner.Name+" создан.")
}

// adminPartnerPage — карточка партнёра.
func (s *Server) adminPartnerPage(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Can("partners.manage") {
		s.renderError(w, r, 403, "Нет доступа", "Недостаточно прав.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	partnerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	partner, err := s.db.GetPartner(ctx, partnerID)
	if err != nil || partner == nil {
		s.notFound(w, r)
		return
	}
	data := adminPartnerCard{Partner: *partner}
	if codes, err := s.db.PartnerCodes(ctx, partnerID); err == nil {
		data.Codes = codes
	}
	if rewards, err := s.db.PartnerRewards(ctx, partnerID, 50); err == nil {
		data.Rewards = rewards
	}
	s.renderPage(w, r, "admin_partner.html", http.StatusOK, pageMeta{Title: "Партнёр"}, data)
}

type adminPartnerCard struct {
	Partner domain.Partner
	Codes   []domain.PromoCode
	Rewards []domain.PartnerReward
}

// adminPartnerUpdate — правит карточку партнёра.
func (s *Server) adminPartnerUpdate(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.flashRedirect(w, r, "/admin/partners", "error", "Это может сделать только владелец.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	partnerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	status := strings.TrimSpace(r.PostFormValue("status"))
	if status != domain.PartnerActive && status != domain.PartnerPaused {
		status = ""
	}
	if _, err := s.db.UpdatePartner(ctx, partnerID,
		strings.TrimSpace(r.PostFormValue("name")),
		strings.TrimSpace(r.PostFormValue("contact")),
		strings.TrimSpace(r.PostFormValue("payout_details")),
		strings.TrimSpace(r.PostFormValue("notes")),
		status); err != nil {
		s.log.Error("админка: правка партнёра", "partner", partnerID, "err", err)
		s.flashRedirect(w, r, "/admin/partners", "error", "Не удалось сохранить партнёра.")
		return
	}
	if status == domain.PartnerPaused {
		if stopped, err := s.db.DeactivatePartnerCodes(ctx, partnerID); err == nil && stopped > 0 {
			_ = s.db.LogAction(ctx, telegramIDOf(session), "partner.pause", "", 0,
				fmt.Sprintf("выключено кодов: %d", stopped))
		}
	}
	_ = s.db.LogAction(ctx, telegramIDOf(session), "partner.update", "", 0, status)
	s.flashRedirect(w, r, "/admin/partners/"+strconv.FormatInt(partnerID, 10), "ok", "Партнёр обновлён.")
}

// adminPartnerInvite — выдаёт ссылку-приглашение.
func (s *Server) adminPartnerInvite(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.flashRedirect(w, r, "/admin/partners", "error", "Это может сделать только владелец.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	partnerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	code, err := s.db.CreatePartnerInviteCode(ctx, partnerID, 7*24*time.Hour)
	if err != nil {
		s.log.Error("админка: приглашение партнёра", "partner", partnerID, "err", err)
		s.flashRedirect(w, r, "/admin/partners", "error", "Не удалось создать приглашение.")
		return
	}
	_ = s.db.LogAction(ctx, telegramIDOf(session), "partner.invite", code, 0, "")
	s.flashRedirect(w, r, "/admin/partners/"+strconv.FormatInt(partnerID, 10), "ok",
		"Ссылка-приглашение создана: "+s.siteURL(r)+"/partner/claim?token="+code)
}

// ---------------------------------------------------------------------------
// Поддержка и журнал
// ---------------------------------------------------------------------------

type adminTickets struct {
	Tickets []storage.Ticket
	Queue   storage.StaffQueue
}

// adminTicketsPage — очередь обращений.
func (s *Server) adminTicketsPage(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Can("tickets.view") {
		s.renderError(w, r, http.StatusForbidden, "Нет доступа", "Очередь обращений доступна сотрудникам поддержки.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	staffID := int64(0)
	if session.Subject.TelegramID != nil {
		staffID = *session.Subject.TelegramID
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	queue, err := s.db.StaffQueue(ctx, staffID, r.URL.Query().Get("scope"), page)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	data := adminTickets{Queue: queue}
	s.renderPage(w, r, "admin_tickets.html", http.StatusOK, pageMeta{Title: "Обращения"}, data)
}

type adminAudit struct {
	Entries []storage.AuditEntry
}

// adminAuditPage — журнал действий сотрудников.
func (s *Server) adminAuditPage(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.renderError(w, r, http.StatusForbidden, "Нет доступа", "Журнал доступен владельцу.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	data := adminAudit{}
	if entries, err := s.db.RecentAudit(ctx, 100); err == nil {
		data.Entries = entries
	}
	s.renderPage(w, r, "admin_audit.html", http.StatusOK, pageMeta{Title: "Журнал"}, data)
}

// telegramIDOf — Telegram-аккаунт сотрудника для журнала действий.
func telegramIDOf(session adminSession) int64 {
	if session.Subject != nil && session.Subject.TelegramID != nil {
		return *session.Subject.TelegramID
	}
	return 0
}
