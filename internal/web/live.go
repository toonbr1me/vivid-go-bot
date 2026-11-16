package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
)

// supportChannel — канал Postgres, по которому приходят уведомления об
// изменениях в обращениях. Триггер в схеме шлёт его при каждом изменении.
const supportChannel = "support_updates"

// liveHub — подписчики живых обновлений.
//
// Подписка идёт на конкретное обращение, но есть и общая: сотрудник
// держит открытой очередь и должен видеть новые тикеты сразу.
type liveHub struct {
	mu          sync.Mutex
	subscribers map[int64]map[chan struct{}]struct{}
	all         map[chan struct{}]struct{}
}

func newLiveHub() *liveHub {
	return &liveHub{
		subscribers: map[int64]map[chan struct{}]struct{}{},
		all:         map[chan struct{}]struct{}{},
	}
}

// subscribe — подписаться на обновления обращения (ticketID = 0 — на все).
func (h *liveHub) subscribe(ticketID int64) chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	if ticketID == 0 {
		h.all[ch] = struct{}{}
		return ch
	}
	if h.subscribers[ticketID] == nil {
		h.subscribers[ticketID] = map[chan struct{}]struct{}{}
	}
	h.subscribers[ticketID][ch] = struct{}{}
	return ch
}

// unsubscribe — отписаться и закрыть канал.
func (h *liveHub) unsubscribe(ticketID int64, ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ticketID == 0 {
		delete(h.all, ch)
	} else if set := h.subscribers[ticketID]; set != nil {
		delete(set, ch)
		if len(set) == 0 {
			delete(h.subscribers, ticketID)
		}
	}
	close(ch)
}

// notify — сообщить подписчикам: конкретному обращению и всем остальным.
func (h *liveHub) notify(ticketID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	notify := func(ch chan struct{}) {
		select {
		case ch <- struct{}{}:
		default: // сигнал уже стоит в очереди — второй раз не нужен
		}
	}
	for ch := range h.all {
		notify(ch)
	}
	for ch := range h.subscribers[ticketID] {
		notify(ch)
	}
}

// StartLive включает живые обновления обращений. Слушатель поднимается
// отдельным соединением и переподключается сам.
func (s *Server) StartLive(ctx context.Context) {
	s.startListener(ctx)
}

// startListener слушает канал Postgres и будит подписчиков.
//
// Соединение отдельное: LISTEN нельзя выполнять на соединении пула —
// оно может уйти к другому запросу.
func (s *Server) startListener(ctx context.Context) {
	go func() {
		backoff := 5 * time.Second
		for ctx.Err() == nil {
			if err := s.listenOnce(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("слушатель обновлений обращений прерван", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}()
}

func (s *Server) listenOnce(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, s.cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "LISTEN "+supportChannel); err != nil {
		return err
	}
	s.log.Info("слушатель обновлений обращений подключён")
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		ticketID, err := strconv.ParseInt(strings.TrimSpace(notification.Payload), 10, 64)
		if err != nil {
			continue
		}
		s.hub.notify(ticketID)
	}
}

// ticketStream — живой поток сообщений по обращению.
//
// Формат — Server-Sent Events: браузер сам переподключается, а страница
// обновляет переписку без перезагрузки.
func (s *Server) ticketStream(w http.ResponseWriter, r *http.Request) {
	subject := s.subjectFrom(r)
	if subject == nil {
		http.NotFound(w, r)
		return
	}
	ticketID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || ticketID <= 0 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	ticket, err := s.db.TicketByID(ctx, ticketID)
	if err != nil || !ticketOwned(ticket, subject) && s.staffAccess(ctx, subject).Role == "" {
		http.NotFound(w, r)
		return
	}

	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	updates := s.hub.subscribe(ticketID)
	defer s.hub.unsubscribe(ticketID, updates)

	send := func() bool {
		payload, err := s.ticketPayload(ctx, ticketID)
		if err != nil {
			return false
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
			return false
		}
		return controller.Flush() == nil
	}

	if !send() {
		return
	}
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-updates:
			if !send() {
				return
			}
		case <-ticker.C:
			// Комментарий-пинг: прокси не рвут соединение по тишине.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

// ticketPayload — переписка и состояние обращения для живого потока.
func (s *Server) ticketPayload(ctx context.Context, ticketID int64) (map[string]any, error) {
	ticket, err := s.db.TicketByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	messages, err := s.db.TicketHistory(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		items = append(items, map[string]any{
			"id":         message.ID,
			"text":       message.Text,
			"html":       mdhtml.Render(message.Text),
			"from_admin": message.FromAdmin,
			"assistant":  message.Assistant,
			"created_at": message.CreatedAt.UTC().Format(time.RFC3339),
			"attachment": message.FileID != nil,
		})
	}
	return map[string]any{
		"ticket_id": ticketID,
		"status":    ticket.Status,
		"open":      ticket.Status == "open",
		"messages":  items,
	}, nil
}
