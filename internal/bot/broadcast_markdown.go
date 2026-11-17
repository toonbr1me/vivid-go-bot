package bot

import (
	"sort"
	"strings"

	"github.com/mymmrac/telego"
)

// broadcastMarkdown carries editor-selected Telegram formatting into the rich
// renderer. The delivery worker never uses Bot API parse_mode for broadcasts.
func broadcastMarkdown(text string, entities []telego.MessageEntity) string {
	type tag struct {
		start, end  int
		open, close string
	}
	tags := make([]tag, 0, len(entities))
	for _, entity := range entities {
		open, close := broadcastEntityMarkdown(entity)
		if open != "" && entity.Offset >= 0 && entity.Length > 0 {
			tags = append(tags, tag{entity.Offset, entity.Offset + entity.Length, open, close})
		}
	}
	positions := utf16Positions(text)
	starts, ends := map[int][]tag{}, map[int][]tag{}
	for _, item := range tags {
		if item.start < len(positions) && item.end < len(positions) {
			starts[item.start] = append(starts[item.start], item)
			ends[item.end] = append(ends[item.end], item)
		}
	}
	var out strings.Builder
	for pos := 0; pos < len(positions); pos++ {
		sort.Slice(ends[pos], func(i, j int) bool { return ends[pos][i].start > ends[pos][j].start })
		for _, item := range ends[pos] {
			out.WriteString(item.close)
		}
		sort.Slice(starts[pos], func(i, j int) bool { return starts[pos][i].end > starts[pos][j].end })
		for _, item := range starts[pos] {
			out.WriteString(item.open)
		}
		if pos+1 < len(positions) {
			out.WriteString(escapeBroadcastMarkdown(text[positions[pos]:positions[pos+1]]))
		}
	}
	return out.String()
}

func utf16Positions(text string) []int {
	positions := []int{0}
	for byteIndex, r := range text {
		if byteIndex != 0 {
			positions = append(positions, byteIndex)
		}
		if r > 0xffff {
			positions = append(positions, byteIndex)
		}
	}
	return append(positions, len(text))
}

func escapeBroadcastMarkdown(text string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`, "~", `\~`, "`", "\\`").Replace(text)
}

func broadcastEntityMarkdown(entity telego.MessageEntity) (string, string) {
	switch entity.Type {
	case telego.EntityTypeBold:
		return "**", "**"
	case telego.EntityTypeItalic:
		return "_", "_"
	case telego.EntityTypeUnderline:
		return "__", "__"
	case telego.EntityTypeStrikethrough:
		return "~~", "~~"
	case telego.EntityTypeSpoiler:
		return "||", "||"
	case telego.EntityTypeCode:
		return "`", "`"
	case telego.EntityTypePre:
		return "```" + entity.Language + "\n", "\n```"
	case telego.EntityTypeTextLink:
		return "[", "](" + entity.URL + ")"
	}
	return "", ""
}
