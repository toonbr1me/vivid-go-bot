package main

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// loadDotEnv читает файл окружения перед стартом.
//
// Docker передаёт переменные сам, а при локальном `go run ./cmd/vivid` их
// взять неоткуда: без этого README обещал запуск, который падал на пустом
// DATABASE_URL. Уже заданные переменные не перезаписываются — окружение
// сильнее файла, иначе нельзя быстро проверить другую базу или токен.
//
// Формат простой: KEY=VALUE, строки с # — комментарии, кавычки вокруг
// значения снимаются. Отсутствие файла не ошибка: в контейнере его нет.
func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		separator := strings.IndexByte(line, '=')
		if separator <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if key == "" {
			continue
		}
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}
