package mdhtml

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// RichLimit — предел длины rich-сообщения по документации Bot API.
const RichLimit = 32768

// richButtonRe находит кнопки в тексте: [подпись](btn:действие) и
// [подпись](btn-url:адрес).
var richButtonRe = regexp.MustCompile(`\[([^\]]+)\]\((btn(?:-url)?):([^)]+)\)`)

// Rich переводит Markdown в HTML богатого сообщения Telegram.
//
// Богатые сообщения понимают заголовки, таблицы, списки и кнопки. Это не
// обычный HTML: теги tg-spoiler, tg-button-row и table bordered striped
// compact есть только здесь. Всё незнакомое экранируется.
func Rich(source string) string {
	blocks := richBlocks(source)
	return strings.Join(blocks, "\n")
}

// RichPlain — запасной текст, если Telegram не принял rich-сообщение.
func RichPlain(source string) string {
	return Plain(Rich(source))
}

func richBlocks(source string) []string {
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
			out = append(out, "<hr/>")
			i++
		case headingRe.MatchString(trimmed):
			level := headingLevel(trimmed)
			title := headingRe.FindStringSubmatch(trimmed)[1]
			out = append(out, "<h"+level+">"+inlineMode(title, true)+"</h"+level+">")
			i++
		case strings.HasPrefix(trimmed, ">"):
			block, next := richQuoteBlock(lines, i)
			out = append(out, block)
			i = next
		case tableStart(lines, i):
			block, next := richTableBlock(lines, i)
			out = append(out, block)
			i = next
		case listRe.MatchString(lines[i]):
			block, next := richListBlock(lines, i)
			out = append(out, block)
			i = next
		default:
			block, next := richParagraph(lines, i)
			out = append(out, block)
			i = next
		}
	}
	return out
}

// headingLevel возвращает уровень заголовка строкой: "1"…"6".
func headingLevel(line string) string {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	return string(rune('0' + level))
}

// richParagraph — абзац или ряд кнопок.
//
// Строка, целиком состоящая из кнопок, становится отдельным рядом: так
// клавиатура читается как часть сообщения, а не как приписка снизу.
func richParagraph(lines []string, start int) (string, int) {
	body := make([]string, 0, 4)
	onlyButtons := true
	i := start
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		if i > start && startsBlock(lines, i) {
			break
		}
		if !isButtonLine(trimmed) {
			onlyButtons = false
		}
		body = append(body, strings.TrimRight(lines[i], " \t"))
	}

	if onlyButtons && len(body) > 0 {
		rows := make([]string, 0, len(body))
		for _, line := range body {
			if row, ok := richButtonRow(strings.TrimSpace(line)); ok {
				rows = append(rows, row)
			}
		}
		return strings.Join(rows, "\n"), i
	}

	parts := make([]string, 0, len(body))
	for _, line := range body {
		parts = append(parts, inlineMode(line, true))
	}
	return "<p>" + strings.Join(parts, "<br>") + "</p>", i
}

func isButtonLine(line string) bool {
	if !strings.Contains(line, "(btn:") && !strings.Contains(line, "(btn-url:") {
		return false
	}
	matches := richButtonRe.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return false
	}
	rest := line
	for _, match := range matches {
		rest = strings.Replace(rest, line[match[0]:match[1]], "", 1)
	}
	return strings.TrimSpace(rest) == ""
}

// richButtonRow собирает ряд кнопок из строки разметки.
func richButtonRow(line string) (string, bool) {
	matches := richButtonRe.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return "", false
	}
	var out strings.Builder
	out.WriteString("<tg-button-row>")
	for _, match := range matches {
		label := escape(match[1])
		target := match[3]
		switch match[2] {
		case "btn":
			out.WriteString(`<tg-button type="callback_data" data="` + escapeAttr(target) + `">` + label + "</tg-button>")
		case "btn-url":
			out.WriteString(`<tg-button type="url" url="` + escapeAttr(target) + `">` + label + "</tg-button>")
		}
	}
	out.WriteString("</tg-button-row>")
	return out.String(), true
}

// richQuoteBlock — цитата одной плашкой.
func richQuoteBlock(lines []string, start int) (string, int) {
	var body []string
	i := start
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, ">") {
			break
		}
		body = append(body, inlineMode(strings.TrimSpace(strings.TrimPrefix(trimmed, ">")), true))
	}
	return "<blockquote>" + strings.Join(body, "<br>") + "</blockquote>", i
}

// richTableBlock — настоящая таблица Telegram.
func richTableBlock(lines []string, start int) (string, int) {
	header := tableCells(lines[start])
	aligns := tableAligns(lines[start+1])
	width := len(header)
	if width > 20 {
		width = 20
		header = header[:width]
		aligns = aligns[:width]
	}

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

	var out strings.Builder
	out.WriteString("<table bordered striped compact>")
	for index, row := range rows {
		out.WriteString("<tr>")
		tag := "td"
		if index == 0 {
			tag = "th"
		}
		for column, cell := range row {
			align := "left"
			if column < len(aligns) && aligns[column] != "" {
				align = aligns[column]
			}
			out.WriteString("<" + tag + ` align="` + align + `">` + inlineMode(cell, true) + "</" + tag + ">")
		}
		out.WriteString("</tr>")
	}
	out.WriteString("</table>")
	return out.String(), i
}

// richListBlock — вложенный список. Telegram поддерживает ul/ol, поэтому
// отступы сохраняются структурой, а не пробелами.
func richListBlock(lines []string, start int) (string, int) {
	type level struct {
		indent int
		tag    string
	}
	var stack []level
	var out strings.Builder

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
		indent := indentWidth(lines[i])
		for len(stack) > 0 && indent < stack[len(stack)-1].indent {
			out.WriteString("</li></" + stack[len(stack)-1].tag + ">")
			stack = stack[:len(stack)-1]
		}

		marker := match[2]
		ordered := !(marker == "-" || marker == "*" || marker == "+")
		tag := "ul"
		if ordered {
			tag = "ol"
		}

		switch {
		case len(stack) == 0:
			out.WriteString("<" + tag + ">")
			stack = append(stack, level{indent: indent, tag: tag})
		case indent > stack[len(stack)-1].indent:
			out.WriteString("<" + tag + ">")
			stack = append(stack, level{indent: indent, tag: tag})
		default:
			out.WriteString("</li>")
			if stack[len(stack)-1].tag != tag {
				out.WriteString("</" + stack[len(stack)-1].tag + ">")
				stack = stack[:len(stack)-1]
				out.WriteString("<" + tag + ">")
				stack = append(stack, level{indent: indent, tag: tag})
			}
		}

		content := match[3]
		prefix := ""
		switch {
		case strings.HasPrefix(content, "[ ] "):
			prefix = `<input type="checkbox">`
			content = strings.TrimPrefix(content, "[ ] ")
		case strings.HasPrefix(strings.ToLower(content), "[x] "):
			prefix = `<input type="checkbox" checked>`
			content = content[4:]
		}
		out.WriteString("<li>" + prefix + inlineMode(content, true))
	}
	for len(stack) > 0 {
		out.WriteString("</li></" + stack[len(stack)-1].tag + ">")
		stack = stack[:len(stack)-1]
	}
	return out.String(), i
}

// RichLen — длина готового rich-сообщения в символах.
func RichLen(rendered string) int {
	return utf8.RuneCountInString(rendered)
}
