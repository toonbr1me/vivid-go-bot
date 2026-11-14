package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Файл окружения нужен только для локального запуска: переменные из него
// попадают в процесс, но не затирают уже заданные.
func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "\ufeffBOT_TOKEN=123:abc\n" +
		"# комментарий\n" +
		"\n" +
		"export BRAND_NAME=\"Мой сервис\"\n" +
		"DATABASE_URL=postgres://from-file\n" +
		"EMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DATABASE_URL", "postgres://from-env")
	if err := loadDotEnv(path); err != nil {
		t.Fatalf("чтение .env: %v", err)
	}

	cases := map[string]string{
		"BOT_TOKEN":    "123:abc",
		"BRAND_NAME":   "Мой сервис",
		"DATABASE_URL": "postgres://from-env",
		"EMPTY":        "",
	}
	for key, want := range cases {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, ждали %q", key, got, want)
		}
	}
}

// Без файла запуск не падает: в контейнере переменные приходят из compose.
func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), "нет-такого")); err != nil {
		t.Fatalf("отсутствие файла — не ошибка: %v", err)
	}
}
