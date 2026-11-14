// Package mdhtml переводит Markdown в HTML, который понимает Telegram.
//
// Это не полный CommonMark, а та разметка, которую люди действительно
// пишут: ответы помощника, сообщения поддержки, пояснения в базе знаний.
// Всё, что не разобрано как разметка, остаётся текстом и экранируется:
// присланный «<script>» не станет тегом, а непарная звёздочка не сорвёт
// доставку сообщения.
package mdhtml

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// DefaultLimit — предел длины одного сообщения с запасом.
//
// Telegram принимает 4096 символов, но считает их по-своему (UTF-16), а
// разметка добавляет теги. Запас убирает отказы на границе.
const DefaultLimit = 3500

// divider — горизонтальная черта. Тега <hr> в Telegram нет, а строка из
// черт читается как разделитель и не ломает разметку.
const divider = "──────────"

// indent — отступ вложенного пункта списка. Обычные пробелы Telegram
// схлопывает, поэтому отступ неразрывный.
const indent = "\u00a0\u00a0"

var (
	headingRe = regexp.MustCompile(`^#{1,6}\s+(.+)$`)
	listRe    = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$`)
	fenceRe   = regexp.MustCompile("^(`{3,}|~{3,})(.*)$")
)

// Render собирает сообщение из блоков Markdown.
func Render(source string) string {
	blocks := renderBlocks(source)
	return strings.Join(blocks, "\n\n")
}

// Chunks делит текст на части, каждая из которых влезает в сообщение.
//
// Делим исходник, а не готовый HTML: разрез посреди <b> или <pre> прислал
// бы Telegram битую разметку. Пустой текст — ни одной части.
func Chunks(source string, limit int) []string {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if strings.TrimSpace(source) == "" {
		return nil
	}
	if rendered := Render(source); utf8.RuneCountInString(rendered) <= limit {
		return []string{rendered}
	}

	parts := splitBlocks(source)
	var out []string
	var current []string

	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, Render(strings.Join(current, "\n\n")))
		current = nil
	}

	for _, part := range parts {
		candidate := append(append([]string{}, current...), part)
		if len(current) > 0 && utf8.RuneCountInString(Render(strings.Join(candidate, "\n\n"))) > limit {
			flush()
			candidate = []string{part}
		}
		if utf8.RuneCountInString(Render(part)) <= limit {
			current = candidate
			continue
		}
		flush()
		out = append(out, splitLarge(part, limit)...)
	}
	flush()
	return out
}

// Plain оставляет от HTML только текст.
//
// Нужен как запасной путь: если Telegram всё-таки отказался разбирать
// разметку, сообщение уходит без неё, но доходит.
func Plain(markup string) string {
	var out strings.Builder
	inTag := false
	for _, r := range markup {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(html.UnescapeString(out.String()))
}

// ---------------------------------------------------------------------------
// Блоки
// ---------------------------------------------------------------------------

func renderBlocks(source string) []string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	lines := strings.Split(source, "\n")

	out := make([]string, 0, len(lines)/2)
	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		switch {
		case trimmed == "":
			i++
		case fenceRe.MatchString(trimmed):
			block, next := codeBlock(lines, i)
			out = append(out, block)
			i = next
		case isDivider(trimmed):
			out = append(out, divider)
			i++
		case headingRe.MatchString(trimmed):
			out = append(out, "<b>"+inline(headingRe.FindStringSubmatch(trimmed)[1])+"</b>")
			i++
		case strings.HasPrefix(trimmed, ">"):
			block, next := quoteBlock(lines, i)
			out = append(out, block)
			i = next
		case tableStart(lines, i):
			block, next := tableBlock(lines, i)
			out = append(out, block)
			i = next
		case listRe.MatchString(lines[i]):
			block, next := listBlock(lines, i)
			out = append(out, block)
			i = next
		default:
			block, next := paragraph(lines, i)
			out = append(out, block)
			i = next
		}
	}
	return out
}

// codeBlock — блок кода. Язык идёт в class: Telegram подсвечивает код по
// нему.
func codeBlock(lines []string, start int) (string, int) {
	match := fenceRe.FindStringSubmatch(strings.TrimSpace(lines[start]))
	marker := match[1]
	language := languageClass(match[2])

	var body []string
	i := start + 1
	for ; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), marker) {
			i++
			break
		}
		body = append(body, lines[i])
	}
	code := escape(strings.Join(body, "\n"))
	if language != "" {
		return `<pre><code class="language-` + language + `">` + code + "</code></pre>", i
	}
	return "<pre>" + code + "</pre>", i
}

// quoteBlock — цитата: Telegram рисует её отдельной плашкой.
func quoteBlock(lines []string, start int) (string, int) {
	var body []string
	i := start
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, ">") {
			break
		}
		body = append(body, inline(strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))))
	}
	return "<blockquote>" + strings.Join(body, "\n") + "</blockquote>", i
}

