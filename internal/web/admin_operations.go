package web

import (
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, action string) (adminSession, bool) {
	a, ok := s.adminAccess(w, r)
	if !ok {
		return a, false
	}
	if !a.Can(action) {
		s.renderError(w, r, 403, "Нет доступа", "Недостаточно прав.")
		return a, false
	}
	return a, true
}

var settingsEditMu sync.Mutex

type settingField struct{ Key, Value string }

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePermission(w, r, "settings.edit")
	if !ok {
		return
	}
	settingsEditMu.Lock()
	defer settingsEditMu.Unlock()
	values, err := s.db.AppSettings(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	if r.Method == http.MethodPost {
		nextValues := map[string]string{}
		for k, v := range values {
			nextValues[k] = v
		}
		edits := map[string]string{}
		for _, k := range config.EditableSettings {
			if r.Form.Has(k) {
				v := strings.TrimSpace(r.PostFormValue(k))
				edits[k] = v
				nextValues[k] = v
			}
		}
		next, err := config.Load(nextValues)
		if err != nil {
			s.renderError(w, r, 400, "Настройки не сохранены", err.Error())
			return
		}
		if next.RefBalancePercent > 100 || next.DeviceLimitBase > 100 || next.TrialBotDays > 365 || next.TrialWebDays > 365 || next.RemindNotConnectedHours > 8760 || next.RemindUnpaidHours > 8760 {
			s.renderError(w, r, 400, "Настройки не сохранены", "Проверьте проценты, число устройств и сроки.")
			return
		}
		next.BotUsername = s.cfg.Current().BotUsername
		if err = s.db.SaveSettings(r.Context(), edits, telegramIDOf(a)); err != nil {
			s.operationError(w, r, err)
			return
		}
		s.cfg.Publish(next)
		s.flashRedirect(w, r, "/admin/settings", "ok", "Настройки применены.")
		return
	}
	fields := []settingField{}
	for _, k := range config.EditableSettings {
		fields = append(fields, settingField{k, values[k]})
	}
	s.renderPage(w, r, "admin_settings.html", 200, pageMeta{Title: "Настройки"}, fields)
}

func (s *Server) adminPlans(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePermission(w, r, "plans.manage")
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		number := func(k string) int { n, _ := strconv.Atoi(r.PostFormValue(k)); return n }
		if r.PostFormValue("kind") == "devices" {
			p := domain.DevicePack{ID: strings.TrimSpace(r.PostFormValue("id")), Title: strings.TrimSpace(r.PostFormValue("title")), Devices: number("devices"), Days: number("days"), Price: domain.FromRubles(int64(number("price"))), SortOrder: number("sort"), IsActive: r.PostFormValue("active") == "on"}
			if !catalogueID(p.ID) || p.Title == "" || p.Devices < 1 || p.Devices > 100 || p.Days < 1 || p.Days > 3660 || p.Price <= 0 {
				s.renderError(w, r, 400, "Некорректный пакет", "Проверьте срок, цену и число устройств.")
				return
			}
			if err := s.db.SaveDevicePack(r.Context(), p); err != nil {
				s.operationError(w, r, err)
				return
			}
		} else {
			p := domain.Plan{ID: strings.TrimSpace(r.PostFormValue("id")), Title: strings.TrimSpace(r.PostFormValue("title")), Days: number("days"), Devices: number("devices"), Price: domain.FromRubles(int64(number("price"))), SortOrder: number("sort"), IsActive: r.PostFormValue("active") == "on", GiftEnabled: r.PostFormValue("gifts") == "on"}
			if !catalogueID(p.ID) || strings.HasPrefix(p.ID, "trial") || p.ID == "gift" || p.Title == "" || p.Days < 1 || p.Days > 3660 || p.Price <= 0 || p.Devices < 0 || p.Devices > 100 {
				s.renderError(w, r, 400, "Некорректный тариф", "Проверьте ID, срок, цену и число устройств.")
				return
			}
			if raw := strings.TrimSpace(r.PostFormValue("traffic")); raw != "" {
				n, err := config.ParseBytes(raw)
				if err != nil {
					s.renderError(w, r, 400, "Некорректный трафик", err.Error())
					return
				}
				p.TrafficLimitBytes = &n
			}
			if raw := strings.TrimSpace(r.PostFormValue("squads")); raw != "" {
				p.InternalSquads = strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == ' ' })
			}
			if raw := strings.TrimSpace(r.PostFormValue("external")); raw != "" {
				p.ExternalSquadUUID = &raw
			}
			if err := s.db.SavePlan(r.Context(), p); err != nil {
				s.operationError(w, r, err)
				return
			}
		}
		_ = s.db.LogAction(r.Context(), telegramIDOf(a), "plans.manage", r.PostFormValue("id"), 0, "Изменён каталог")
		s.flashRedirect(w, r, "/admin/plans", "ok", "Каталог обновлён. Условия уже созданных заказов сохранены.")
		return
	}
	plans, err := s.db.PaidPlans(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	packs, err := s.db.DevicePacks(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_plans.html", 200, pageMeta{Title: "Тарифы и устройства"}, struct {
		Plans []domain.Plan
		Packs []domain.DevicePack
	}{plans, packs})
}
func catalogueID(s string) bool {
	if len(s) < 1 || len(s) > 24 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (s *Server) operationError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("операция не выполнена", "err", err)
	s.renderError(w, r, 500, "Операция не выполнена", "Повторите позже.")
}

