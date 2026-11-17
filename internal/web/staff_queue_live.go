package web

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) staffQueueStream(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePermission(w, r, "tickets.view")
	if !ok {
		return
	}
	actor := telegramIDOf(a)
	c := http.NewResponseController(w)
	_ = c.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	updates := s.hub.subscribe(0)
	defer s.hub.unsubscribe(0, updates)
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	send := func() bool {
		if !s.db.Permission(r.Context(), actor, "tickets.view", s.cfg.Current().AdminIDs) {
			return false
		}
		_, e := fmt.Fprint(w, "data: {}\n\n")
		return e == nil && c.Flush() == nil
	}
	if !send() {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			if !send() {
				return
			}
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}
