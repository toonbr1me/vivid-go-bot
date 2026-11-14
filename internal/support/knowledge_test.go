package support

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeEmptyWithoutFiles(t *testing.T) {
	knowledge := LoadKnowledge(t.TempDir())
	if !knowledge.Empty() {
		t.Error("без файлов база знаний должна быть пустой")
	}
	if knowledge.Prompt() != "" {
		t.Errorf("пустая база не должна давать подсказку: %q", knowledge.Prompt())
	}
}

func TestKnowledgeReadsMainAndExtraFiles(t *testing.T) {
	dir := t.TempDir()
	main := "# Основное\n\nТарифы описаны на сайте."
	if err := os.WriteFile(filepath.Join(dir, KnowledgeFile), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	extra := "# Дополнительное\n\nВозврат в течение суток."
	if err := os.WriteFile(filepath.Join(dir, "knowledge-refund.md"), []byte(extra), 0o600); err != nil {
		t.Fatal(err)
	}

	knowledge := LoadKnowledge(dir)
	if knowledge.Empty() {
		t.Fatal("база знаний должна быть прочитана")
	}
	prompt := knowledge.Prompt()
	if !strings.Contains(prompt, "Тарифы описаны на сайте") {
		t.Errorf("в подсказке нет основного файла: %q", prompt)
	}
	if !strings.Contains(prompt, "Возврат в течение суток") {
		t.Errorf("в подсказке нет дополнительного файла: %q", prompt)
	}
}

func TestKnowledgeAppendsToPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KnowledgeFile), []byte("Знание"), 0o600); err != nil {
		t.Fatal(err)
	}
	knowledge := LoadKnowledge(dir)

	prompt := knowledge.Append("Правила")
	if !strings.Contains(prompt, "Правила") || !strings.Contains(prompt, "Знание") {
		t.Errorf("знания не добавились к подсказке: %q", prompt)
	}
	// Без знаний подсказка остаётся как была.
	empty := LoadKnowledge(t.TempDir())
	if got := empty.Append("Правила"); got != "Правила" {
		t.Errorf("без знаний подсказка изменилась: %q", got)
	}
}
