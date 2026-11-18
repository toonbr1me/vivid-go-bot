package bot

import (
	"regexp"
	"strings"
	"unicode"
)

var screenTitleRE = regexp.MustCompile(`^<(?:b|strong)>(.*?)</(?:b|strong)>$`)
var screenBlockRE = regexp.MustCompile(`(?s)(<(?:pre|blockquote)(?:\s[^>]*)?>.*?</(?:pre|blockquote)>)`)

// screenRichHTML сохраняет HTML экрана и делает абзацы явными. Блоки кода
// и цитаты проходят отдельно: переносы внутри pre должны остаться текстом.
func screenRichHTML(source string) string {
	source = strings.TrimSpace(strings.ReplaceAll(source, "\r\n", "\n"))
	source = strings.NewReplacer(`<span class="tg-spoiler">`, "<tg-spoiler>",
		"</span>", "</tg-spoiler>").Replace(source)
	var out strings.Builder
	for _, part := range splitScreenBlocks(source) {
		if strings.HasPrefix(part, "<pre") || strings.HasPrefix(part, "<blockquote") {
			out.WriteString(part)
			continue
		}
		for _, paragraph := range strings.Split(part, "\n\n") {
			paragraph = strings.TrimSpace(paragraph)
			if paragraph == "" {
				continue
			}
			first, rest, _ := strings.Cut(paragraph, "\n")
			first = strings.TrimLeftFunc(first, func(r rune) bool { return r != '<' && (unicode.IsSymbol(r) || unicode.IsSpace(r) || r == 0xFE0F) })
			if match := screenTitleRE.FindStringSubmatch(first); match != nil {
				out.WriteString("<h3>" + match[1] + "</h3>")
				if rest != "" {
					out.WriteString("<p>" + strings.ReplaceAll(rest, "\n", "<br/>") + "</p>")
				}
			} else {
				out.WriteString("<p>" + strings.ReplaceAll(paragraph, "\n", "<br/>") + "</p>")
			}
		}
	}
	return out.String()
}

func splitScreenBlocks(source string) []string {
	var out []string
	last := 0
	for _, bounds := range screenBlockRE.FindAllStringIndex(source, -1) {
		out = append(out, source[last:bounds[0]], source[bounds[0]:bounds[1]])
		last = bounds[1]
	}
	return append(out, source[last:])
}
