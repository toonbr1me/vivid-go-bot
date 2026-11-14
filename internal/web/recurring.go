package web

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) accountCancelRecurring(w http.ResponseWriter, r *http.Request) {
	subject, ok := s.requireSubject(w, r, "/account")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.pay.CancelRecurring(ctx, subject.ID); err != nil {
		s.log.Warn("отмена автопродления", "subject", subject.ID, "err", err)
		s.flashRedirect(w, r, "/account", "error", "Не удалось подтвердить отмену. Автопродление пока включено; повторите попытку или обратитесь в поддержку.")
		return
	}
	s.flashRedirect(w, r, "/account", "ok", "Автопродление отключено. Уже оплаченный срок сохранён. Платёж, отправленный до отмены, ещё может завершиться.")
}
