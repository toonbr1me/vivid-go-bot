// Package bot — Telegram-интерфейс сервиса: экраны, оплата, поддержка и
// администрирование. Бизнес-логика живёт в payments, здесь только разговоры.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"

	"github.com/toonbr1me/vivid-go-bot/internal/bot/texts"
	richmsg "github.com/toonbr1me/vivid-go-bot/internal/botui/legacy/transport"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/mdhtml"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// Виды ожидаемого ввода: ответ на вопрос нельзя распознать по тексту.
const (
	inputNone         = ""
	inputPromo        = "promo"
	inputTicket       = "ticket"
	inputTicketReply  = "ticket_reply"
	inputPollQuestion = "poll_question"
	inputPollOptions  = "poll_options"
	inputTimeout      = 20 * time.Minute
)

type inputState struct {
	kind string
	ref  string
	at   time.Time
}

// Bot связывает Telegram с сервисом. Поля с картами защищены мьютексом:
// обновления обрабатываются параллельно.
type Bot struct {
	api   *telego.Bot
	db    *storage.DB
	cfg   *config.Settings
	pay   *payments.Service
	texts *texts.Texts
	rich  *richmsg.Client

	mu                 sync.Mutex
	inputs             map[int64]inputState
	promos             map[int64]string
	widgetRefresh      map[int64]time.Time
	widgetsUnsupported bool
}

// New создаёт бота. Проверка токена остаётся клиенту telego.
func New(api *telego.Bot, db *storage.DB, cfg *config.Settings, pay *payments.Service) *Bot {
	bot := &Bot{
		api:    api,
		db:     db,
		cfg:    cfg,
		pay:    pay,
		inputs: map[int64]inputState{},
		promos: map[int64]string{},
	}
	if cfg != nil && cfg.RichMessages {
		bot.rich = richmsg.New(cfg.TelegramToken, "")
	}
	// Тексты читаются из файла: без него бот работает на встроенных.
	values, err := texts.Load(cfg.WebDir)
	if err != nil {
		slog.Error("бот: тексты не прочитаны, берутся встроенные", "err", err)
		values = textsPtr()
	}
	bot.texts = values
	return bot
}

// textsPtr — встроенные тексты на случай неудачного чтения файла.
func textsPtr() *texts.Texts {
	values := texts.Default()
	return &values
}

// text подставляет значения в текст бота.
//
// Бренд и валюта доступны всегда: их подставляют чаще всего, а искать их
// вручную в каждом экране незачем.
func (b *Bot) text(text string, data map[string]any) string {
	values := map[string]any{
		"Brand":    b.cfg.BrandName,
		"Currency": b.cfg.Currency,
	}
	for key, value := range data {
		values[key] = value
	}
	return b.texts.Render(text, values)
}

