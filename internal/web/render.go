package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
)

//go:embed templates/*.html static/*
var assets embed.FS

// pageFiles перечисляет страницы. Каждая собирается отдельно вместе с
// общим каркасом, поэтому одноимённые блоки не конфликтуют.
var pageFiles = []string{
	"landing.html",
	"buy.html",
	"login.html",
	"account.html",
	"connect.html",
	"support.html",
	"ticket.html",
	"documents.html",
	"pay_result.html",
	"notice.html",
	"admin.html", "admin_campaigns.html", "admin_campaign.html",
	"admin_promos.html",
	"admin_partners.html",
	"admin_partner.html",
	"admin_tickets.html",
	"admin_audit.html",
	"admin_orders.html",
	"admin_screens.html",
	"admin_appearance.html",
	"admin_refunds.html", "admin_operations.html", "admin_settings.html", "admin_plans.html", "admin_staff.html", "admin_withdrawals.html", "admin_customer.html",
	"partner.html", "gifts.html", "history.html",
}

// feature и faqItem — блоки, которые владелец меняет в theme.json.
type feature struct {
	Icon  string `json:"icon"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type faqItem struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// Block — блок страницы. Конструктор витрины: страница собирается из
// блоков, порядок и содержание которых задаёт владелец в theme.json.
type Block struct {
	ID       string         `json:"id"`
	Class    string         `json:"class"`
	Template string         `json:"template"`
	ImageURL string         `json:"image_url"`
	ImageAlt string         `json:"image_alt"`
	Data     map[string]any `json:"data"`
	// Type — вид блока: hero, features, plans, faq, steps, cta, text.
	Type     string      `json:"type"`
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle"`
	Text     string      `json:"text"`
	Note     string      `json:"note"`
	Icon     string      `json:"icon"`
	Items    []BlockItem `json:"items"`
	Steps    []string    `json:"steps"`
	Links    []BlockLink `json:"links"`
}

