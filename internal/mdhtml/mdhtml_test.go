package mdhtml

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Разметка, которой пользуются люди, должна доезжать до Telegram тегами.
func TestRenderInline(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"жирный", "**важно**", "<b>важно</b>"},
		{"подчёркнутый жирный", "__важно__", "<b>важно</b>"},
		{"курсив", "*важно*", "<i>важно</i>"},
		{"курсив подчёркиванием", "_важно_", "<i>важно</i>"},
		{"зачёркнутый", "~~старое~~", "<s>старое</s>"},
		{"спойлер", "||секрет||", `<span class="tg-spoiler">секрет</span>`},
		{"код", "`curl -I`", "<code>curl -I</code>"},
		{"жирный курсив", "***сразу***", "<b><i>сразу</i></b>"},
		{"ссылка", "[канал](https://t.me/example)", `<a href="https://t.me/example">канал</a>`},
		{"картинка как ссылка", "![схема](https://example.com/a.png)", `<a href="https://example.com/a.png">схема</a>`},
		{"смешанное", "**до** и *после*", "<b>до</b> и <i>после</i>"},
		{"экранирование", "a * b < c & d", "a * b &lt; c &amp; d"},
		{"html не проходит", "<b>чужой тег</b>", "&lt;b&gt;чужой тег&lt;/b&gt;"},
		{"script не проходит", `<script>alert(1)</script>`, "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{"snake_case остаётся", "user_first_name", "user_first_name"},
		{"умножение остаётся", "2 * 3 * 4", "2 * 3 * 4"},
		{"непарная звезда", "**не закрыто", "**не закрыто"},
		{"небезопасная ссылка", "[тык](javascript:alert(1))", "[тык](javascript:alert(1))"},
		{"экранированная звезда", `\*текст\*`, "*текст*"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := Render(test.source); got != test.want {
				t.Fatalf("Render(%q) = %q, ждали %q", test.source, got, test.want)
			}
		})
	}
}

// Блоки: заголовки, цитаты, списки, черта.
func TestRenderBlocks(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"заголовок", "## Тарифы", "<b>Тарифы</b>"},
		{"цитата", "> Доступ выдан", "<blockquote>Доступ выдан</blockquote>"},
		{"черта", "---", divider},
		{"маркированный список", "- раз\n- два", "• раз\n• два"},
		{"нумерованный список", "1. раз\n2. два", "1. раз\n2. два"},
		{"задача сделана", "- [x] оплатить", "☑ оплатить"},
		{"задача не сделана", "- [ ] проверить", "☐ проверить"},
		{"абзацы", "Первый.\n\nВторой.", "Первый.\n\nВторой."},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := Render(test.source); got != test.want {
				t.Fatalf("Render(%q) = %q, ждали %q", test.source, got, test.want)
			}
		})
	}
}

// Вложенный список не сливается с внешним.
func TestRenderNestedList(t *testing.T) {
	got := Render("- первый\n  - вложенный\n- второй")
	want := "• первый\n" + indent + "◦ вложенный\n• второй"
	if got != want {
		t.Fatalf("Render = %q, ждали %q", got, want)
	}
}

// Код остаётся кодом: звёздочки и угловые скобки внутри не форматируются.
func TestRenderCodeKeepsText(t *testing.T) {
	source := "```go\nfmt.Println(\"<b>**не жирный**</b>\")\n```"
	got := Render(source)
	want := `<pre><code class="language-go">fmt.Println("&lt;b&gt;**не жирный**&lt;/b&gt;")</code></pre>`
	if got != want {
		t.Fatalf("Render = %q, ждали %q", got, want)
	}
	if strings.Contains(got, "<b>") {
		t.Error("внутри кода разметка не должна срабатывать")
	}
}

// Непарный забор кода не съедает остаток сообщения молча.
func TestRenderUnclosedFence(t *testing.T) {
	got := Render("```\nстрока")
	if !strings.HasPrefix(got, "<pre>") {
		t.Fatalf("Render = %q, ждали блок кода", got)
	}
}

