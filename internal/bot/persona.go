package bot

import (
	"fmt"
	"time"
)

// greeting — приветствие по времени суток.
//
// Время берётся в часовом поясе сервиса: «доброй ночи» в три часа ночи по
// Москве — это приветствие, а не ошибка.
func (b *Bot) greeting() string {
	now := time.Now().In(b.timezone())
	key := "night"
	switch hour := now.Hour(); {
	case hour >= 5 && hour < 12:
		key = "morning"
	case hour >= 12 && hour < 18:
		key = "day"
	case hour >= 18 && hour < 23:
		key = "evening"
	}
	text, ok := b.texts.Persona.Greetings[key]
	if !ok || text == "" {
		return ""
	}
	if mascot := b.texts.Persona.Mascot; mascot != "" {
		return fmt.Sprintf("%s %s", mascot, text)
	}
	return text
}

// timezone — часовой пояс сервиса. Если он не задан или неизвестен, берётся
// время сервера.
func (b *Bot) timezone() *time.Location {
	if b.cfg == nil || b.cfg.Timezone == "" {
		return time.Local
	}
	location, err := time.LoadLocation(b.cfg.Timezone)
	if err != nil {
		return time.Local
	}
	return location
}

// personaGreetingKey — ключ приветствия для теста и для отладки.
func personaGreetingKey(hour int) string {
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 18:
		return "day"
	case hour >= 18 && hour < 23:
		return "evening"
	default:
		return "night"
	}
}
