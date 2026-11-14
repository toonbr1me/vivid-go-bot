package bot

import (
	"strings"
	"testing"

	"github.com/mymmrac/telego"
)

// Telegram отклоняет всё сообщение, если хоть одна кнопка ведёт на
// негодный адрес. Такую кнопку лучше убрать: экран важнее лишней кнопки.
func TestKeyboardDropsUnusableURL(t *testing.T) {
	markup := keyboard(
		[]telego.InlineKeyboardButton{
			urlButton("Локальный сайт", "http://localhost:8081/happ?u=x"),
			urlButton("Наш сайт", "https://example.com"),
		},
		[]telego.InlineKeyboardButton{
			button("Меню", "nav:main"),
			copyButton("Скопировать", "vless://link"),
		},
	)
	rows := markup.InlineKeyboard
	if len(rows) != 2 {
		t.Fatalf("рядов = %d, ждали 2: %v", len(rows), rows)
	}
	if len(rows[0]) != 1 {
		t.Fatalf("в первом ряду %d кнопок, ждали 1: %v", len(rows[0]), rows[0])
	}
	if rows[0][0].URL != "https://example.com" {
		t.Fatalf("осталась не та кнопка: %v", rows[0][0])
	}
	if len(rows[1]) != 2 {
		t.Fatalf("во втором ряду %d кнопок, ждали 2: %v", len(rows[1]), rows[1])
	}
}

// Кнопка tg:// Telegram открывает как ссылку — её оставляем.
func TestKeyboardKeepsTelegramURL(t *testing.T) {
	markup := keyboard([]telego.InlineKeyboardButton{
		urlButton("Открыть", "tg://resolve?domain=example"),
	})
	if len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 1 {
		t.Fatalf("кнопка пропала: %v", markup.InlineKeyboard)
	}
}

// Ряд, где все кнопки негодные, исчезает целиком.
func TestKeyboardDropsEmptyRows(t *testing.T) {
	markup := keyboard(
		[]telego.InlineKeyboardButton{
			urlButton("Локальный сайт", "http://localhost:8081/"),
		},
		[]telego.InlineKeyboardButton{
			button("Меню", "nav:main"),
		},
	)
	if len(markup.InlineKeyboard) != 1 {
		t.Fatalf("рядов = %d, ждали 1: %v", len(markup.InlineKeyboard), markup.InlineKeyboard)
	}
}

// Кнопка с негодным адресом не встраивается в текст rich-сообщения.
func TestRichButtonRejectsUnusableURL(t *testing.T) {
	if _, ok := richButton(urlButton("Локальный сайт", "http://localhost:8081/")); ok {
		t.Fatal("кнопка с http-адресом не должна встраиваться в текст")
	}
	if _, ok := richButton(urlButton("Наш сайт", "https://example.com")); !ok {
		t.Fatal("кнопка с https-адресом должна встраиваться")
	}
}

// Адрес, который Telegram открывает: https и tg.
func TestTelegramURL(t *testing.T) {
	good := []string{"https://example.com", "HTTPS://example.com", "tg://resolve?domain=x"}
	bad := []string{"http://localhost:8081", "example.com", "", "ftp://example.com"}
	for _, url := range good {
		if !telegramURL(url) {
			t.Fatalf("%q должен быть годным адресом", url)
		}
	}
	for _, url := range bad {
		if telegramURL(url) {
			t.Fatalf("%q не должен быть годным адресом", url)
		}
	}
}

// Экран подписки показывает ссылку текстом: кнопка Happ может не дойти,
// а подключиться надо.
func TestRichSubscriptionKeepsLinkAsText(t *testing.T) {
	if !strings.Contains(plainLine("Ссылка"), "Ссылка") {
		t.Fatal("plainLine должен убирать только разметку")
	}
}