// Run запускает long polling и блокируется до отмены контекста.
//
// Обрыв связи с Telegram не должен ронять сервис: сайт, вебхуки и
// фоновые задачи работают без бота. Поэтому ошибка опроса — это пауза с
// переподключением, а не конец процесса.
func (b *Bot) Run(ctx context.Context) error {
	backoff := 5 * time.Second
	for {
		err := b.poll(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		if isFatalTokenError(err) {
			// Токен не принят: это ошибка настройки, её должен увидеть
			// человек, а не молчаливые повторы.
			b.logError("бот: Telegram не принимает токен", err)
			backoff = time.Minute
		} else {
			b.logError("бот: опрос прерван, переподключаемся", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

// poll выполняет один цикл long polling.
func (b *Bot) poll(ctx context.Context) error {
	updates, err := b.api.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{
		Timeout: 30,
		AllowedUpdates: []string{
			telego.MessageUpdates,
			telego.CallbackQueryUpdates,
			telego.PreCheckoutQueryUpdates,
			telego.WidgetQueryUpdates,
		},
	})
	if err != nil {
		return fmt.Errorf("long polling: %w", err)
	}
	handler, err := telegohandler.NewBotHandler(b.api, updates)
	if err != nil {
		return fmt.Errorf("обработчик обновлений: %w", err)
	}
	b.register(handler)
	go func() {
		<-ctx.Done()
		_ = handler.Stop()
	}()
	if err := b.setCommands(ctx); err != nil {
		// Меню команд — удобство, но человек не должен гадать, где оно.
		slog.Warn("бот: меню команд не выставлено", "err", err)
	}
	slog.Info("бот запущен")
	return handler.Start()
}

// isFatalTokenError — Telegram отверг токен.
func isFatalTokenError(err error) bool {
	var apiErr *telegoapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == 401 || apiErr.ErrorCode == 403
	}
	return false
}

// logError пишет ошибку бота.
func (b *Bot) logError(message string, err error) {
	slog.Error(message, "err", err)
}

// setCommands раскладывает меню команд. Ошибка не мешает работе: меню —
// удобство, а не условие приёма сообщений.
func (b *Bot) setCommands(ctx context.Context) error {
	commands := []telego.BotCommand{
		{Command: "start", Description: "Главное меню"},
		{Command: "menu", Description: "Открыть меню заново"},
		{Command: "buy", Description: "Выбрать тариф"},
		{Command: "subscription", Description: "Моя подписка"},
		{Command: "status", Description: "Состояние подписки"},
		{Command: "devices", Description: "Устройства"},
		{Command: "diagnostics", Description: "Проверить готовность к подключению"},
		{Command: "promo", Description: "Промокод"},
		{Command: "referral", Description: "Пригласить друга"},
		{Command: "support", Description: "Поддержка"},
		{Command: "info", Description: "О сервисе"},
		{Command: "weblogin", Description: "Войти на сайт"},
		{Command: "help", Description: "Команды"},
	}
	return b.api.SetMyCommands(ctx, &telego.SetMyCommandsParams{
		Commands: commands,
		// Тип области обязателен: без него Telegram отвечает «Unsupported
		// type specified» и меню команд не выставляется вовсе.
		Scope: &telego.BotCommandScopeDefault{Type: telego.ScopeTypeDefault},
	})
}

// register навешивает обработчики. Порядок важен: telegohandler выполняет
// только первый подошедший обработчик, поэтому команды идут до общего текста.
func (b *Bot) register(h *telegohandler.BotHandler) {
	h.HandleMessage(b.cleanCommand(b.handleStart), telegohandler.CommandEqual("start"))
	h.HandleMessage(b.cleanCommand(b.handleMenuCommand), telegohandler.CommandEqual("menu"))
	h.HandleMessage(b.cleanCommand(b.handleBuyCommand), telegohandler.CommandEqual("buy"))
	h.HandleMessage(b.cleanCommand(b.handleTrialCommand), telegohandler.CommandEqual("trial"))
	h.HandleMessage(b.cleanCommand(b.handleSubscriptionCommand), telegohandler.CommandEqual("subscription"))
	h.HandleMessage(b.cleanCommand(b.handleStatusCommand), telegohandler.CommandEqual("status"))
	h.HandleMessage(b.cleanCommand(b.handleViewCommand), telegohandler.CommandEqual("view"))
	h.HandleMessage(b.cleanCommand(b.handleDevicesCommand), telegohandler.CommandEqual("devices"))
	h.HandleMessage(b.cleanCommand(b.handleDiagnosticsCommand), telegohandler.CommandEqual("diagnostics"))
	h.HandleMessage(b.cleanCommand(b.handlePromoCommand), telegohandler.CommandEqual("promo"))
	h.HandleMessage(b.cleanCommand(b.handleReferralCommand), telegohandler.CommandEqual("referral"))
	h.HandleMessage(b.cleanCommand(b.handleSupportCommand), telegohandler.CommandEqual("support"))
	h.HandleMessage(b.cleanCommand(b.handleInfoCommand), telegohandler.CommandEqual("info"))
	h.HandleMessage(b.cleanCommand(b.handleWebLoginCommand), telegohandler.CommandEqual("weblogin"))
	h.HandleMessage(b.cleanCommand(b.handleHelpCommand), telegohandler.CommandEqual("help"))
	h.HandleMessage(b.cleanCommand(b.handlePaySupportCommand), telegohandler.CommandEqual("paysupport"))
	h.HandleMessage(b.cleanCommand(b.handleCancelCommand), telegohandler.CommandEqual("cancel"))

	h.HandleMessage(b.cleanCommand(b.handleAdmin), telegohandler.CommandEqual("admin"))
	h.HandleMessage(b.cleanCommand(b.handleGrant), telegohandler.CommandEqual("grant"))
	h.HandleMessage(b.cleanCommand(b.handleTickets), telegohandler.CommandEqual("tickets"))
	h.HandleMessage(b.cleanCommand(b.handleReply), telegohandler.CommandEqual("reply"))
	h.HandleMessage(b.cleanCommand(b.handleCloseTicketCommand), telegohandler.CommandEqual("close"))
	h.HandleMessage(b.cleanCommand(b.handleStats), telegohandler.CommandEqual("stats"))
	h.HandleMessage(b.cleanCommand(b.handlePollsCommand), telegohandler.CommandEqual("polls"))
	h.HandleMessage(b.cleanCommand(b.handlePartnersCommand), telegohandler.CommandEqual("partners"))

	h.HandleMessage(b.handleText, telegohandler.AnyMessageWithText())
	h.HandleMessage(b.handleSupportAttachment, telegohandler.AnyMessageWithMedia())
	h.HandleCallbackQuery(b.handleCallback)
	h.HandleWidgetQuery(b.handleWidgetQuery)
	h.Handle(b.handlePreCheckout, telegohandler.AnyPreCheckoutQuery())
	h.HandleMessage(b.handleSuccessfulPayment, telegohandler.SuccessPayment())
}

// ticketMode — кто отвечает на новое обращение: сотрудник или помощник.
//
// Помощник отвечает на типовые вопросы сам, но любое его молчание не
// должно выглядеть как игнорирование: при его включении сотрудник всё
// равно получает уведомление.
func (b *Bot) ticketMode() string {
	if b.cfg != nil && b.cfg.AssistantEnabled() {
		return storage.SupportModeAssistant
	}
	return storage.SupportModeHuman
}

// ---------------------------------------------------------------------------
// Отправка и правка сообщений
// ---------------------------------------------------------------------------
func (b *Bot) send(ctx context.Context, chatID telego.ChatID, text string, kb *telego.InlineKeyboardMarkup) {
	if err := b.deliver(ctx, chatID, 0, text, kb); err != nil {
		slog.Error("не удалось отправить сообщение", "chat", chatID, "err", err)
	}
}

// SendText — уведомление фоновой задачи. Ошибка возвращается вызывающему:
// напоминание без доставки не должно считаться отправленным.
func (b *Bot) SendText(ctx context.Context, telegramID int64, text string) error {
	ctx = context.WithValue(ctx, keepMessageKey{}, true)
	// Notifications do not replace the user's active interactive screen.
	if b.viewMode(ctx, telegramID) == "cards" {
		return b.deliverLegacy(ctx, telego.ChatID{ID: telegramID}, 0, text, nil)
	}
	return b.deliver(ctx, telego.ChatID{ID: telegramID}, 0, text, nil)
}

func (b *Bot) edit(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) error {
	return b.deliver(ctx, chatID, messageID, text, kb)
}

// deliver отправляет новое сообщение или правит прежнее.
//
// Если Telegram не принял разметку — например, текст собран из чужих данных
// и в нём оказался непарный тег — сообщение повторяется простым текстом.
// Лучше без выделения, чем никак: человек ждёт ответа.
func (b *Bot) deliver(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) error {
	mode := b.viewMode(ctx, chatID.ID)
	if mode == "cards" {
		if err := b.deliverCard(ctx, chatID, messageID, text, kb); err == nil {
			return nil
		} else {
			slog.Warn("HTML-карточка не обновлена", "err", err)
			return err
		}
	}
	if mode != "rich" && mode != "classic" {
		return b.deliverLegacy(ctx, chatID, messageID, text, kb)
	}
	if ok, err := b.deliverRich(ctx, chatID, messageID, screenRichHTML(text), kb); ok {
		return nil
	} else if err != nil {
		slog.Warn("rich-сообщение не принято, использую HTML", "err", err)
	}
	return b.deliverLegacy(ctx, chatID, messageID, text, kb)
}

// deliverLegacy не пробует rich повторно после отказа или разбиения Markdown.
func (b *Bot) deliverLegacy(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) error {
	err := b.deliverHTML(ctx, chatID, messageID, text, kb)
	if err == nil || !isMarkupError(err) {
		return err
	}
	plain := mdhtml.Plain(text)
	if plain == "" || plain == text {
		return err
	}
	slog.Warn("разметка не принята Telegram, отправляю текстом", "err", err)
	return b.deliverPlain(ctx, chatID, messageID, plain, kb)
}

func (b *Bot) deliverHTML(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) error {
	messageID = b.nativeTarget(ctx, chatID.ID, messageID)
	if messageID == 0 {
		message, err := b.api.SendMessage(ctx, &telego.SendMessageParams{
			ChatID:             chatID,
			Text:               text,
			ParseMode:          telego.ModeHTML,
			LinkPreviewOptions: disabledPreview(),
			ReplyMarkup:        kb,
		})
		if err == nil {
			return b.rememberNative(ctx, chatID.ID, message.MessageID)
		}
		return err
	}
	_, err := b.api.EditMessageText(ctx, &telego.EditMessageTextParams{
		ChatID:             chatID,
		MessageID:          messageID,
		Text:               text,
		ParseMode:          telego.ModeHTML,
		LinkPreviewOptions: disabledPreview(),
		ReplyMarkup:        kb,
	})
	err = ignoreNotModified(err)
	if err == nil {
		markDelivered(ctx)
	}
	return err
}

func (b *Bot) deliverPlain(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) error {
	messageID = b.nativeTarget(ctx, chatID.ID, messageID)
	if messageID == 0 {
		message, err := b.api.SendMessage(ctx, &telego.SendMessageParams{
			ChatID:             chatID,
			Text:               text,
			LinkPreviewOptions: disabledPreview(),
			ReplyMarkup:        kb,
		})
		if err == nil {
			return b.rememberNative(ctx, chatID.ID, message.MessageID)
		}
		return err
	}
	_, err := b.api.EditMessageText(ctx, &telego.EditMessageTextParams{
		ChatID:             chatID,
		MessageID:          messageID,
		Text:               text,
		LinkPreviewOptions: disabledPreview(),
		ReplyMarkup:        kb,
	})
	err = ignoreNotModified(err)
	if err == nil {
		markDelivered(ctx)
	}
	return err
}

// ignoreNotModified — правка тем же текстом не ошибка.
func ignoreNotModified(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "message is not modified") {
		return nil
	}
	return err
}

// richAvailable — можно ли пробовать богатые сообщения.
func (b *Bot) richAvailable() bool {
	return b != nil && b.rich != nil && b.cfg != nil && b.cfg.RichMessages && b.rich.Available()
}

// deliverRich пытается отправить rich-сообщение.
//
// Первое значение — принял ли Telegram сообщение. false означает, что
// вызывающему стоит взять обычный путь: HTML, текст или разбиение на части.
// Кнопки обычной клавиатуры переносятся внутрь сообщения, чтобы они были
// частью текста; если хоть одна кнопка не выражается rich-тегом, клавиатура
// остаётся обычной.
func (b *Bot) deliverRich(ctx context.Context, chatID telego.ChatID, messageID int, html string, kb *telego.InlineKeyboardMarkup) (bool, error) {
	if !b.richAvailable() || strings.TrimSpace(html) == "" || chatID.ID == 0 {
		return false, nil
	}
	var markup any
	if rows, ok := richButtonRows(kb); ok {
		html = strings.TrimRight(html, "\n") + "\n" + rows
	} else if kb != nil {
		markup = kb
	}

	message := richmsg.Message{HTML: html}
	var err error
	messageID = b.nativeTarget(ctx, chatID.ID, messageID)
	if messageID == 0 {
		var sentID int64
		sentID, err = b.rich.Send(ctx, chatID.ID, message, markup)
		if err == nil {
			err = b.rememberNative(ctx, chatID.ID, int(sentID))
		}
	} else {
		err = b.rich.Edit(ctx, chatID.ID, messageID, message, markup)
	}
	switch {
	case err == nil, richmsg.IsNotModified(err):
		markDelivered(ctx)
		return true, nil
	case richmsg.IsUnsupported(err):
		slog.Warn("богатые сообщения не поддерживаются, остаюсь на HTML", "err", err)
		return false, nil
	default:
		return false, err
	}
}

// SendRichMarkdown отправляет Markdown богатым сообщением с запасным путём.
func (b *Bot) SendRichMarkdown(ctx context.Context, telegramID int64, markdown string) error {
	return b.deliverMarkdown(ctx, telego.ChatID{ID: telegramID}, 0, markdown, nil)
}

// deliverMarkdown показывает ответ модели или сотрудника.
//
// Сначала пробуем rich-сообщение: заголовки, списки и таблицы в нём выглядят
// как в обычном документе. Если сервер их не принял — делим Markdown на
// части и отправляем обычным HTML, чтобы длинный ответ дошёл целиком.
func (b *Bot) deliverMarkdown(ctx context.Context, chatID telego.ChatID, messageID int, markdown string, kb *telego.InlineKeyboardMarkup) error {
	if strings.TrimSpace(markdown) == "" {
		return nil
	}
	mode := b.viewMode(ctx, chatID.ID)
	if mode == "cards" {
		return b.deliverCard(ctx, chatID, messageID, mdhtml.Render(markdown), kb)
	}
	if (mode == "rich" || mode == "classic") && b.richAvailable() {
		if ok, err := b.deliverRich(ctx, chatID, messageID, mdhtml.Rich(markdown), kb); ok {
			return nil
		} else if err != nil {
			slog.Warn("rich-ответ не доставлен, отправляю обычный HTML", "err", err)
		}
	}
	parts := mdhtml.Chunks(markdown, mdhtml.DefaultLimit)
	for index, part := range parts {
		target := messageID
		var markup *telego.InlineKeyboardMarkup
		if index == 0 {
			markup = kb
		}
		if index > 0 {
			target = 0
		}
		if err := b.deliverLegacy(ctx, chatID, target, part, markup); err != nil {
			return err
		}
	}
	return nil
}

// isMarkupError — Telegram не смог разобрать HTML сообщения.
func isMarkupError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"can't parse entities",
		"unsupported start tag",
		"can't find end tag",
		"unclosed",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

// show рисует экран на месте прежнего сообщения. Если правка не удалась
// (сообщение слишком старое или удалено), отправляет новое — человек не
// должен остаться без ответа на нажатие.
func (b *Bot) show(ctx context.Context, chatID telego.ChatID, messageID int, text string, kb *telego.InlineKeyboardMarkup) {
	if err := b.edit(ctx, chatID, messageID, text, kb); err != nil {
		if b.db != nil && b.viewMode(ctx, chatID.ID) == "cards" {
			id, _, _ := b.db.BotCard(ctx, chatID.ID)
			if id == messageID {
				slog.Warn("не удалось обновить карточку", "err", err)
				return
			}
		}
		b.send(ctx, chatID, text, kb)
	}
}

func (b *Bot) answer(ctx context.Context, queryID, text string, alert bool) {
	if strings.HasPrefix(queryID, "card/") {
		if alert && text != "" {
			parts := strings.Split(queryID, "/")
			if len(parts) == 3 {
				id, _ := strconv.ParseInt(parts[1], 10, 64)
				messageID, _ := strconv.Atoi(parts[2])
				b.upsert(ctx, telego.ChatID{ID: id}, messageID, "<b>Не удалось выполнить действие</b>\n\n"+escapeHTML(text), keyboard(b.backRow()))
			}
		}
		return
	}
	if queryID == "" {
		return
	}
	params := &telego.AnswerCallbackQueryParams{CallbackQueryID: queryID, Text: text}
	if alert {
		params.ShowAlert = true
	}
	if err := b.api.AnswerCallbackQuery(ctx, params); err != nil {
		slog.Debug("ответ на callback не доставлен", "err", err)
	}
}

func disabledPreview() *telego.LinkPreviewOptions {
	return &telego.LinkPreviewOptions{IsDisabled: true}
}

// ---------------------------------------------------------------------------
// Общие помощники
// ---------------------------------------------------------------------------

// username — имя пользователя панели для Telegram-аккаунта.
func telegramUsername(tgID int64) string { return fmt.Sprintf("tg_%d", tgID) }

func (b *Bot) subject(ctx context.Context, tgID int64) (domain.Subject, error) {
	return b.db.EnsureTelegramSubject(ctx, tgID)
}

// accessFor собирает подписку. Ошибка панели не скрывает срок из последнего
// заказа: Access уже подставил его.
func (b *Bot) accessFor(ctx context.Context, subject domain.Subject) (domain.Access, error) {
	return b.pay.Access(ctx, subject, telegramUsername(*subject.TelegramID))
}

func (b *Bot) trialAvailable(ctx context.Context, subject domain.Subject) bool {
	used, err := b.db.HasTrial(ctx, subject.ID, "bot")
	return err != nil || !used
}

// currentPromo — код, введённый в этом сеансе. Он не тратится до оплаты.
func (b *Bot) currentPromo(tgID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.promos[tgID]
}

func (b *Bot) setPromo(tgID int64, code string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if code == "" {
		delete(b.promos, tgID)
		return
	}
	b.promos[tgID] = code
}

// setInput запоминает, что бот ждёт от человека текст. ref — необязательная
// привязка (номер обращения для ответа сотрудника).
func (b *Bot) setInput(tgID int64, kind, ref string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if kind == inputNone {
		delete(b.inputs, tgID)
		return
	}
	b.inputs[tgID] = inputState{kind: kind, ref: ref, at: time.Now()}
}

// input возвращает вид ожидаемого ввода и привязку. Просроченный ввод
// забывается: через двадцать минут человек уже не помнит, о чём бот спросил.
func (b *Bot) input(tgID int64) (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state, ok := b.inputs[tgID]
	if !ok || time.Since(state.at) > inputTimeout {
		delete(b.inputs, tgID)
		return inputNone, ""
	}
	return state.kind, state.ref
}

func (b *Bot) clearInput(tgID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inputs, tgID)
}

// isStaff отвечает на вопрос «можно ли этому человеку админку».
func (b *Bot) isStaff(ctx context.Context, tgID int64) bool {
	return b.db.Permission(ctx, tgID, "tickets.view", b.cfg.AdminIDs)
}

// isOwner — владелец сервиса: он один, отвечает за деньги и рассылки.
func (b *Bot) isOwner(ctx context.Context, tgID int64) bool {
	return b.cfg != nil && b.cfg.AdminIDs[tgID]
}

func quoteUsername(user telego.User) string {
	if strings.TrimSpace(user.Username) != "" {
		return user.Username
	}
	return telegramUsername(user.ID)
}

func firstRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func isNotFound(err error) bool { return errors.Is(err, storage.ErrNotFound) }