// listBlock — список. Своих тегов у Telegram нет: пункты остаются
// строками, но с маркером и отступом, поэтому список читается глазами.
func listBlock(lines []string, start int) (string, int) {
	var out []string
	base := indentWidth(lines[start])
	i := start
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		match := listRe.FindStringSubmatch(lines[i])
		if match == nil {
			break
		}
		level := indentWidth(lines[i]) - base
		if level < 0 {
			level = 0
		}
		marker := match[2]
		text := match[3]

		prefix := strings.Repeat(indent, level/2)
		switch {
		case strings.HasPrefix(text, "[ ] "):
			prefix += "☐ "
			text = strings.TrimPrefix(text, "[ ] ")
		case strings.HasPrefix(strings.ToLower(text), "[x] "):
			prefix += "☑ "
			text = text[4:]
		case marker == "-" || marker == "*" || marker == "+":
			if level >= 2 {
				prefix += "◦ "
			} else {
				prefix += "• "
			}
		default:
			prefix += marker + " "
		}
		out = append(out, prefix+inline(text))
	}
	return strings.Join(out, "\n"), i
}

// tableBlock — таблица в моноширинном блоке.
//
// Таблицы Telegram не умеет, а ответы помощника ими пользуются: ровные
// столбцы в <pre> читаются лучше, чем слипшиеся через пробел слова.
func tableBlock(lines []string, start int) (string, int) {
	header := tableCells(lines[start])
	aligns := tableAligns(lines[start+1])
	width := len(header)

	rows := [][]string{header}
	i := start + 2
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.Contains(trimmed, "|") {
			break
		}
		cells := tableCells(trimmed)
		for len(cells) < width {
			cells = append(cells, "")
		}
		rows = append(rows, cells[:width])
	}

	widths := make([]int, width)
	for _, row := range rows {
		for column, cell := range row {
			if size := utf8.RuneCountInString(cell); size > widths[column] {
				widths[column] = size
			}
		}
	}
	for column := range widths {
		if widths[column] > 28 {
			widths[column] = 28
		}
	}

	var out []string
	for index, row := range rows {
		cells := make([]string, width)
		for column := range cells {
			cells[column] = pad(truncate(row[column], widths[column]), widths[column], aligns[column])
		}
		out = append(out, strings.Join(cells, " │ "))
		if index == 0 {
			line := make([]string, width)
			for column := range line {
				line[column] = strings.Repeat("─", widths[column])
			}
			out = append(out, strings.Join(line, "─┼─"))
		}
	}
	return "<pre>" + escape(strings.Join(out, "\n")) + "</pre>", i
}

func paragraph(lines []string, start int) (string, int) {
	body := make([]string, 0, 4)
	i := start
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		if i > start && startsBlock(lines, i) {
			break
		}
		body = append(body, inline(strings.TrimRight(lines[i], " \t")))
	}
	return strings.Join(body, "\n"), i
}

// startsBlock — начинается ли на строке новый блок: тогда абзац пора
// закончить.
func startsBlock(lines []string, index int) bool {
	trimmed := strings.TrimSpace(lines[index])
	switch {
	case trimmed == "":
		return true
	case fenceRe.MatchString(trimmed), isDivider(trimmed), strings.HasPrefix(trimmed, ">"):
		return true
	case headingRe.MatchString(trimmed), listRe.MatchString(lines[index]):
		return true
	case tableStart(lines, index):
		return true
	}
	return false
}

func isDivider(text string) bool {
	if len(text) < 3 {
		return false
	}
	symbol := text[0]
	if symbol != '-' && symbol != '*' && symbol != '_' {
		return false
	}
	for i := 1; i < len(text); i++ {
		if text[i] != symbol {
			return false
		}
	}
	return true
}

func tableStart(lines []string, index int) bool {
	if index+1 >= len(lines) {
		return false
	}
	if !strings.Contains(lines[index], "|") || !strings.Contains(lines[index+1], "|") {
		return false
	}
	separator := strings.TrimSpace(lines[index+1])
	for _, r := range separator {
		if r != '|' && r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return strings.Contains(separator, "-")
}

func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func tableAligns(separator string) []string {
	cells := tableCells(separator)
	aligns := make([]string, len(cells))
	for index, cell := range cells {
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			aligns[index] = "center"
		case right:
			aligns[index] = "right"
		default:
			aligns[index] = "left"
		}
	}
	return aligns
}

func pad(text string, width int, align string) string {
	gap := width - utf8.RuneCountInString(text)
	if gap <= 0 {
		return text
	}
	switch align {
	case "right":
		return strings.Repeat(" ", gap) + text
	case "center":
		left := gap / 2
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", gap-left)
	default:
		return text + strings.Repeat(" ", gap)
	}
}

