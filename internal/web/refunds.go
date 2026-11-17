package web

import (
	"net/http"
	"strings"
)

func (s *Server) adminRefunds(w http.ResponseWriter, r *http.Request) {
	a, ok := s.requirePermission(w, r, "refund.manage")
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		err := s.db.UpdateRefundCase(r.Context(), telegramIDOf(a), strings.TrimSpace(r.PostFormValue("order")), strings.TrimSpace(r.PostFormValue("reason")), r.PostFormValue("status"), strings.TrimSpace(r.PostFormValue("reference")))
		if err != nil {
			s.renderError(w, r, 409, "Возврат не сохранён", err.Error())
			return
		}
		s.flashRedirect(w, r, "/admin/refunds", "ok", "Решение по возврату сохранено.")
		return
	}
	cases, err := s.db.RefundCases(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	s.renderPage(w, r, "admin_refunds.html", 200, pageMeta{Title: "Возвраты"}, cases)
}