func (s *Server) adminStaff(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePermission(w, r, "staff.manage")
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
		role := r.PostFormValue("role")
		if err != nil || id <= 0 || s.cfg.Current().AdminIDs[id] || role != "support" && role != "admin" && role != "disabled" {
			s.renderError(w, r, 400, "Проверьте сотрудника", "Владельцы из ADMIN_IDS управляются через окружение.")
			return
		}
		if role == "disabled" {
			_, err = s.db.RemoveStaff(r.Context(), id)
		} else {
			err = s.db.AddStaff(r.Context(), id, role, strings.TrimSpace(r.PostFormValue("name")), telegramIDOf(a))
		}
		if err != nil {
			s.operationError(w, r, err)
			return
		}
		_ = s.db.LogAction(r.Context(), telegramIDOf(a), "staff.manage", strconv.FormatInt(id, 10), 0, role)
		s.flashRedirect(w, r, "/admin/staff", "ok", "Права обновлены.")
		return
	}
	staff, err := s.db.Staff(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_staff.html", 200, pageMeta{Title: "Сотрудники"}, staff)
}

func (s *Server) staffTicketAction(w http.ResponseWriter, r *http.Request) {
	action := r.PostFormValue("action")
	permission := "tickets." + action
	if action == "reopen" {
		permission = "tickets.close"
	}
	a, ok := s.requirePermission(w, r, permission)
	if !ok {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil {
		s.notFound(w, r)
		return
	}
	target, _ := strconv.ParseInt(r.PostFormValue("target"), 10, 64)
	err := s.db.StaffTicketAction(r.Context(), id, telegramIDOf(a), action, strings.TrimSpace(r.PostFormValue("message")), target)
	if err != nil {
		s.renderError(w, r, 409, "Действие не выполнено", err.Error())
		return
	}
	if action == "reply" {
		if t, e := s.db.TicketByID(r.Context(), id); e == nil && t.TelegramID != nil && s.notify != nil {
			_ = s.notify.SendText(r.Context(), *t.TelegramID, "Поддержка ответила в обращении #"+strconv.FormatInt(id, 10)+". Откройте поддержку для просмотра.")
		}
	}
	s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(id, 10), "ok", "Обращение обновлено.")
}

func (s *Server) adminWithdrawals(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePermission(w, r, "withdrawal.approve")
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
		decision := r.PostFormValue("decision")
		if err != nil || decision != "approved" && decision != "rejected" {
			s.renderError(w, r, 400, "Проверьте решение", "Неизвестное действие.")
			return
		}
		if err = s.db.DecideWithdrawalAtomic(r.Context(), id, telegramIDOf(a), decision); err != nil {
			s.renderError(w, r, 409, "Решение не сохранено", err.Error())
			return
		}
		s.flashRedirect(w, r, "/admin/withdrawals", "ok", "Решение сохранено.")
		return
	}
	rows, err := s.db.Withdrawals(r.Context(), "", 200)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_withdrawals.html", 200, pageMeta{Title: "Выплаты"}, rows)
}

func parseOrderFilter(r *http.Request) (storage.OrderFilter, error) {
	q := r.URL.Query()
	f := storage.OrderFilter{Query: strings.TrimSpace(q.Get("q")), Status: q.Get("status"), Provider: q.Get("provider"), Attention: q.Get("attention") == "1"}
	f.Page, _ = strconv.Atoi(q.Get("page"))
	for _, k := range []string{"from", "to"} {
		if raw := q.Get(k); raw != "" {
			t, e := time.ParseInLocation("2006-01-02", raw, time.Local)
			if e != nil {
				return f, fmt.Errorf("неверная дата %s", k)
			}
			if k == "to" {
				t = t.AddDate(0, 0, 1)
				f.To = &t
			} else {
				f.From = &t
			}
		}
	}
	return f, nil
}

func (s *Server) adminCustomer(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "orders.view"); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	customer, err := s.db.SubjectByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	access, accessErr := s.pay.SupportAccess(r.Context(), customer)
	balance, err := s.db.Balance(r.Context(), id)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	f, err := parseOrderFilter(r)
	if err != nil {
		s.renderError(w, r, 400, "Фильтр", err.Error())
		return
	}
	f.SubjectID = &id
	orders, err := s.db.SearchOrders(r.Context(), f)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	tickets, err := s.db.TicketsForSubject(r.Context(), id, 100)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_customer.html", 200, pageMeta{Title: "Клиент"}, struct {
		Customer    domain.Subject
		Access      domain.Access
		AccessError bool
		Balance     domain.Money
		Orders      storage.OrderPage
		Tickets     []storage.Ticket
	}{customer, access, accessErr != nil, balance, orders, tickets})
}
