package web

import (
	"context"
	"crypto/subtle"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"net/http"
	"os"
	"strings"
	"time"
)

type operationHealth struct {
	Paid, Inbox, Broadcasts int
	OldestPaidSeconds       float64
	Jobs                    []storage.JobHealth
}

func (s *Server) operationHealth(ctx context.Context) (h operationHealth, err error) {
	err = s.db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM orders WHERE status='paid' AND provisioned_at IS NULL),(SELECT count(*) FROM payment_inbox WHERE processed_at IS NULL),(SELECT count(*) FROM broadcast_deliveries WHERE status IN ('pending','sending')),COALESCE((SELECT extract(epoch from now()-min(created_at)) FROM orders WHERE status='paid' AND provisioned_at IS NULL),0)::float8`).Scan(&h.Paid, &h.Inbox, &h.Broadcasts, &h.OldestPaidSeconds)
	if err != nil {
		return
	}
	h.Jobs, err = s.db.JobHealth(ctx)
	return
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Pool.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, "ready")
}
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(os.Getenv("METRICS_TOKEN"))
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	h, err := s.operationHealth(ctx)
	if err != nil {
		http.Error(w, "metrics unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "vivid_paid_waiting %d\nvivid_paid_oldest_seconds %g\nvivid_payment_inbox_pending %d\nvivid_broadcast_pending %d\n", h.Paid, h.OldestPaidSeconds, h.Inbox, h.Broadcasts)
	for _, j := range h.Jobs {
		fmt.Fprintf(w, "vivid_job_failures{name=%q} %d\n", j.Name, j.Failures)
		if j.LastSuccess != nil {
			fmt.Fprintf(w, "vivid_job_last_success_timestamp_seconds{name=%q} %d\n", j.Name, j.LastSuccess.Unix())
		}
	}
}
func (s *Server) adminOperations(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePermission(w, r, "stats.view"); !ok {
		return
	}
	h, err := s.operationHealth(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_operations.html", 200, pageMeta{Title: "Состояние сервиса"}, h)
}
