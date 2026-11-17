package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
	"github.com/toonbr1me/vivid-go-bot/internal/bot"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type experienceData struct {
	Theme   appearance.Theme
	Catalog template.JS
	Markup  string
	CSS     string
	Error   string
}

func (s *Server) experienceData(r *http.Request) (experienceData, error) {
	theme, err := appearance.Load(s.cfg.Current().WebDir)
	if err != nil {
		return experienceData{}, err
	}
	value, err := s.db.AppSetting(r.Context(), appearance.SettingKey)
	if err != nil {
		return experienceData{}, err
	}
	if value != "" {
		theme, err = appearance.Parse([]byte(value), theme)
		if err != nil {
			return experienceData{}, err
		}
	}
	assets, err := bot.PreviewAssets(theme)
	if err != nil {
		return experienceData{}, err
	}
	catalog, err := json.Marshal(struct {
		Screens []bot.PreviewScreen `json:"screens"`
		Markup  string              `json:"markup"`
		CSS     string              `json:"css"`
	}{bot.ScreenCatalog(), assets.Markup, assets.CSS})
	if err != nil {
		return experienceData{}, err
	}
	return experienceData{Theme: theme, Catalog: template.JS(catalog), Markup: assets.Markup, CSS: assets.CSS}, nil
}

func (s *Server) adminScreensPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminAccess(w, r); !ok {
		return
	}
	data, err := s.experienceData(r)
	if err != nil {
		s.renderError(w, r, 500, "Не удалось открыть каталог", "Проверьте настройки оформления.")
		return
	}
	s.renderPage(w, r, "admin_screens.html", 200, pageMeta{Title: "Экраны и сценарии"}, data)
}
func (s *Server) adminAppearancePage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminAccess(w, r); !ok {
		return
	}
	data, err := s.experienceData(r)
	if err != nil {
		s.renderError(w, r, 500, "Не удалось загрузить тему", "Проверьте bot-theme.json.")
		return
	}
	s.renderPage(w, r, "admin_appearance.html", 200, pageMeta{Title: "Оформление бота"}, data)
}
func (s *Server) adminAppearanceSave(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.renderError(w, r, 403, "Недостаточно прав", "Оформление меняет владелец сервиса.")
		return
	}
	if r.FormValue("operation") == "reset" {
		if err := s.db.DeleteAppSetting(r.Context(), appearance.SettingKey); err != nil {
			s.renderError(w, r, 500, "Не удалось сбросить тему", "Попробуйте позже.")
			return
		}
		_ = s.db.LogAction(r.Context(), *session.Subject.TelegramID, "appearance.reset", "bot", 0, "")
		s.flashRedirect(w, r, "/admin/appearance", "ok", "Восстановлена тема из файлов проекта.")
		return
	}
	radius, _ := strconv.Atoi(r.FormValue("radius"))
	spacing, _ := strconv.Atoi(r.FormValue("spacing"))
	height, _ := strconv.Atoi(r.FormValue("button_height"))
	theme := appearance.Theme{Name: r.FormValue("name"), Accent: r.FormValue("accent"), Radius: radius, Spacing: spacing, ButtonHeight: height, Heading: r.FormValue("heading"), Navigation: r.FormValue("navigation")}
	if err := theme.Validate(); err != nil {
		s.flashRedirect(w, r, "/admin/appearance", "error", err.Error())
		return
	}
	if _, err := bot.WidgetDocument(theme); err != nil {
		s.flashRedirect(w, r, "/admin/appearance", "error", err.Error())
		return
	}
	payload, _ := json.Marshal(theme)
	if err := s.db.SaveAppSetting(r.Context(), appearance.SettingKey, string(payload), *session.Subject.TelegramID); err != nil {
		s.renderError(w, r, 500, "Не удалось сохранить оформление", "Попробуйте позже.")
		return
	}
	_ = s.db.LogAction(r.Context(), *session.Subject.TelegramID, "appearance.update", "bot", 0, string(payload))
	s.flashRedirect(w, r, "/admin/appearance", "ok", "Оформление сохранено. Бот применит его при следующем переходе.")
}
func (s *Server) widgetRuntime(w http.ResponseWriter, r *http.Request) {
	assets, err := bot.PreviewAssets(appearance.Default())
	if err != nil {
		http.Error(w, "runtime unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(assets.Runtime))
}

type adminOrdersData struct {
	Search    storage.OrderPage
	Orders    []domain.Order
	Issues    []domain.RecurringAgreement
	Attention bool
}

func (s *Server) adminOrdersPage(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.renderError(w, r, 403, "Недостаточно прав", "Заказы доступны владельцу сервиса.")
		return
	}
	filter, err := parseOrderFilter(r)
	if err != nil {
		s.renderError(w, r, 400, "Фильтр", err.Error())
		return
	}
	search, err := s.db.SearchOrders(r.Context(), filter)
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	orders := search.Orders
	attention := filter.Attention

	issues, err := s.db.RecurringIssues(r.Context(), 100)
	if err != nil {
		s.renderError(w, r, 500, "Не удалось загрузить автопродления", "Попробуйте позже.")
		return
	}
	s.renderPage(w, r, "admin_orders.html", 200, pageMeta{Title: "Заказы"}, adminOrdersData{Search: search, Orders: orders, Issues: issues, Attention: attention})
}

func (s *Server) adminOrderRetry(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminAccess(w, r)
	if !ok {
		return
	}
	if !session.Owner {
		s.renderError(w, r, 403, "Недостаточно прав", "Повтор выдачи доступен владельцу.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	order, err := s.db.OrderByID(ctx, r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, 404, "Заказ не найден", "Проверьте номер заказа.")
		return
	}
	if order.Status == domain.OrderProvisioned {
		s.flashRedirect(w, r, "/admin/orders", "ok", "Доступ по этому заказу уже выдан.")
		return
	}
	if order.Status != domain.OrderPaid {
		s.renderError(w, r, 409, "Оплата не подтверждена", "Повтор выдачи разрешён только для оплаченного заказа.")
		return
	}
	_ = s.db.LogAction(ctx, *session.Subject.TelegramID, "order.retry", order.OrderID, 0, "Повтор выдачи оплаченного заказа из пульта")
	updated, err := s.pay.CheckOrder(ctx, order.OrderID)
	if err != nil {
		s.flashRedirect(w, r, "/admin/orders", "error", "Не удалось завершить выдачу. Оплаченный заказ сохранён, его можно проверить позже.")
		return
	}
	if updated.Status == domain.OrderProvisioned {
		s.flashRedirect(w, r, "/admin/orders", "ok", "Доступ выдан. Повторного списания оплаты не было.")
		return
	}
	s.flashRedirect(w, r, "/admin/orders", "ok", "Состояние заказа проверено.")
}
