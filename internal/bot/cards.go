package bot

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
)

//go:embed widgets/interface.html
var interfaceWidgetHTML string

const textCardButtonsPerPage = 3
const interfaceCardVersion = 5

type cardButton struct {
	Text    string `json:"t"`
	Index   int    `json:"i"`
	Style   string `json:"s,omitempty"`
	Detail  string `json:"d,omitempty"`
	Kind    string `json:"k,omitempty"`
	Payload string `json:"p,omitempty"`
	Checked *bool  `json:"checked,omitempty"`
}
type cardStat struct {
	Label string `json:"l"`
	Value string `json:"v"`
	Nav   string `json:"n,omitempty"`
	Warn  bool   `json:"w,omitempty"`
}
type cardPage struct {
	Title    string       `json:"t"`
	Value    string       `json:"v,omitempty"`
	Note     string       `json:"n,omitempty"`
	Body     string       `json:"x,omitempty"`
	Tab      string       `json:"tab,omitempty"`
	Buttons  []cardButton `json:"b,omitempty"`
	Revision string       `json:"r"`
	Page     int          `json:"p"`
	Pages    int          `json:"z"`
	Status   string       `json:"s,omitempty"`
	Tone     string       `json:"c,omitempty"`
	Kicker   string       `json:"k,omitempty"`
	Stats    []cardStat   `json:"m,omitempty"`
	Layout   string       `json:"l,omitempty"`
	Step     int          `json:"q,omitempty"`
	Back     int          `json:"back,omitempty"`
	Refresh  int          `json:"f,omitempty"`
	Updated  string       `json:"u,omitempty"`
	Hero     bool         `json:"hero,omitempty"`
	Used     int          `json:"used,omitempty"`
	Limit    int          `json:"limit,omitempty"`
}
type cardState struct {
	Pages      []cardPage                    `json:"pages"`
	Actions    []telego.InlineKeyboardButton `json:"actions"`
	Current    int                           `json:"current"`
	Version    int                           `json:"version,omitempty"`
	Appearance string                        `json:"appearance,omitempty"`
}

func cardUtility(state cardState, callback string, refresh bool) cardState {
	index := len(state.Actions)
	state.Actions = append(state.Actions, button("Назад", callback))
	for i := range state.Pages {
		if refresh {
			state.Pages[i].Refresh = index + 1
		} else {
			state.Pages[i].Back = index + 1
		}
	}
	return state
}

// Clipboard and links are synchronous tap actions; their payload is private.
// Split controls rather than exceeding Telegram's 1 KB state limit.
func prepareCard(state cardState) (cardState, error) {
	var pages []cardPage
	current := 0
	for p, page := range state.Pages {
		if p == state.Current {
			current = len(pages)
		}
		page.Revision = "1234567890123"
		page.Page = 999
		page.Pages = 999
		buttons := page.Buttons
		page.Buttons = nil
		for _, control := range buttons {
			if control.Index < 0 || control.Index >= len(state.Actions) {
				return state, errors.New("invalid card control")
			}
			a := state.Actions[control.Index]
			if a.CopyText != nil {
				control.Kind = ""
				control.Payload = ""
				control.Text = strings.Replace(control.Text, "Скопировать", "Получить", 1)
			} else if strings.HasPrefix(a.URL, "https://") {
				control.Kind = "u"
				control.Payload = a.URL
			}
			candidate := page
			candidate.Buttons = append(append([]cardButton{}, page.Buttons...), control)
			payload, _ := json.Marshal(candidate)
			if len(payload) > 1024 {
				if len(page.Buttons) > 0 || page.Body != "" || page.Value != "" || len(page.Stats) > 0 {
					pages = append(pages, page)
				}
				page.Buttons = nil
				page.Body = ""
				page.Value = ""
				page.Stats = nil
			}
			page.Buttons = append(page.Buttons, control)
			payload, _ = json.Marshal(page)
			if len(payload) > 1024 {
				return state, errors.New("card control exceeds private state limit")
			}
		}
		pages = append(pages, page)
	}
	for i := range pages {
		pages[i].Page = i
		pages[i].Pages = len(pages)
	}
	state.Pages = pages
	state.Current = current
	state.Version = interfaceCardVersion
	return state, nil
}

