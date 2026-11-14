package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

// Only fixed schemes can be opened. The subscription URL is never fetched
// server-side or sent to an external link-generation service.
func importDeepLink(app, raw string) (string, bool) {
	if !isHTTPURL(raw) {
		return "", false
	}
	switch app {
	case "happ":
		return "happ://add/" + raw, true
	case "incy":
		return "incy://import/" + raw, true
	case "clashmi":
		return "clashmi://install-config?url=" + url.QueryEscape(raw), true
	case "flclashx":
		return "flclashx://install-config?url=" + url.QueryEscape(raw), true
	}
	return "", false
}

var importPage = template.Must(template.New("import").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="no-referrer"><title>Подключить {{.Name}}</title><style>body{margin:0;background:#202328;color:#edf0f4;font:16px/1.6 system-ui;min-height:100vh;display:grid;place-items:center}main{max-width:360px;padding:24px}h1{font-size:24px;font-weight:500}a{color:#77c8ec}#open{display:block;background:#207ba6;color:white;padding:12px;text-align:center;border-radius:8px;text-decoration:none}p{color:#b1bac7}code{display:block;overflow-wrap:anywhere;background:#2c3139;padding:12px;border-radius:8px;font-size:13px}small{color:#b1bac7}</style><main><h1>Добавьте подписку в {{.Name}}</h1><p>Подтвердите добавление в приложении, затем включите подключение.</p><a id="open" href="{{.Deep}}">Открыть {{.Name}}</a><p>Не открылось? Убедитесь, что приложение установлено, или добавьте ссылку вручную:</p><code>{{.Raw}}</code><small>Вернитесь в Telegram, если нужна помощь.</small></main><script>window.location.href={{.DeepText}};</script></html>`))

func (s *Server) appImport(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	app := r.PathValue("app")
	deep, ok := importDeepLink(app, raw)
	if !ok || len(raw) > 2048 {
		http.Error(w, "Ссылка не подходит", http.StatusBadRequest)
		return
	}
	names := map[string]string{"happ": "Happ", "incy": "Incy", "clashmi": "Clash Mi", "flclashx": "FlClashX"}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	_ = importPage.Execute(w, struct {
		Name          string
		Deep          template.URL
		DeepText, Raw string
	}{names[app], template.URL(deep), deep, raw})
}