func truncate(text string, width int) string {
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	runes := []rune(text)
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func indentWidth(line string) int {
	width := 0
	for _, r := range line {
		switch r {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width
		}
	}
	return width
}

func languageClass(language string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(language) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '+', r == '#', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		}
	}
	return out.String()
}

// ---------------------------------------------------------------------------
// Строчная разметка
// ---------------------------------------------------------------------------

func inline(text string) string { return inlineMode(text, false) }

// inlineMode собирает строчную разметку. rich включает теги, которых нет в
// обычном HTML Telegram: mark и tg-spoiler, а также кнопки в тексте.
func inlineMode(text string, rich bool) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		rest := text[i:]
		switch {
		case rest[0] == '\\' && len(rest) > 1:
			out.WriteString(escape(rest[1:2]))
			i += 2
		case rest[0] == '`':
			if end := strings.IndexByte(rest[1:], '`'); end >= 0 {
				out.WriteString("<code>" + escape(rest[1:1+end]) + "</code>")
				i += end + 2
				continue
			}
			out.WriteByte('`')
			i++
		case strings.HasPrefix(rest, "***"):
			inner, size, ok := wrapped(rest, "***")
			if !ok {
				out.WriteString("***")
				i += 3
				continue
			}
			out.WriteString("<b><i>" + inlineMode(inner, rich) + "</i></b>")
			i += size
		case strings.HasPrefix(rest, "**"), strings.HasPrefix(rest, "__"):
			delimiter := rest[:2]
			inner, size, ok := wrapped(rest, delimiter)
			if !ok {
				out.WriteString(delimiter)
				i += 2
				continue
			}
			out.WriteString("<b>" + inlineMode(inner, rich) + "</b>")
			i += size
		case strings.HasPrefix(rest, "~~"):
			inner, size, ok := wrapped(rest, "~~")
			if !ok {
				out.WriteString("~~")
				i += 2
				continue
			}
			out.WriteString("<s>" + inlineMode(inner, rich) + "</s>")
			i += size
		case strings.HasPrefix(rest, "||"):
			inner, size, ok := wrapped(rest, "||")
			if !ok {
				out.WriteString("||")
				i += 2
				continue
			}
			if rich {
				out.WriteString("<tg-spoiler>" + inlineMode(inner, rich) + "</tg-spoiler>")
			} else {
				out.WriteString(`<span class="tg-spoiler">` + inlineMode(inner, rich) + "</span>")
			}
			i += size
		case rich && strings.HasPrefix(rest, "=="):
			inner, size, ok := wrapped(rest, "==")
			if !ok {
				out.WriteString("==")
				i += 2
				continue
			}
			out.WriteString("<mark>" + inlineMode(inner, rich) + "</mark>")
			i += size
		case rest[0] == '*':
			inner, size, ok := wrapped(rest, "*")
			if !ok {
				out.WriteByte('*')
				i++
				continue
			}
			out.WriteString("<i>" + inlineMode(inner, rich) + "</i>")
			i += size
		case rest[0] == '_':
			inner, size, ok := wrapped(rest, "_")
			if !ok || insideWord(text, i) {
				out.WriteByte('_')
				i++
				continue
			}
			out.WriteString("<i>" + inlineMode(inner, rich) + "</i>")
			i += size
		case rest[0] == '!' && strings.HasPrefix(rest, "!["):
			if label, target, size, ok := link(rest[1:]); ok {
				out.WriteString(`<a href="` + escapeAttr(target) + `">` + inlineMode(label, rich) + "</a>")
				i += size + 1
				continue
			}
			out.WriteByte('!')
			i++
		case rest[0] == '[':
			if label, target, size, ok := link(rest); ok {
				switch {
				case strings.HasPrefix(target, "btn:"):
					data := strings.TrimPrefix(target, "btn:")
					if rich {
						out.WriteString(`<tg-button type="callback_data" data="` + escapeAttr(data) + `">` + escape(label) + "</tg-button>")
					} else {
						out.WriteString("<b>" + escape(label) + "</b>")
					}
				case strings.HasPrefix(target, "btn-url:"):
					target = strings.TrimPrefix(target, "btn-url:")
					if rich {
						out.WriteString(`<tg-button type="url" url="` + escapeAttr(target) + `">` + escape(label) + "</tg-button>")
					} else {
						out.WriteString(`<a href="` + escapeAttr(target) + `">` + inlineMode(label, rich) + "</a>")
					}
				default:
					out.WriteString(`<a href="` + escapeAttr(target) + `">` + inlineMode(label, rich) + "</a>")
				}
				i += size
				continue
			}
			out.WriteByte('[')
			i++
		default:
			out.WriteString(escape(rest[:1]))
			i++
		}
	}
	return out.String()
}

