package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

// happBridge — страница-мостик к приложению Happ.
//
// Telegram не принимает happ:// в кнопках-ссылках, а на Windows такой
// deep link режет ещё и система. Поэтому кнопка ведёт на обычный https-
// адрес, а уже он открывает приложение. Ссылка на подписку проверяется:
// мост не должен превращаться в открытый редирект на чужие адреса.
var happPage = template.Must(template.New("happ").Parse(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Открываем Happ</title>
<style>
  body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0a0d16;color:#eef1ff;font:16px/1.5 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif}
  main{max-width:26rem;padding:2rem;text-align:center}
  a{display:inline-block;margin-top:1rem;padding:.9rem 1.4rem;border-radius:12px;background:#6d5efc;color:#fff;text-decoration:none;font-weight:600}
  p{color:#96a0b5}
</style>
</head>
<body>
<main>
  <h1>Открываем Happ</h1>
  <p>Если приложение не запустилось само, нажмите кнопку ниже.</p>
  <a id="open" href="{{.Deep}}">Открыть в Happ</a>
</main>
<script>
  document.getElementById('open').addEventListener('click', function (event) {
    event.preventDefault();
    window.location.href = {{.Deep}};
  });
  window.location.href = {{.Deep}};
</script>
</body>
</html>
`))

func (s *Server) happBridge(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if !isHTTPURL(raw) {
		s.renderError(w, r, http.StatusBadRequest, "Ссылка не подходит",
			"Откройте страницу подключения в кабинете и попробуйте ещё раз.")
		return
	}
	// Страница живёт вне обычной вёрстки: ей нужен собственный скрипт,
	// поэтому общий CSP ослабляется только здесь.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	data := struct{ Deep string }{Deep: "happ://add/" + raw}
	if err := happPage.Execute(w, data); err != nil {
		s.log.Warn("не удалось отрисовать мост Happ", "err", err)
	}
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
