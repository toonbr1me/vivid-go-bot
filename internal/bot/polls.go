package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"

	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// pollAudience — кому отправлять опрос.
const (
	pollAudienceAll    = "all"
	pollAudienceActive = "active"
)

// handlePollsCommand — список опросов и запуск нового.
func (b *Bot) handlePollsCommand(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	if !b.isOwner(ctx, tgID) {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID}, "Недостаточно прав.", keyboard(b.backRow()))
		return nil
	}
	text, kb := b.pollsScreen(ctx, tgID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

// pollsScreen — список опросов.
func (b *Bot) pollsScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	polls, err := b.db.ListPolls(ctx, 20)
	if err != nil {
		return "⚠️ Не удалось загрузить опросы.", keyboard(b.backRow())
	}
	lines := []string{"📊 <b>Опросы</b>", ""}
	var rows [][]telego.InlineKeyboardButton
	for _, poll := range polls {
		state := "черновик"
		switch poll.Status {
		case "sent":
			state = "отправлен"
		case "closed":
			state = "закрыт"
		}
		lines = append(lines, fmt.Sprintf("• %s — %s, отправлено: %d",
			escapeHTML(poll.Question), state, poll.SentCount))
		rows = append(rows, []telego.InlineKeyboardButton{
			button("Итоги", "poll:results:"+strconv.FormatInt(poll.ID, 10)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("➕ Новый опрос", "poll:new"),
		button("🏠 Меню", "nav:main"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// pollResultsScreen — итоги опроса.
func (b *Bot) pollResultsScreen(ctx context.Context, pollID int64) (string, *telego.InlineKeyboardMarkup) {
	poll, err := b.db.GetPoll(ctx, pollID)
	if err != nil || poll == nil {
		return "Опрос не найден.", keyboard(b.backRow())
	}
	counts, err := b.db.PollResults(ctx, pollID)
	if err != nil {
		return "⚠️ Не удалось посчитать результаты.", keyboard(b.backRow())
	}
	lines := []string{
		"📊 <b>" + escapeHTML(poll.Question) + "</b>",
		"",
		fmt.Sprintf("Голосов: <b>%d</b>", counts.Voters),
		"",
	}
	for i, option := range counts.Options {
		value := 0
		if i < len(counts.Counts) {
			value = counts.Counts[i]
		}
		lines = append(lines, fmt.Sprintf("• %s — <b>%d</b>", escapeHTML(option), value))
	}
	return strings.Join(lines, "\n"), keyboard([]telego.InlineKeyboardButton{
		button("← Опросы", "poll:list"),
		button("🏠 Меню", "nav:main"),
	})
}

// handlePollInput — шаг мастера опроса: сначала вопрос, потом варианты.
func (b *Bot) handlePollInput(ctx context.Context, chatID telego.ChatID, tgID int64, kind, ref, text string) {
	switch kind {
	case inputPollQuestion:
		b.clearInput(tgID)
		if strings.TrimSpace(text) == "" {
			b.send(ctx, chatID, "Вопрос не может быть пустым.", keyboard(b.backRow()))
			return
		}
		b.setInput(tgID, inputPollOptions, strings.TrimSpace(text))
		b.send(ctx, chatID,
			"📝 Варианты ответа — по одному в строке. Например:\n"+
				"Один месяц\nТри месяца\nПолгода",
			keyboard([]telego.InlineKeyboardButton{button("← Отмена", "poll:list")}))
	case inputPollOptions:
		b.clearInput(tgID)
		options := parsePollOptions(text)
		if len(options) < 2 {
			b.send(ctx, chatID, "Нужно минимум два варианта, каждый — с новой строки.",
				keyboard([]telego.InlineKeyboardButton{button("← Отмена", "poll:list")}))
			return
		}
		b.createAndSendPoll(ctx, chatID, tgID, ref, options)
	}
}

// parsePollOptions — варианты ответа по строкам.
func parsePollOptions(text string) []string {
	out := make([]string, 0, 4)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= 10 {
			break
		}
	}
	return out
}

// createAndSendPoll создаёт опрос и рассылает его.
func (b *Bot) createAndSendPoll(ctx context.Context, chatID telego.ChatID, tgID int64, question string, options []string) {
	createdBy := tgID
	poll, err := b.db.CreatePoll(ctx, storage.Poll{
		Question:       question,
		Options:        options,
		IsAnonymous:    true,
		AllowsMultiple: false,
		Audience:       pollAudienceAll,
	}, &createdBy)
	if err != nil {
		b.send(ctx, chatID, "⚠️ Не удалось создать опрос: "+err.Error(), keyboard(b.backRow()))
		return
	}
	recipients, err := b.db.PollAudience(ctx, pollAudienceAll, 20000)
	if err != nil {
		b.send(ctx, chatID, "⚠️ Не удалось собрать аудиторию: "+err.Error(), keyboard(b.backRow()))
		return
	}
	sent, failed := b.sendPollTo(ctx, poll, recipients)
	_ = b.db.MarkPollSent(ctx, poll.ID, sent, failed)
	b.send(ctx, chatID, fmt.Sprintf("📊 Опрос отправлен: %d, ошибок: %d.", sent, failed),
		keyboard([]telego.InlineKeyboardButton{
			button("Итоги", "poll:results:"+strconv.FormatInt(poll.ID, 10)),
			button("← Опросы", "poll:list"),
		}))
}

// sendPollTo рассылает опрос и считает доставленные.
func (b *Bot) sendPollTo(ctx context.Context, poll storage.Poll, recipients []int64) (int, int) {
	sent, failed := 0, 0
	anonymous := poll.IsAnonymous
	for _, telegramID := range recipients {
		choices := make([]telego.InputPollOption, 0, len(poll.Options))
		for _, option := range poll.Options {
			choices = append(choices, telego.InputPollOption{Text: option})
		}
		message, err := b.api.SendPoll(ctx, &telego.SendPollParams{
			ChatID:                telego.ChatID{ID: telegramID},
			Question:              poll.Question,
			Options:               choices,
			IsAnonymous:           &anonymous,
			AllowsMultipleAnswers: poll.AllowsMultiple,
		})
		if err != nil || message == nil {
			failed++
			continue
		}
		sent++
		var pollID string
		if message.Poll != nil {
			pollID = message.Poll.ID
		}
		if pollID != "" {
			_ = b.db.RecordPollDelivery(ctx, pollID, poll.ID, telegramID, int64(message.MessageID))
		}
	}
	return sent, failed
}

// handlePollCallback — кнопки опросов.
func (b *Bot) handlePollCallback(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, action, rest string) {
	if !b.isOwner(ctx, tgID) {
		b.answer(ctx, queryID, "Недостаточно прав", true)
		return
	}
	switch action {
	case "new":
		b.answer(ctx, queryID, "", false)
		b.setInput(tgID, inputPollQuestion, "")
		b.upsert(ctx, chatID, messageID,
			"📝 Вопрос опроса — одним сообщением.",
			keyboard([]telego.InlineKeyboardButton{button("← Отмена", "poll:list")}))
	case "list":
		b.answer(ctx, queryID, "", false)
		text, kb := b.pollsScreen(ctx, tgID)
		b.upsert(ctx, chatID, messageID, text, kb)
	case "results":
		b.answer(ctx, queryID, "", false)
		pollID, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			b.answer(ctx, queryID, "Опрос не найден", true)
			return
		}
		text, kb := b.pollResultsScreen(ctx, pollID)
		b.upsert(ctx, chatID, messageID, text, kb)
	default:
		b.answer(ctx, queryID, "Неизвестное действие", true)
	}
}