// BlockItem — элемент блока: преимущество, вопрос с ответом.
type BlockItem struct {
	Icon  string `json:"icon"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Q     string `json:"q"`
	A     string `json:"a"`
}

// BlockLink — ссылка внутри блока.
type BlockLink struct {
	Label string `json:"label"`
	Href  string `json:"href"`
	// Style — вид кнопки: primary, ghost.
	Style string `json:"style"`
}

// theme — оформление и тексты витрины. Всё, что здесь есть, можно
// переопределить файлом WEB_DIR/theme.json, не трогая Go-код.
type theme struct {
	Locale          string               `json:"locale"`
	LogoURL         string               `json:"logo_url"`
	Navigation      []BlockLink          `json:"navigation"`
	Stylesheets     []string             `json:"stylesheets"`
	Scripts         []string             `json:"scripts"`
	Texts           map[string]string    `json:"texts"`
	Data            map[string]any       `json:"data"`
	Pages           map[string]pageTheme `json:"pages"`
	Brand           string               `json:"brand"`
	Tagline         string               `json:"tagline"`
	Description     string               `json:"description"`
	HeroTitle       string               `json:"hero_title"`
	HeroText        string               `json:"hero_text"`
	HeroCTA         string               `json:"hero_cta"`
	HeroNote        string               `json:"hero_note"`
	Features        []feature            `json:"features"`
	FAQ             []faqItem            `json:"faq"`
	FooterNote      string               `json:"footer_note"`
	SupportEmail    string               `json:"support_email"`
	SupportUsername string               `json:"support_username"`
	LegalEntity     string               `json:"legal_entity"`
	TermsURL        string               `json:"terms_url"`
	PrivacyURL      string               `json:"privacy_url"`
	ConnectGuideURL string               `json:"connect_guide_url"`
	TermsText       string               `json:"terms_text"`
	PrivacyText     string               `json:"privacy_text"`
	Colors          map[string]string    `json:"colors"`
	// FontCSS — адрес таблицы шрифтов (обычно Google Fonts). Подключается
	// на всех страницах.
	FontCSS string `json:"font_css"`

	// Blocks — конструктор витрины: страница собирается из блоков в этом
	// порядке. Пусто — берутся встроенные.
	Blocks []Block `json:"blocks"`

	// CSS собирается из Colors и подставляется в <style> на странице.
	CSS template.CSS `json:"-"`
}

func defaultTheme() theme {
	return theme{
		Locale:      "ru",
		Navigation:  []BlockLink{{Label: "Тарифы", Href: "/buy"}, {Label: "Подключение", Href: "/connect"}, {Label: "Поддержка", Href: "/support"}},
		Brand:       "Vivid",
		Tagline:     "Доступ без границ",
		Description: "Быстрый и приватный доступ к интернету. Оплата картой, подключение за пару минут.",
		HeroTitle:   "Интернет, который не мешает жить",
		HeroText: "Стабильное соединение на телефоне и компьютере, понятные тарифы " +
			"и поддержка, которая отвечает по-человечески.",
		HeroCTA:  "Выбрать тариф",
		HeroNote: "Оплата картой или через СБП. Подключение сразу после оплаты.",
		Features: []feature{
			{Icon: "⚡", Title: "Скорость без просадок", Text: "Серверы держат нагрузку в часы пик: видео, звонки и загрузки идут ровно."},
			{Icon: "🔒", Title: "Приватность по умолчанию", Text: "Мы не ведём журналы посещений и не передаём данные третьим лицам."},
			{Icon: "📱", Title: "Одна подписка на всё", Text: "Телефон, ноутбук и планшет подключаются по одной ссылке. Устройства можно докупать."},
			{Icon: "💬", Title: "Живая поддержка", Text: "Пишите в бот или на сайт — отвечает команда, а не бесконечный автоответчик."},
		},
		FAQ: []faqItem{
			{Q: "Как подключиться?", A: "После оплаты в кабинете появится ссылка на подписку. Откройте её в приложении HAPP, Streisand или v2rayNG — инструкция есть на странице «Подключение»."},
			{Q: "Можно вернуть деньги?", A: "Если доступ не заработал, напишите в поддержку в течение суток после оплаты: разберёмся или оформим возврат."},
			{Q: "Сколько устройств можно подключить?", A: "База входит в тариф, дополнительные слоты докупаются на срок подписки. Пакеты устройств видны в кабинете."},
			{Q: "Что если оплата прошла, а доступ не появился?", A: "Откройте кабинет: выдача обычно занимает несколько секунд. Если статус заказа всё ещё «ждёт», нажмите «Проверить» или напишите в поддержку."},
		},
		FooterNote: "Сервис для доступа к открытым ресурсам. Используйте с соблюдением законодательства вашей страны.",
		Colors: map[string]string{
			"bg":          "#0a0d16",
			"surface":     "#111624",
			"surface-alt": "#1a2033",
			"text":        "#eef1ff",
			"muted":       "#96a0b5",
			"accent":      "#6d5efc",
			"accent-soft": "#8f84ff",
			"accent-text": "#ffffff",
			"border":      "#242c42",
			"success":     "#35c77b",
			"danger":      "#ff6b6b",
			"radius":      "18px",
			"max-width":   "1120px",
			"font":        "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
		},
	}
}

func loadTheme(cfg *config.Settings) (theme, error) {
	th := defaultTheme()
	if dir := strings.TrimSpace(cfg.WebDir); dir != "" {
		path := filepath.Join(dir, "theme.json")
		if data, err := os.ReadFile(path); err == nil {
			// Разбор идёт прямо в тему: отсутствующие ключи остаются
			// встроенными, а цвета мержатся в существующую карту.
			if err := json.Unmarshal(data, &th); err != nil {
				return th, fmt.Errorf("тема %s: %w", path, err)
			}
		} else if !os.IsNotExist(err) {
			return th, fmt.Errorf("тема %s: %w", path, err)
		}
	}
	// Настройки из окружения главнее файла темы: адрес поддержки и
	// юридические ссылки — это не оформление, а факты о сервисе.
	if cfg.BrandName != "" {
		th.Brand = cfg.BrandName
	}
	if cfg.LegalEntity != "" {
		th.LegalEntity = cfg.LegalEntity
	}
	if cfg.SupportEmail != "" {
		th.SupportEmail = cfg.SupportEmail
	}
	if cfg.SupportUser != "" {
		th.SupportUsername = cfg.SupportUser
	}
	if cfg.TermsURL != "" {
		th.TermsURL = cfg.TermsURL
	}
	if cfg.PrivacyURL != "" {
		th.PrivacyURL = cfg.PrivacyURL
	}
	if cfg.ConnectGuideURL != "" {
		th.ConnectGuideURL = cfg.ConnectGuideURL
	}
	// Блоки витрины: если владелец задал свои, они главнее; иначе собираем
	// встроенные — уже с учётом переопределений из окружения.
	if len(th.Blocks) == 0 {
		th.Blocks = buildBlocks(th)
	}
	th.CSS = cssVariables(th.Colors)
	return th, nil
}

// buildBlocks собирает витрину по умолчанию.
func buildBlocks(th theme) []Block {
	items := make([]BlockItem, 0, len(th.Features))
	for _, item := range th.Features {
		items = append(items, BlockItem{Icon: item.Icon, Title: item.Title, Text: item.Text})
	}
	questions := make([]BlockItem, 0, len(th.FAQ))
	for _, item := range th.FAQ {
		questions = append(questions, BlockItem{Q: item.Q, A: item.A})
	}
	return []Block{
		{
			Type: "hero", Title: th.HeroTitle, Text: th.HeroText, Note: th.HeroNote,
			Icon:  th.Tagline,
			Links: []BlockLink{{Label: th.HeroCTA, Href: "/buy", Style: "primary"}},
		},
		{
			Type: "features", Title: "Почему сюда возвращаются",
			Subtitle: "Никаких обещаний «самого быстрого интернета» — просто рабочие вещи.",
			Items:    items,
		},
		{
			Type: "plans", Title: "Тарифы",
			Subtitle: "Цена за весь срок. Чем дольше — тем дешевле месяц.",
		},
		{
			Type: "faq", Title: "Частые вопросы", Items: questions,
		},
		{
			Type: "cta", Title: "Готовы подключиться?",
			Text:  "Оплата занимает минуту, доступ выдаётся автоматически.",
			Links: []BlockLink{{Label: "Выбрать тариф", Href: "/buy", Style: "primary"}},
		},
	}
}

func cssVariables(colors map[string]string) template.CSS {
	keys := make([]string, 0, len(colors))
	for key := range colors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var buf strings.Builder
	for _, key := range keys {
		value := sanitizeCSS(colors[key])
		if key == "" || value == "" {
			continue
		}
		buf.WriteString("--")
		buf.WriteString(key)
		buf.WriteString(":")
		buf.WriteString(value)
		buf.WriteString(";")
	}
	return template.CSS(buf.String())
}

func sanitizeCSS(value string) string {
	replacer := strings.NewReplacer("<", "", ">", "", "{", "", "}", "")
	return strings.TrimSpace(replacer.Replace(value))
}

// ---------------------------------------------------------------------------
// Шаблоны
// ---------------------------------------------------------------------------

type renderer struct {
	theme  theme
	pages  map[string]*template.Template
	webDir string
}

func newRenderer(cfg *config.Settings) (*renderer, error) {
	th, err := loadTheme(cfg)
	if err != nil {
		return nil, err
	}
	r := &renderer{theme: th, pages: map[string]*template.Template{}, webDir: strings.TrimSpace(cfg.WebDir)}
	for _, page := range pageFiles {
		t, err := r.build(page)
		if err != nil {
			return nil, err
		}
		r.pages[page] = t
	}
	if err := r.buildCustomPages(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *renderer) build(page string) (*template.Template, error) {
	settings := r.theme.Pages[strings.TrimSuffix(page, ".html")]
	layout, source := "layout.html", page
	if strings.HasPrefix(page, "admin") {
		layout = "admin_layout.html"
	}
	if settings.Layout != "" {
		layout = settings.Layout
	}
	if settings.Template != "" {
		source = settings.Template
	}
	var t *template.Template
	funcs := templateFuncs()
	funcs["text"] = func(key string, fallback ...string) string {
		if value, ok := settings.Texts[key]; ok {
			return value
		}
		return r.text(key, fallback...)
	}
	funcs["dict"] = templateDict
	funcs["include"] = func(name string, data any) (template.HTML, error) {
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, name, data); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil
	}
	t = template.New("root").Funcs(funcs)
	names := []string{layout, "partials.html", "screen_lab.html"}
	components, err := r.componentFiles()
	if err != nil {
		return nil, err
	}
	names = append(names, components...)
	names = append(names, source)
	for _, name := range names {
		if !fs.ValidPath(name) || !strings.HasSuffix(name, ".html") {
			return nil, fmt.Errorf("неверный путь шаблона %q", name)
		}
		data, err := r.read("templates/" + name)
		if err != nil {
			return nil, fmt.Errorf("шаблон %s: %w", name, err)
		}
		if _, err := t.Parse(string(data)); err != nil {
			return nil, fmt.Errorf("шаблон %s: %w", name, err)
		}
	}
	blocks := settings.Blocks
	if page == "landing.html" && blocks == nil {
		blocks = r.theme.Blocks
	}
	for _, block := range blocks {
		if block.Template != "" && t.Lookup(block.Template) == nil {
			return nil, fmt.Errorf("нет компонента %q", block.Template)
		}
	}
	if t.Lookup("layout") == nil || t.Lookup("content") == nil {
		return nil, fmt.Errorf("шаблон %s должен определять layout и content", page)
	}
	return t, nil
}

// read сначала смотрит файл в WEB_DIR, затем во встроенных ресурсах. Так
// владелец может заменить одну страницу, не копируя остальные.
func (r *renderer) read(name string) ([]byte, error) {
	if r.webDir != "" {
		path := filepath.Join(r.webDir, filepath.FromSlash(name))
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return assets.ReadFile(name)
}

func (r *renderer) render(w http.ResponseWriter, status int, page string, data pageData) error {
	t, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("нет шаблона %q", page)
	}
	data.View = strings.TrimSuffix(page, ".html")
	data.Page = r.theme.Pages[data.View]
	if data.Page.Blocks != nil {
		data.Site.Blocks = data.Page.Blocks
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

func (r *renderer) staticHandler() http.Handler {
	embedded, _ := fs.Sub(assets, "static")
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/static/")
		if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
			http.NotFound(w, req)
			return
		}
		if r.webDir != "" {
			path := filepath.Join(r.webDir, "static", filepath.FromSlash(name))
			if file, err := os.Open(path); err == nil {
				defer file.Close()
				if info, err := file.Stat(); err == nil && !info.IsDir() {
					http.ServeContent(w, req, info.Name(), info.ModTime(), file)
					return
				}
			}
		}
		data, err := fs.ReadFile(embedded, name)
		if err != nil {
			http.NotFound(w, req)
			return
		}
		http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(data))
	})
}

// ---------------------------------------------------------------------------
// Данные страницы
// ---------------------------------------------------------------------------

type pageMeta struct {
	Title       string
	Description string
}

type pageData struct {
	View    string
	Page    pageTheme
	Site    theme
	Meta    pageMeta
	Path    string
	Year    int
	Subject *domain.Subject
	Access  *domain.Access
	Flash   *flashMessage
	CSRF    string
	BotURL  string
	SiteURL string
	Now     time.Time
	Data    any
	Staff   adminSession
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page string, status int, meta pageMeta, data any) {
	settings := s.render.theme.Pages[strings.TrimSuffix(page, ".html")]
	if settings.Title != "" {
		meta.Title = settings.Title
	}
	if settings.Description != "" {
		meta.Description = settings.Description
	}
	currentTheme := s.render.theme
	if s.cfg.Current() != s.cfg && s.cfg.Current().BrandName != "" {
		currentTheme.Brand = s.cfg.Current().BrandName
	}
	if s.cfg.Current() != s.cfg {
		currentTheme.SupportEmail = s.cfg.Current().SupportEmail
	}
	year := time.Now().Year()
	if meta.Title == "" {
		meta.Title = currentTheme.Brand
	} else {
		meta.Title = meta.Title + " — " + currentTheme.Brand
	}
	if meta.Description == "" {
		meta.Description = s.render.theme.Description
	}
	pageData := pageData{
		Site:    currentTheme,
		Meta:    meta,
		Path:    r.URL.Path,
		Year:    year,
		Subject: s.subjectFrom(r),
		Flash:   s.takeFlash(w, r),
		CSRF:    s.csrfToken(w, r),
		BotURL:  s.botURL(),
		SiteURL: s.siteURL(r),
		Now:     time.Now().UTC(),
		Data:    data,
	}
	if strings.HasPrefix(page, "admin") {
		pageData.Staff = s.staffAccess(r.Context(), pageData.Subject)
	}
	if err := s.render.render(w, status, page, pageData); err != nil {
		s.log.Error("не удалось отрисовать страницу", "page", page, "err", err)
		http.Error(w, "Внутренняя ошибка", http.StatusInternalServerError)
	}
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	s.renderPage(w, r, "notice.html", status, pageMeta{Title: title},
		noticeData{Title: title, Text: text})
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if s.customPage(w, r) {
		return
	}
	s.renderError(w, r, http.StatusNotFound, "Страница не найдена",
		"Возможно, ссылка устарела. Начните с главной — там тарифы и вход в кабинет.")
}

// ---------------------------------------------------------------------------
// Помощники шаблонов
// ---------------------------------------------------------------------------

// markdownHTML рисует текст переписки как Markdown.
//
// template.HTML здесь осознанно: mdhtml экранирует всё, что не разметка, и
// пропускает только известные теги. Без этого в обращениях было бы видно
// «**важно**» вместо выделения, а ответы помощника приходили бы с
// решётками и звёздочками.
func markdownHTML(text string) template.HTML {
	return template.HTML(mdhtml.Render(text))
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"join": func(v []string) string { return strings.Join(v, ",") },
		"scopeLabel": func(s string) string {
			return map[string]string{"free": "Свободные", "mine": "Мои", "waiting": "Ждут ответа", "open": "Открытые", "closed": "Закрытые"}[s]
		},
		"campaignAudience": campaignAudienceLabel,
		"progress":         func(o domain.Order) domain.OrderProgress { return o.Progress(time.Now()) },
		"money":            moneyText,
		"date":             dateText,
		"datetime":         datetimeText,
		"bytes":            bytesText,
		"pct":              percent,
		"daysWord":         daysWord,
		"status":           orderStatus,
		"tone":             orderStatusTone,
		"kind":             orderKind,
		"paragraphs":       paragraphs,
		"markdown":         markdownHTML,
		"add":              func(a, b int) int { return a + b },
		"str": func(value *string) string {
			if value == nil {
				return ""
			}
			return *value
		},
		"num": func(value *int) int {
			if value == nil {
				return 0
			}
			return *value
		},
		"i64": func(value *int64) int64 {
			if value == nil {
				return 0
			}
			return *value
		},
		"monthly":  monthlyPrice,
		"perMonth": perMonth,
		"initials": initials,
		"reward": func(rewardType string, rewardValue domain.Money) string {
			return domain.RewardValueText(rewardType, rewardValue)
		},
		"ts": func(value time.Time) string {
			return datetimeText(&value)
		},
		"firstLines": func(text string, limit int) string {
			return firstLines(text, limit)
		},
	}
}

// firstLines — короткий срез текста для таблиц: сообщение целиком не нужно.
func firstLines(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}

func moneyText(amount domain.Money) string {
	negative := amount < 0
	value := int64(amount)
	if negative {
		value = -value
	}
	whole := value / 100
	cents := value % 100
	digits := fmt.Sprintf("%d", whole)
	var parts []string
	for len(digits) > 3 {
		parts = append([]string{digits[len(digits)-3:]}, parts...)
		digits = digits[:len(digits)-3]
	}
	parts = append([]string{digits}, parts...)
	text := strings.Join(parts, "\u2009")
	if cents != 0 {
		text += fmt.Sprintf(",%02d", cents)
	}
	if negative {
		text = "−" + text
	}
	return text + "\u00a0₽"
}

func dateText(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "—"
	}
	return value.In(time.Local).Format("02.01.2006")
}

func datetimeText(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "—"
	}
	return value.In(time.Local).Format("02.01.2006 15:04")
}

func bytesText(value *int64) string {
	if value == nil || *value <= 0 {
		return "без ограничения"
	}
	units := []string{"Б", "КБ", "МБ", "ГБ", "ТБ"}
	size := float64(*value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func percent(used int64, limit *int64) int {
	if limit == nil || *limit <= 0 {
		return 0
	}
	value := int(used * 100 / *limit)
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func daysWord(days int) string {
	mod100 := days % 100
	if mod100 >= 11 && mod100 <= 14 {
		return "дней"
	}
	switch days % 10 {
	case 1:
		return "день"
	case 2, 3, 4:
		return "дня"
	default:
		return "дней"
	}
}

func orderStatus(status string) string {
	switch status {
	case domain.OrderPending:
		return "Ждёт оплаты"
	case domain.OrderPaid:
		return "Оплачен, выдаём"
	case domain.OrderProvisioned:
		return "Готово"
	case domain.OrderFailed:
		return "Не прошёл"
	case domain.OrderCancelled:
		return "Отменён"
	case domain.OrderRefunded:
		return "Возврат"
	case "trial_granted":
		return "Пробный период"
	default:
		return status
	}
}

func orderKind(kind string) string {
	switch kind {
	case domain.KindSubscription:
		return "Подписка"
	case domain.KindDevices:
		return "Устройства"
	case domain.KindGift:
		return "Подарок"
	default:
		return kind
	}
}

func paragraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	chunks := strings.Split(text, "\n\n")
	out := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if trimmed := strings.TrimSpace(chunk); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func monthlyPrice(plan domain.Plan) domain.Money {
	if plan.Days <= 0 {
		return plan.Price
	}
	return domain.Money(int64(plan.Price) * 30 / int64(plan.Days))
}

func perMonth(amount domain.Money, days int) domain.Money {
	if days <= 0 {
		return amount
	}
	return domain.Money(int64(amount) * 30 / int64(days))
}

func initials(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	if len(fields) == 0 {
		return "?"
	}
	out := []rune(fields[0])
	if len(out) > 1 {
		return string(out[:1])
	}
	return string(out)
}