// Таблица печатается ровными столбцами в моноширинном блоке.
func TestRenderTable(t *testing.T) {
	source := "| Тариф | Цена |\n|---|--:|\n| Месяц | 300 ₽ |\n| Год | 2400 ₽ |"
	got := Render(source)
	if !strings.HasPrefix(got, "<pre>") || !strings.HasSuffix(got, "</pre>") {
		t.Fatalf("Render = %q, ждали <pre>", got)
	}
	if !strings.Contains(got, "Тариф") || !strings.Contains(got, "2400") {
		t.Errorf("в таблице потерялись данные: %q", got)
	}
	if !strings.Contains(got, "┼") {
		t.Errorf("нет линии под заголовком: %q", got)
	}
}

// Длинный ответ делится на части, и ни одна не превышает предел.
func TestChunksRespectLimit(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 60; i++ {
		source.WriteString("**Пункт ")
		source.WriteString(strings.Repeat("д", 40))
		source.WriteString("**\n\n")
	}
	parts := Chunks(source.String(), 500)
	if len(parts) < 2 {
		t.Fatalf("частей %d, ждали несколько", len(parts))
	}
	for index, part := range parts {
		if size := utf8.RuneCountInString(part); size > 500 {
			t.Errorf("часть %d длиной %d, предел 500", index, size)
		}
		if !balanced(part) {
			t.Errorf("часть %d с несбалансированными тегами: %q", index, part)
		}
	}
	if joined := strings.Join(parts, ""); strings.Count(joined, "<b>") != 60 {
		t.Errorf("потерялись пункты: %d из 60", strings.Count(joined, "<b>"))
	}
}

// Одна гигантская строка без переносов тоже делится.
func TestChunksSplitSingleLine(t *testing.T) {
	source := strings.Repeat("слово ", 400)
	parts := Chunks(source, 300)
	if len(parts) < 2 {
		t.Fatalf("частей %d, ждали несколько", len(parts))
	}
	for index, part := range parts {
		if size := utf8.RuneCountInString(part); size > 300 {
			t.Errorf("часть %d длиной %d", index, size)
		}
	}
}

func TestChunksEmpty(t *testing.T) {
	if parts := Chunks("   \n\n", 100); parts != nil {
		t.Fatalf("части = %v, ждали nil", parts)
	}
}

// Plain нужен для запасной отправки без разметки.
func TestPlain(t *testing.T) {
	got := Plain("<b>Жирный</b> и <code>код</code> &amp; <a href=\"https://x\">ссылка</a>")
	want := "Жирный и код & ссылка"
	if got != want {
		t.Fatalf("Plain = %q, ждали %q", got, want)
	}
}

// balanced проверяет, что теги закрываются в правильном порядке.
func balanced(html string) bool {
	var stack []string
	for index := 0; index < len(html); index++ {
		switch {
		case html[index] != '<':
			continue
		case strings.HasPrefix(html[index:], "</"):
			end := strings.IndexByte(html[index:], '>')
			if end < 0 {
				return false
			}
			name := strings.TrimSpace(html[index+2 : index+end])
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return false
			}
			stack = stack[:len(stack)-1]
			index += end
		case strings.HasPrefix(html[index:], "<a "), strings.HasPrefix(html[index:], "<span "):
			end := strings.IndexByte(html[index:], '>')
			if end < 0 {
				return false
			}
			name := "a"
			if strings.HasPrefix(html[index:], "<span ") {
				name = "span"
			}
			stack = append(stack, name)
			index += end
		default:
			end := strings.IndexByte(html[index:], '>')
			if end < 0 {
				return false
			}
			name := strings.TrimSpace(html[index+1 : index+end])
			stack = append(stack, name)
			index += end
		}
	}
	return len(stack) == 0
}