func bytePrefix(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
func cardLabel(s string) string {
	s = strings.TrimSpace(s)
	// Remove decorative prefixes; labels and consequences carry the meaning.
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if unicode.IsSymbol(r) || r == 0xFE0F || unicode.IsSpace(r) {
			s = strings.TrimSpace(s[n:])
			continue
		}
		break
	}
	return bytePrefix(s, 64)
}
func textCard(text string, kb *telego.InlineKeyboardMarkup) cardState {
	plain := strings.TrimSpace(mdhtml.Plain(text))
	title, body, found := strings.Cut(plain, "\n")
	if !found {
		body = ""
	}
	if len(title) > 80 {
		body = title + "\n" + body
		title = "Сообщение"
	}
	title = cardLabel(title)
	if title == "" {
		title = "Сообщение"
	}
	var chunks []string
	body = strings.TrimSpace(body)
	for len(body) > 0 {
		piece := bytePrefix(body, 200)
		// A byte limit alone cannot bound height for text with many short lines.
		lines := 0
		for i, r := range piece {
			if r == '\n' {
				lines++
				if lines == 5 {
					piece = piece[:i]
					break
				}
			}
		}
		if len(piece) < len(body) {
			if i := strings.LastIndexAny(piece, " \n"); i > len(piece)/2 {
				piece = piece[:i]
			}
		}
		chunks = append(chunks, piece)
		body = strings.TrimSpace(body[len(piece):])
	}
	state := cardState{}
	if kb != nil {
		for _, row := range kb.InlineKeyboard {
			for _, button := range row {
				if button.CallbackData != "" || button.URL != "" || button.CopyText != nil {
					state.Actions = append(state.Actions, button)
				}
			}
		}
	}
	count := max(1, len(chunks), (len(state.Actions)+textCardButtonsPerPage-1)/textCardButtonsPerPage)
	tab := "more"
	for _, a := range state.Actions {
		if strings.HasPrefix(a.CallbackData, "pay") || strings.HasPrefix(a.CallbackData, "plan:") || a.CallbackData == "nav:buy" {
			tab = "subscription"
			break
		}
		if strings.HasPrefix(a.CallbackData, "dev:") {
			tab = "devices"
			break
		}
		if strings.HasPrefix(a.CallbackData, "ticket:") || strings.HasPrefix(a.CallbackData, "support:") {
			tab = "support"
			break
		}
	}
	for p := 0; p < count; p++ {
		page := cardPage{Title: title, Page: p, Pages: count, Tab: tab}
		if p < len(chunks) {
			page.Body = chunks[p]
		}
		for i := p * textCardButtonsPerPage; i < min(len(state.Actions), (p+1)*textCardButtonsPerPage); i++ {
			a := state.Actions[i]
			page.Buttons = append(page.Buttons, cardButton{Text: cardLabel(a.Text), Index: i, Style: a.Style})
		}
		state.Pages = append(state.Pages, page)
	}
	return state
}

func (b *Bot) deliverUI(ctx context.Context, id int64, messageID int, state cardState) (err error) {
	defer func() {
		if err == nil {
			markDelivered(ctx)
		}
	}()
	if len(state.Pages) == 0 || state.Current < 0 || state.Current >= len(state.Pages) {
		return errors.New("empty card")
	}
	state, err = prepareCard(state)
	if err != nil {
		return err
	}
	html, signature, err := b.widgetAppearance(ctx)
	if err != nil {
		return err
	}
	state.Appearance = signature
	revision := strconv.FormatInt(time.Now().UnixNano(), 36)
	for i := range state.Pages {
		state.Pages[i].Revision = revision
	}
	payload, err := json.Marshal(state.Pages[state.Current])
	if err != nil {
		return err
	}
	if len(payload) > 1024 {
		return fmt.Errorf("card data exceeds limit: %d", len(payload))
	}
	oldID, raw, _ := b.db.BotCard(ctx, id)
	if messageID == 0 && b.cfg.Current().BotCleanChat && !forceNewCard(ctx) {
		messageID = oldID
	}
	var previous cardState
	previousValid := json.Unmarshal(raw, &previous) == nil && len(previous.Pages) > 0 && previous.Version == interfaceCardVersion
	currentWidget := messageID > 0 && messageID == oldID && previousValid && previous.Appearance == signature
	if currentWidget {
		err = ignoreNotModified(b.api.EditMessageWidgetData(ctx, &telego.EditMessageWidgetDataParams{ChatID: telego.ChatID{ID: id}, MessageID: messageID, ChatData: string(payload)}))
		if err == nil {
			return b.db.SaveBotCard(ctx, id, messageID, state)
		}
		// An API outage must not turn one tap into a new message.
		return err
	}
	message, err := b.api.SendWidget(ctx, &telego.SendWidgetParams{ChatID: telego.ChatID{ID: id}, HTML: html, ChatData: string(payload), AllowQueries: true,
		Caption: "Кабинет " + b.cfg.Current().BrandName, ShowCaptionAboveMedia: true})
	if err != nil {
		// A new template may be refused while a compatible widget already in
		// this private chat can still be updated. Keep that widget usable and
		// retain its actual appearance hash, so a later successful send can
		// apply the pending theme rather than claiming it was applied now.
		if strings.Contains(err.Error(), "INPUT_MEDIA_WIDGET_NOT_ALLOWED") && oldID > 0 && previousValid {
			if editErr := ignoreNotModified(b.api.EditMessageWidgetData(ctx, &telego.EditMessageWidgetDataParams{ChatID: telego.ChatID{ID: id}, MessageID: oldID, ChatData: string(payload)})); editErr == nil {
				state.Appearance = previous.Appearance
				return b.db.SaveBotCard(ctx, id, oldID, state)
			}
		}
		return err
	}
	if err = b.db.SaveBotCard(ctx, id, message.MessageID, state); err != nil {
		return err
	}
	replacedID := messageID
	if replacedID == 0 {
		replacedID = oldID
	}
	if replacedID > 0 && replacedID != message.MessageID {
		_ = b.api.DeleteMessage(ctx, &telego.DeleteMessageParams{ChatID: telego.ChatID{ID: id}, MessageID: replacedID})
	}
	return nil
}

