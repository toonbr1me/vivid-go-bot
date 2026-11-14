package web

import (
	"context"
	"io"
	"net/http"
	"strconv"
)

func (s *Server) ticketResolve(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/support")
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	t, err := s.db.TicketByID(r.Context(), id)
	if err != nil || !ticketOwned(t, subject) {
		s.notFound(w, r)
		return
	}
	if _, err = s.db.ResolveOwnTicket(r.Context(), id, subject.ID); err != nil {
		s.renderError(w, r, 500, "Не удалось закрыть обращение", "Попробуйте позже.")
		return
	}
	s.flashRedirect(w, r, "/support/tickets/"+strconv.FormatInt(id, 10), "ok", "Проблема решена. Переписка сохранена в истории.")
}

type supportFileReader interface {
	OpenSupportFile(context.Context, string) (io.ReadCloser, error)
}

func (s *Server) ticketRead(w http.ResponseWriter, r *http.Request) {
	subject := s.subjectFrom(r)
	if subject == nil {
		s.notFound(w, r)
		return
	}
	id, e1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	through, e2 := strconv.ParseInt(r.PostFormValue("through"), 10, 64)
	if e1 != nil || e2 != nil || through < 0 {
		s.notFound(w, r)
		return
	}
	t, err := s.db.TicketByID(r.Context(), id)
	if err != nil || !ticketOwned(t, subject) {
		s.notFound(w, r)
		return
	}
	if err = s.db.MarkTicketRead(r.Context(), id, subject.ID, through); err != nil {
		http.Error(w, "Не удалось сохранить отметку прочтения", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ticketFile(w http.ResponseWriter, r *http.Request) {
	subject := s.subjectFrom(r)
	if subject == nil {
		s.notFound(w, r)
		return
	}
	id, e1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	messageID, e2 := strconv.ParseInt(r.PathValue("message"), 10, 64)
	if e1 != nil || e2 != nil {
		s.notFound(w, r)
		return
	}
	t, err := s.db.TicketByID(r.Context(), id)
	if err != nil || !ticketOwned(t, subject) && s.staffAccess(r.Context(), subject).Role == "" {
		s.notFound(w, r)
		return
	}
	var fileID string
	err = s.db.Pool.QueryRow(r.Context(), `SELECT file_id FROM support_messages WHERE id=$1 AND ticket_id=$2 AND NOT is_internal AND file_id IS NOT NULL`, messageID, id).Scan(&fileID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	reader, ok := s.notify.(supportFileReader)
	if !ok {
		http.Error(w, "Вложение временно недоступно. Откройте его в боте.", 503)
		return
	}
	file, err := reader.OpenSupportFile(r.Context(), fileID)
	if err != nil {
		http.Error(w, "Не удалось получить вложение. Повторите позже.", 502)
		return
	}
	defer file.Close()
	const maxFile = 20 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxFile+1))
	if err != nil || len(data) > maxFile {
		http.Error(w, "Вложение недоступно для скачивания на сайте. Откройте его в боте.", 502)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="support-attachment.bin"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
