package web

import (
	"github.com/toonbr1me/vivid-go-bot/internal/bot"
	"net/http"
)

func (s *Server) botMascot(w http.ResponseWriter, r *http.Request) {
	data, err := bot.MascotAsset(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}
