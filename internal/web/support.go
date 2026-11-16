package web

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

type supportData struct {
	LoggedIn bool
	Tickets  []storage.Ticket
	Name     string
	Contact  string
	Message  string
	Email    string
	BotURL   string
	Order    string
}

func (s *Server) supportPage(w http.ResponseWriter, r *http.Request) {
	data := supportData{
		LoggedIn: s.subjectFrom(r) != nil,
		Email:    s.render.theme.SupportEmail,
		BotURL:   s.botURL(),
	}
	if subject := s.subjectFrom(r); subject != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if id := strings.TrimSpace(r.URL.Query().Get("order")); id != "" {
			o, err := s.pay.Order(ctx, id)
			if err != nil || !orderBelongsTo(&o, subject) {
				s.notFound(w, r)
				return
			}
			data.Order = id
		}
		if tickets, err := s.db.TicketsForSubject(ctx, subject.ID, 10); err == nil {
			data.Tickets = tickets
		}
	}
	s.renderPage(w, r, "support.html", http.StatusOK, pageMeta{Title: "Поддержка"}, data)
}

func (s *Server) supportSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.limit.allow("support:"+clientIP(r), 5, 10*time.Minute) {
		s.flashRedirect(w, r, "/support", "error", "Слишком много обращений. Подождите несколько минут.")
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	contact := strings.TrimSpace(r.PostFormValue("contact"))
	message := strings.TrimSpace(r.PostFormValue("message"))
	if message == "" {
		s.flashRedirect(w, r, "/support", "error", "Напишите, что случилось.")
		return
	}
	if len([]rune(message)) > 4000 {
		s.flashRedirect(w, r, "/support", "error", "Сообщение слишком длинное — до 4000 символов.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	subject := s.subjectFrom(r)
	if subject != nil {
		contextText := ""
		if id := strings.TrimSpace(r.PostFormValue("order")); id != "" {
			o, err := s.pay.Order(ctx, id)
			if err != nil || !orderBelongsTo(&o, subject) {
				s.notFound(w, r)
				return
			}
			contextText = "Заказ: " + id
		}
		for _, field := range []struct{ key, label string }{{"device", "Устройство"}, {"app", "Приложение"}} {
			value := strings.TrimSpace(r.PostFormValue(field.key))
			if len([]rune(value)) > 100 {
				s.renderError(w, r, 400, "Слишком длинное поле", "Укажите устройство и приложение короче 100 символов.")
				return
			}
			if value != "" {
				contextText += "\n" + field.label + ": " + value
			}
		}
		ticket, err := s.db.CreateTicketContent(ctx, subject.ID, subject.TelegramID,
			firstRunes(message, 60), message, s.ticketMode(), strings.TrimSpace(contextText), "", "")
		if err != nil {
			s.log.Error("поддержка: обращение", "err", err)
			s.flashRedirect(w, r, "/support", "error", "Не удалось создать обращение. Попробуйте позже.")
			return
		}
		s.notifyStaff(fmt.Sprintf("🆕 Обращение #%d с сайта\n\n%s\n\n%s",
			ticket.ID, html.EscapeString(ticket.Subject), html.EscapeString(firstRunes(message, 500))))
		s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(ticket.ID, 10), "ok",
			"Обращение создано. Ответ появится здесь и в боте, если Telegram привязан.")
		return
	}

	if contact == "" {
		s.flashRedirect(w, r, "/support", "error", "Оставьте почту или Telegram — иначе не сможем ответить.")
		return
	}
	if len([]rune(name)) > 200 || len([]rune(contact)) > 200 {
		s.flashRedirect(w, r, "/support", "error", "Имя и контакт короче 200 символов.")
		return
	}
	record, err := s.db.SaveWebSupportRequest(ctx, name, contact, message)
	if err != nil {
		s.log.Error("поддержка: заявка с сайта", "err", err)
		s.flashRedirect(w, r, "/support", "error", "Не удалось отправить заявку. Попробуйте позже.")
		return
	}
	s.notifyStaff(fmt.Sprintf("✉️ Заявка с сайта #%d\n\nИмя: %s\nКонтакт: %s\n\n%s",
		record.ID, html.EscapeString(record.Name), html.EscapeString(record.Contact),
		html.EscapeString(firstRunes(record.Message, 1000))))
	s.flashRedirect(w, r, "/support", "ok",
		"Заявка отправлена. Ответим на указанный контакт в течение дня.")
}

type ticketData struct {
	Ticket        storage.Ticket
	Messages      []storage.Message
	Open          bool
	Staff         bool
	CanCompensate bool
}

func (s *Server) ticketPage(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, r.URL.Path)
	if !ok {
		return
	}
	ticketID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || ticketID <= 0 {
		s.notFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ticket, err := s.db.TicketByID(ctx, ticketID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	// Сотрудник видит любое обращение: иначе ответить из админки нельзя.
	staff := s.staffAccess(ctx, subject).Role != ""
	if !staff && !ticketOwned(ticket, subject) {
		s.notFound(w, r)
		return
	}
	messages, err := s.db.TicketHistory(ctx, ticketID)
	if err != nil {
		s.log.Error("поддержка: переписка", "err", err)
	}
	if ticketOwned(ticket, subject) {
		_ = s.db.MarkTicketRead(ctx, ticket.ID, subject.ID, ticket.LastStaffMessageID)
	}
	canCompensate := false
	if subject.TelegramID != nil && s.db.Permission(ctx, *subject.TelegramID, "support.compensate", s.cfg.AdminIDs) {
		_, e := s.db.TicketCompensation(ctx, ticketID)
		canCompensate = errors.Is(e, storage.ErrNotFound)
	}
	s.renderPage(w, r, "ticket.html", http.StatusOK, pageMeta{Title: "Обращение #" + strconv.FormatInt(ticket.ID, 10)},
		ticketData{Ticket: ticket, Messages: messages, Open: ticket.Status == "open", Staff: staff, CanCompensate: canCompensate})
}

func (s *Server) ticketReply(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, r.URL.Path)
	if !ok {
		return
	}
	ticketID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || ticketID <= 0 {
		s.notFound(w, r)
		return
	}
	text := strings.TrimSpace(r.PostFormValue("message"))
	if text == "" {
		s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(ticketID, 10), "error", "Сообщение пустое.")
		return
	}
	if len([]rune(text)) > 4000 {
		s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(ticketID, 10), "error",
			"Сообщение слишком длинное — до 4000 символов.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ticket, err := s.db.TicketByID(ctx, ticketID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	staff := s.staffAccess(ctx, subject).Role != ""
	if !staff && !ticketOwned(ticket, subject) {
		s.notFound(w, r)
		return
	}
	if ticket.Status != "open" {
		s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(ticketID, 10), "error", "Обращение закрыто. Создайте новое, если нужна помощь.")
		return
	}
	if !staff {
		_, err = s.db.AddOwnerTicketMessage(ctx, ticketID, subject.ID, subject.TelegramID, text, "", "")
	} else {
		_, err = s.db.AddTicketMessage(ctx, ticketID, subject.ID, subject.TelegramID, text, true, "", "")
	}
	if err != nil {
		s.log.Error("поддержка: ответ", "err", err)
		s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(ticketID, 10), "error",
			"Не удалось отправить сообщение. Попробуйте позже.")
		return
	}
	if staff && !ticketOwned(ticket, subject) && s.notify != nil {
		owner := ticket.TelegramID
		if ticket.SubjectID != nil {
			if account, e := s.db.SubjectByID(ctx, *ticket.SubjectID); e == nil && account.TelegramID != nil {
				owner = account.TelegramID
			}
		}
		if owner != nil {
			if notifier, ok := s.notify.(interface {
				SendTicketUpdate(context.Context, int64, int64, string, string) error
			}); ok {
				if e := notifier.SendTicketUpdate(ctx, *owner, ticketID, text, "support"); e != nil {
					s.log.Warn("ответ сохранён, Telegram недоступен", "ticket", ticketID)
				}
			}
		}
	}
	s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(ticketID, 10), "ok", "Сообщение отправлено.")
}

// ticketMode — кто отвечает на новое обращение: сотрудник или помощник.
func (s *Server) ticketMode() string {
	if s.cfg != nil && s.cfg.AssistantEnabled() {
		return storage.SupportModeAssistant
	}
	return storage.SupportModeHuman
}

// notifyStaff рассылает уведомление сотрудникам в фоне: ответ на форму не
// должен ждать Telegram, а недоступность одного адресата не отменяет
// остальные.
func (s *Server) notifyStaff(text string) {
	if s.notify == nil {
		return
	}
	for adminID := range s.cfg.AdminIDs {
		id := adminID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := s.notify.SendText(ctx, id, text); err != nil {
				s.log.Warn("уведомление сотруднику не доставлено", "telegram_id", id, "err", err)
			}
		}()
	}
}

func firstRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
