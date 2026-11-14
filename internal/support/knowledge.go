package support

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// KnowledgeFile — основной файл базы знаний в каталоге WEB_DIR.
const KnowledgeFile = "knowledge.md"

// Knowledge — база знаний о сервисе.
//
// Это то, о чём помощник рассказывает сам: как подключиться, что делать с
// оплатой, когда возвращают деньги. Факты о конкретном человеке (его
// подписка, баланс, заказы) приходят отдельно и не хранятся здесь.
//
// Формат — обычный текст в Markdown: разделы на «##» подсказывают модели
// тему, а файлы knowledge-*.md добавляются целиком.
type Knowledge struct {
	sections []string
}

// LoadKnowledge читает базу знаний из каталога.
//
// Файла нет — это не ошибка: помощник тогда знает только факты о сервисе из
// программы. Ошибка чтения — тоже не ошибка: база знаний должна помогать, а
// не мешать боту работать.
func LoadKnowledge(dir string) *Knowledge {
	knowledge := &Knowledge{}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return knowledge
	}
	if data, err := os.ReadFile(filepath.Join(dir, KnowledgeFile)); err == nil {
		if text := strings.TrimSpace(string(data)); text != "" {
			knowledge.sections = append(knowledge.sections, text)
		}
	}
	// Дополнительные файлы: knowledge-*.md.
	paths, err := filepath.Glob(filepath.Join(dir, "knowledge-*.md"))
	if err != nil {
		return knowledge
	}
	sort.Strings(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(string(data)); text != "" {
			knowledge.sections = append(knowledge.sections, text)
		}
	}
	return knowledge
}

// Empty — знаний нет.
func (k *Knowledge) Empty() bool { return k == nil || len(k.sections) == 0 }

// Prompt — база знаний для подсказки модели.
func (k *Knowledge) Prompt() string {
	if k.Empty() {
		return ""
	}
	return strings.Join(k.sections, "\n\n---\n\n")
}

// Append добавляет знания к подсказке модели.
func (k *Knowledge) Append(prompt string) string {
	if k.Empty() {
		return prompt
	}
	if prompt == "" {
		return k.Prompt()
	}
	return prompt + "\n\n" + k.Prompt()
}
