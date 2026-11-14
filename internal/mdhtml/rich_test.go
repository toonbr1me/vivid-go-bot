package mdhtml

import (
	"strings"
	"testing"
)

// Богатая разметка должна давать теги, которых нет в обычном HTML.
func TestRichBlocks(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"заголовок", "# Заголовок", "<h1>Заголовок</h1>"},
		{"шестой уровень", "###### Мелкий", "<h6>Мелкий</h6>"},
		{"абзац", "Просто текст", "<p>Просто текст</p>"},
		{"жирный", "**важно**", "<p><b>важно</b></p>"},
		{"выделение", "==важно==", "<p><mark>важно</mark></p>"},
		{"спойлер", "||секрет||", "<p><tg-spoiler>секрет</tg-spoiler></p>"},
		{"черта", "---", "<hr/>"},
		{"цитата", "> Доступ выдан", "<blockquote>Доступ выдан</blockquote>"},
		{"список", "- раз\n- два", "<ul><li>раз</li><li>два</li></ul>"},
		{"нумерация", "1. раз\n2. два", "<ol><li>раз</li><li>два</li></ol>"},
		{"задача", "- [x] оплатить", `<ul><li><input type="checkbox" checked>оплатить</li></ul>`},
		{"код", "```go\nfmt.Println(1)\n```", `<pre><code class="language-go">fmt.Println(1)</code></pre>`},
		{"html не проходит", "<script>alert(1)</script>", "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := Rich(test.source); got != test.want {
				t.Fatalf("Rich(%q) = %q, ждали %q", test.source, got, test.want)
			}
		})
	}
}

// Таблица должна стать настоящей таблицей, а не моноширинным блоком.
func TestRichTable(t *testing.T) {
	source := "| Тариф | Цена |\n|---|--:|\n| Месяц | 300 |"
	want := `<table bordered striped compact>` +
		`<tr><th align="left">Тариф</th><th align="right">Цена</th></tr>` +
		`<tr><td align="left">Месяц</td><td align="right">300</td></tr>` +
		`</table>`
	if got := Rich(source); got != want {
		t.Fatalf("Rich(%q) = %q, ждали %q", source, got, want)
	}
}

// Кнопки в тексте: отдельная строка становится рядом, ссылка — кнопкой.
func TestRichButtons(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			"callback",
			"[Купить](btn:nav:buy)",
			`<tg-button-row><tg-button type="callback_data" data="nav:buy">Купить</tg-button></tg-button-row>`,
		},
		{
			"ссылка",
			"[Сайт](btn-url:https://example.com)",
			`<tg-button-row><tg-button type="url" url="https://example.com">Сайт</tg-button></tg-button-row>`,
		},
		{
			"два ряда",
			"[Раз](btn:a)\n[Два](btn:b)",
			"<tg-button-row><tg-button type=\"callback_data\" data=\"a\">Раз</tg-button></tg-button-row>\n" +
				"<tg-button-row><tg-button type=\"callback_data\" data=\"b\">Два</tg-button></tg-button-row>",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := Rich(test.source); got != test.want {
				t.Fatalf("Rich(%q) = %q, ждали %q", test.source, got, test.want)
			}
		})
	}
}

// Кнопка внутри абзаца остаётся кнопкой, но не ломает текст.
func TestRichInlineButton(t *testing.T) {
	got := Rich("Нажмите [сюда](btn:nav:buy) для покупки")
	want := `<p>Нажмите <tg-button type="callback_data" data="nav:buy">сюда</tg-button> для покупки</p>`
	if got != want {
		t.Fatalf("Rich = %q, ждали %q", got, want)
	}
}

// Запасной текст не должен показывать пользователю служебные теги.
func TestRichPlain(t *testing.T) {
	plain := RichPlain("## Заголовок\n\nПодписка **активна**")
	if strings.Contains(plain, "<") {
		t.Fatalf("в запасном тексте остались теги: %q", plain)
	}
	if !strings.Contains(plain, "Заголовок") || !strings.Contains(plain, "Подписка активна") {
		t.Fatalf("в запасном тексте потерялся смысл: %q", plain)
	}
}