// wrapped находит парную звёздочку или подчёркивание.
//
// Пустая пара и пара с пробелом по краям — не выделение: «2 * 3 * 4» и
// «snake_case» должны остаться текстом.
func wrapped(rest, delimiter string) (string, int, bool) {
	body := rest[len(delimiter):]
	end := strings.Index(body, delimiter)
	if end <= 0 {
		return "", 0, false
	}
	inner := body[:end]
	if strings.TrimSpace(inner) == "" || isSpace(inner[0]) || isSpace(inner[len(inner)-1]) {
		return "", 0, false
	}
	return inner, len(delimiter)*2 + end, true
}

// insideWord не даёт подчёркиванию съесть snake_case.
func insideWord(text string, index int) bool {
	if index == 0 {
		return false
	}
	previous := text[index-1]
	return previous >= 'a' && previous <= 'z' || previous >= 'A' && previous <= 'Z' ||
		previous >= '0' && previous <= '9' || previous == '_'
}

// link разбирает [подпись](адрес).
func link(text string) (string, string, int, bool) {
	if !strings.HasPrefix(text, "[") {
		return "", "", 0, false
	}
	end := strings.Index(text, "](")
	if end <= 1 {
		return "", "", 0, false
	}
	label := text[1:end]
	after := text[end+2:]
	close := strings.IndexByte(after, ')')
	if close < 0 {
		return "", "", 0, false
	}
	target := strings.TrimSpace(after[:close])
	if !safeTarget(target) {
		return "", "", 0, false
	}
	return label, target, end + 2 + close + 1, true
}

// safeTarget разрешает и кнопки в тексте: их адрес — не ссылка, а действие.
func safeTarget(target string) bool {
	if strings.HasPrefix(target, "btn:") || strings.HasPrefix(target, "btn-url:") {
		return true
	}
	return safeURL(target)
}

func safeURL(target string) bool {
	lower := strings.ToLower(target)
	for _, prefix := range []string{"http://", "https://", "tg://", "mailto:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// ---------------------------------------------------------------------------
// Разбиение на части
// ---------------------------------------------------------------------------

// splitBlocks режет исходник по границам блоков: абзац, список, цитата и
// блок кода остаются целыми.
func splitBlocks(source string) []string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	lines := strings.Split(source, "\n")

	var blocks []string
	for i := 0; i < len(lines); {
		next := blockEnd(lines, i)
		if next <= i {
			next = i + 1
		}
		blocks = append(blocks, strings.TrimRight(strings.Join(lines[i:next], "\n"), "\n"))
		i = next
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
	}
	return blocks
}

// blockEnd — строка сразу за блоком, который начинается с index.
func blockEnd(lines []string, index int) int {
	trimmed := strings.TrimSpace(lines[index])
	switch {
	case trimmed == "":
		return index + 1
	case fenceRe.MatchString(trimmed):
		_, next := codeBlock(lines, index)
		return next
	case isDivider(trimmed):
		return index + 1
	case headingRe.MatchString(trimmed):
		return index + 1
	case strings.HasPrefix(trimmed, ">"):
		_, next := quoteBlock(lines, index)
		return next
	case tableStart(lines, index):
		_, next := tableBlock(lines, index)
		return next
	case listRe.MatchString(lines[index]):
		_, next := listBlock(lines, index)
		return next
	default:
		_, next := paragraph(lines, index)
		return next
	}
}

// splitLarge режет слишком длинный блок по строкам, а неразрезаемую
// строку — по символам. Такое бывает с ответом модели без переносов.
func splitLarge(block string, limit int) []string {
	lines := strings.Split(block, "\n")
	var out []string
	var current []string

	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, Render(strings.Join(current, "\n")))
		current = nil
	}

	for _, line := range lines {
		candidate := append(append([]string{}, current...), line)
		if len(current) > 0 && utf8.RuneCountInString(Render(strings.Join(candidate, "\n"))) > limit {
			flush()
		}
		if utf8.RuneCountInString(line) <= limit {
			current = append(current, line)
			continue
		}
		flush()
		for _, piece := range runePieces(line, limit/2) {
			out = append(out, Render(piece))
		}
	}
	flush()
	return out
}

func runePieces(text string, size int) []string {
	if size <= 0 {
		size = 1
	}
	runes := []rune(text)
	var out []string
	for len(runes) > size {
		out = append(out, string(runes[:size]))
		runes = runes[size:]
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}

// ---------------------------------------------------------------------------
// Экранирование
// ---------------------------------------------------------------------------

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

var attrEscaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
)

func escape(text string) string { return textEscaper.Replace(text) }

func escapeAttr(text string) string { return attrEscaper.Replace(text) }
