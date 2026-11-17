// Command vivid поднимает сервис целиком: бота, сайт с вебхуками и
// фоновые задачи.
//
// Проводка ручная и нарочно плоская. Здесь видно, из чего собран процесс и
// в каком порядке он останавливается, — при отладке это важнее, чем
// экономия десяти строк на контейнере зависимостей.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mymmrac/telego"
	"golang.org/x/sync/errgroup"

	"github.com/toonbr1me/vivid-go-bot/internal/bot"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/jobs"
	"github.com/toonbr1me/vivid-go-bot/internal/llm"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/support"
	"github.com/toonbr1me/vivid-go-bot/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("сервис остановлен с ошибкой", "err", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger()
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Локальный запуск берёт настройки из .env; в контейнере файла нет, и
	// переменные приходят из compose.
	if err := loadDotEnv(".env"); err != nil {
		return fmt.Errorf("файл .env: %w", err)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL не задан")
	}

	// Схема применяется до чтения настроек: переопределения живут в базе,
	// и на первом запуске таблицы app_settings ещё нет.
	db, err := storage.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("база данных: %w", err)
	}
	defer db.Close()

	overlay, err := db.AppSettings(ctx)
	if err != nil {
		return fmt.Errorf("настройки из базы: %w", err)
	}
	cfg, err := config.Load(overlay)
	if err != nil {
		return fmt.Errorf("настройки: %w", err)
	}
	cfg.EnableRuntime()
	if cfg.Timezone != "" {
		location, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return fmt.Errorf("часовой пояс: %w", err)
		}
		time.Local = location
	}
	if err := db.SeedDefaults(ctx, seedPlans(), seedPacks()); err != nil {
		return fmt.Errorf("стартовый каталог: %w", err)
	}

	api, err := telego.NewBot(cfg.TelegramToken, telego.WithLogger(botLogger{log: logger}))
	if err != nil {
		return fmt.Errorf("клиент Telegram: %w", err)
	}
	resolveBotUsername(ctx, api, cfg, logger)

	// Панель — интерфейс, а не указатель: иначе в него попал бы nil внутри
	// интерфейса, и проверка «панели нет» проходила бы, а вызов падал бы.
	var panel payments.Panel
	if cfg.APIBaseURL != "" && cfg.APIToken != "" {
		client := remnawave.New(cfg.APIBaseURL, cfg.APIToken,
			time.Duration(cfg.RequestTimeoutSecs*float64(time.Second)))
		panel = client
		if cfg.RemnawaveNamespace != "" {
			scoped, err := remnawave.NewScoped(client, cfg.RemnawaveNamespace, cfg.SubscriptionBaseURL)
			if err != nil {
				return err
			}
			panel = scoped
			logger.Info("отдельные профили панели", "namespace", cfg.RemnawaveNamespace)
		}
	} else {
		logger.Warn("панель не настроена: покупки не смогут выдать доступ")
	}

	registry, err := payments.BuildRegistry(cfg, api)
	if err != nil {
		return fmt.Errorf("платёжные шлюзы: %w", err)
	}
	pay := payments.New(db, panel, registry, cfg)
	if !registry.Enabled() {
		logger.Warn("платежи выключены: PAYMENT_PROVIDERS пуст")
	}

	telegramBot := bot.New(api, db, cfg, pay)
	site, err := web.New(db, pay, cfg, logger, telegramBot)
	if err != nil {
		return fmt.Errorf("сайт: %w", err)
	}
	// Живые обновления обращений: страницы подписываются на поток, а
	// слушатель будит их по уведомлениям Postgres.
	site.StartLive(ctx)

	// Помощник поддержки отвечает на типовые вопросы сам, если модель
	// настроена и включена.
	var assistant *support.Assistant
	if cfg.LLMEnabled {
		client := llm.New(cfg.LLMURL, cfg.LLMAPIKey, cfg.LLMModel,
			time.Duration(cfg.LLMTimeout)*time.Second)
		if !client.Enabled() {
			logger.Warn("помощник включён, но модель не настроена: SUPPORT_LLM_MODEL пуст")
		} else {
			assistant = support.New(db, client, cfg, pay.Facts(),
				support.LoadKnowledge(cfg.WebDir), telegramBot, logger)
			logger.Info("помощник поддержки включён", "модель", client.Model())
		}
	}

	logger.Info("сервис запускается",
		"brand", cfg.BrandName,
		"site", cfg.SiteBaseURL,
		"listen", fmt.Sprintf("%s:%d", cfg.WebhookHost, cfg.WebhookPort),
		"providers", cfg.PaymentProviders,
	)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return ignoreCancel(telegramBot.Run(groupCtx)) })
	group.Go(func() error { return ignoreCancel(site.Run(groupCtx)) })
	group.Go(func() error {
		return ignoreCancel(jobs.RunWithAssistant(groupCtx, db, pay, telegramBot, cfg, assistant))
	})

	if err := group.Wait(); err != nil {
		return err
	}
	logger.Info("сервис остановлен")
	return nil
}

// ignoreCancel переводит штатную остановку в nil: отмена контекста — не
// ошибка, а способ, которым задачи договариваются о завершении.
func ignoreCancel(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// resolveBotUsername подставляет @username в настройки, если администратор
// его не задал: без него ссылки-приглашения и deep link на привязку сайта
// собрать не из чего. Сбой сети не мешает запуску — просто не будет ссылок.
func resolveBotUsername(ctx context.Context, api *telego.Bot, cfg *config.Settings, logger *slog.Logger) {
	if cfg.BotUsername != "" {
		return
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	me, err := api.GetMe(timeoutCtx)
	if err != nil {
		logger.Warn("не удалось узнать username бота", "err", err)
		return
	}
	cfg.BotUsername = me.Username
	logger.Info("username бота определён", "username", cfg.BotUsername)
}

// seedPlans — каталог, который раньше был захардкожен в коде. Он попадает
// в базу только на пустой таблице: дальше цены живут в админке.
func seedPlans() []storage.PlanSeed {
	return []storage.PlanSeed{
		{ID: "30d", Title: "1 месяц", Days: 30, PriceRoubles: 100, SortOrder: 10},
		{ID: "90d", Title: "3 месяца", Days: 90, PriceRoubles: 250, SortOrder: 20},
		{ID: "180d", Title: "6 месяцев", Days: 180, PriceRoubles: 450, SortOrder: 30},
	}
}

func seedPacks() []storage.PackSeed {
	return []storage.PackSeed{
		{ID: "dev1", Title: "+1 устройство", Devices: 1, PriceRoubles: 30, SortOrder: 10},
		{ID: "dev3", Title: "+3 устройства", Devices: 3, PriceRoubles: 70, SortOrder: 20},
		{ID: "dev5", Title: "+5 устройств", Devices: 5, PriceRoubles: 100, SortOrder: 30},
	}
}

// newLogger пишет в stdout разборчивым текстом: в контейнере это всё равно
// уедет в сборщик логов, а человеку в терминале JSON читать нечем.
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

// botLogger переводит два метода telego в обычные записи slog.
type botLogger struct {
	log *slog.Logger
}

func (l botLogger) Debugf(format string, args ...any) {
	l.log.Debug(fmt.Sprintf(format, args...))
}

func (l botLogger) Errorf(format string, args ...any) {
	l.log.Error(fmt.Sprintf(format, args...))
}
