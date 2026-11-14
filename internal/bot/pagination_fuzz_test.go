package bot

import (
	"encoding/json"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzSupportPaginationPreservesText(f *testing.F) {
	for _, seed := range []string{"", "Короткий ответ", strings.Repeat("Сообщение на русском. ", 50), "<b>Разметка</b>\n\nНовая строка", "🙂 Unicode\n\tWhitespace", "<script>не исполнять</script>"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if !utf8.ValidString(text) || len(text) > 8192 {
			return
		}
		state := textCard("Ответ\n"+text, nil)
		state, err := prepareCard(state)
		if err != nil {
			t.Fatal(err)
		}
		var joined strings.Builder
		for _, p := range state.Pages {
			if !utf8.ValidString(p.Body) {
				t.Fatal("pagination cut a UTF-8 character")
			}
			raw, err := json.Marshal(p)
			if err != nil || len(raw) > 1024 {
				t.Fatal("page cannot be delivered as private widget state")
			}
			joined.WriteString(p.Body)
		}
		compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
		if compact(joined.String()) != compact(mdhtml.Plain(text)) {
			t.Fatal("support text lost during pagination")
		}
	})
}