func privateCardQuery(q telego.WidgetQuery) bool {
	_, ok := q.Type.(*telego.WidgetQueryTypeData)
	return ok && q.Chat != nil && q.Chat.Type == "private" && q.From.ID > 0 && q.Chat.ID == q.From.ID && q.MessageID > 0 && q.WidgetID == "" && q.InlineMessageID == ""
}

func (b *Bot) answerCardQuery(ctx *telegohandler.Context, q telego.WidgetQuery) error {
	answer := func(s string) error {
		return b.api.AnswerWidgetQuery(ctx, &telego.AnswerWidgetQueryParams{WidgetQueryID: q.ID, Data: s})
	}
	if !privateCardQuery(q) || b.db == nil {
		return answer(`{"error":"Откройте /menu в личном чате"}`)
	}
	lockCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := b.db.LockBotCard(lockCtx, q.From.ID)
	if err != nil {
		return answer(`{"error":"Попробуйте ещё раз"}`)
	}
	defer unlock()
	messageID, raw, err := b.db.BotCard(ctx, q.From.ID)
	var state cardState
	if err != nil || messageID != q.MessageID || json.Unmarshal(raw, &state) != nil || len(state.Pages) == 0 || state.Current >= len(state.Pages) || state.Current < 0 || b.viewMode(ctx, q.From.ID) != "cards" {
		return answer(`{"error":"Откройте актуальный экран: /menu"}`)
	}
	parts := strings.Split(q.Data, ":")
	if len(parts) != 4 || parts[0] != "ui" || parts[1] != state.Pages[state.Current].Revision {
		return answer(`{"error":"Экран обновился. Нажмите ещё раз."}`)
	}
	// Local clipboard/link actions get their payload only after checking the owner.
	if parts[2] == "a" {
		index, e := strconv.Atoi(parts[3])
		if e != nil || index < 0 || index >= len(state.Actions) {
			return answer(`{"error":"Действие недоступно"}`)
		}
		visible := state.Pages[state.Current].Back == index+1 || state.Pages[state.Current].Refresh == index+1
		for _, button := range state.Pages[state.Current].Buttons {
			if button.Index == index {
				visible = true
			}
		}
		if !visible {
			return answer(`{"error":"Действие недоступно"}`)
		}
		action := state.Actions[index]
		if action.CopyText != nil {
			copyCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			if err := b.sendCardCopy(copyCtx, q.From.ID, action.CopyText.Text); err != nil {
				return answer(`{"error":"Не удалось отправить ссылку. Попробуйте ещё раз."}`)
			}
			return answer(`{"notice":"Сообщение для копирования отправлено ниже"}`)
		}
		if action.CopyText != nil || action.URL != "" {
			if state.Version >= interfaceCardVersion {
				return answer(`{"error":"Откройте ссылку в обычном виде: /view"}`)
			}
			result := map[string]string{}
			if action.CopyText != nil {
				result["copy"] = action.CopyText.Text
			} else {
				result["url"] = action.URL
			}
			payload, _ := json.Marshal(result)
			if len(payload) > 1024 {
				return answer(`{"error":"Откройте ссылку в обычном режиме /view"}`)
			}
			return answer(string(payload))
		}
		if err = answer(`{}`); err != nil {
			return err
		}
		if strings.HasPrefix(action.CallbackData, "card:") {
			if err := b.cardAction(ctx, q, action.CallbackData); err != nil {
				return b.deliverUI(ctx, q.From.ID, q.MessageID, textCard("Не удалось выполнить действие\nПопробуйте ещё раз позже.", keyboard(b.backRow())))
			}
			return nil
		}
		// Reuse the same server-side business rules as the native keyboard.
		return b.handleCallback(ctx, telego.CallbackQuery{ID: fmt.Sprintf("card/%d/%d", q.From.ID, q.MessageID), From: q.From, Data: action.CallbackData, Message: &telego.Message{MessageID: q.MessageID, Chat: *q.Chat}})
	}
	if parts[2] == "p" {
		page, e := strconv.Atoi(parts[3])
		if e != nil || page < 0 || page >= len(state.Pages) {
			return answer(`{"error":"Страница недоступна"}`)
		}
		if err = answer(`{}`); err != nil {
			return err
		}
		state.Current = page
		return b.deliverUI(ctx, q.From.ID, q.MessageID, state)
	}
	if parts[2] == "n" {
		switch parts[3] {
		case "subscription", "devices", "support", "more":
		default:
			return answer(`{"error":"Раздел недоступен"}`)
		}
		if err = answer(`{}`); err != nil {
			return err
		}
		b.showScreen(ctx, telego.ChatID{ID: q.From.ID}, q.MessageID, parts[3], q.From.ID)
		return nil
	}
	return answer(`{"error":"Действие недоступно"}`)
}
