// Package legacy contains presentation for ordinary Telegram rich messages.
package legacy

import (
	"fmt"
	"html"
	"strings"
)

type Stat struct {
	Label, Value string
	Warn         bool
}
type Page struct {
	Title, Status, Kicker, Value, Note, Body, Updated string
	Step                                              int
	Stats                                             []Stat
}

func Fact(label, value string) string {
	return `<tr><td>` + html.EscapeString(label) + `</td><td align="right"><b>` + html.EscapeString(value) + `</b></td></tr>`
}
func Document(p Page) string {

	var out strings.Builder
	out.WriteString("<h3>" + html.EscapeString(p.Title) + "</h3>")
	if p.Status != "" {
		out.WriteString("<p><b>" + html.EscapeString(p.Status) + "</b></p>")
	}
	if p.Step > 0 {
		out.WriteString(fmt.Sprintf("<p>Шаг %d из 2</p>", p.Step))
	}
	if p.Value != "" && p.Kicker == "" {
		out.WriteString("<p><b>" + html.EscapeString(p.Value) + "</b></p>")
	}
	if p.Kicker != "" || len(p.Stats) > 0 {
		out.WriteString("<table compact>")
		if p.Kicker != "" {
			out.WriteString(Fact(p.Kicker, p.Value))
		}
		for _, stat := range p.Stats {
			out.WriteString(Fact(stat.Label, stat.Value))
		}
		out.WriteString("</table>")
	}
	if p.Note != "" {
		out.WriteString("<p>" + html.EscapeString(p.Note) + "</p>")
	}
	for _, stat := range p.Stats {
		if stat.Warn {
			out.WriteString("<blockquote>Устройств больше доступного лимита. Откройте список и выберите лишние для отвязки.</blockquote>")
			break
		}
	}
	if p.Body != "" {
		lines := strings.Split(p.Body, "\n")
		if len(lines) > 1 {
			out.WriteString("<ul>")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					out.WriteString("<li>" + html.EscapeString(line) + "</li>")
				}
			}
			out.WriteString("</ul>")
		} else {
			out.WriteString("<p>" + html.EscapeString(p.Body) + "</p>")
		}
	}
	if p.Updated != "" {
		out.WriteString("<footer>Обновлено: " + html.EscapeString(p.Updated) + "</footer>")
	}
	return out.String()
}
