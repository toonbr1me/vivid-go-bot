// Package testdb — общая обвязка для тестов, которым нужна настоящая база.
//
// Тесты идут против отдельной базы: переменная TEST_DATABASE_URL. Если её
// нет, тесты с базой пропускаются — а не падают и не трогают рабочую.
//
// У каждого пакета своя схема в этой базе: пакеты тестов Go идут параллельно,
// а общая таблица приводит к взаимным блокировкам и взаимной порче данных.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// EnvURL — имя переменной с адресом тестовой базы.
const EnvURL = "TEST_DATABASE_URL"

// URL — адрес тестовой базы. Пусто — тестов с базой быть не должно.
func URL() string {
	return strings.TrimSpace(os.Getenv(EnvURL))
}

func validateTestURL(raw string) error {
	target, err := url.Parse(raw)
	if err != nil || target == nil || (target.Scheme != "postgres" && target.Scheme != "postgresql") {
		return fmt.Errorf("%s: нужна postgres URL отдельной тестовой базы", EnvURL)
	}
	name := strings.ToLower(strings.TrimPrefix(target.Path, "/"))
	if !strings.HasPrefix(name, "test") && !strings.HasSuffix(name, "_test") && !strings.Contains(name, "_test_") {
		return fmt.Errorf("%s: имя базы должно обозначать тестовую БД (test... или ..._test)", EnvURL)
	}
	if strings.Contains(name, "/") || target.Fragment != "" {
		return fmt.Errorf("%s: неверное имя тестовой базы", EnvURL)
	}
	return nil
}

// schemaFor — схема текущего пакета: имя каталога, где лежит тест.
//
// Ищется первый вызов не из самого testdb: иначе схема была бы общей для
// всех пакетов, и тесты снова начали бы мешать друг другу.
func schemaFor() string {
	for depth := 1; depth < 8; depth++ {
		_, file, _, ok := runtime.Caller(depth)
		if !ok {
			break
		}
		if strings.Contains(filepath.ToSlash(file), "/internal/testdb/") {
			continue
		}
		name := filepath.Base(filepath.Dir(file))
		name = strings.NewReplacer("-", "_", ".", "_").Replace(name)
		return name + "_test"
	}
	return "test"
}

// Open открывает тестовую базу, применяет схему и очищает таблицы.
//
// Схема пакета пересоздаётся один раз за прогон: тесты начинают с чистого
// листа, и остатки прошлого прогона (или прошлой версии миграций) не могут
// повлиять на результат. Дальше таблицы просто очищаются, поэтому порядок
// выполнения тестов тоже ничего не меняет.
func Open(t *testing.T) *storage.DB {
	t.Helper()
	url := URL()
	if url == "" {
		if os.Getenv("REQUIRE_TEST_DATABASE") == "true" {
			t.Fatalf("%s обязателен: интеграционные тесты нельзя пропускать", EnvURL)
		}
		t.Skipf("%s не задан: тест с базой пропущен", EnvURL)
	}
	if err := validateTestURL(url); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	schema := schemaFor()
	resetOnce(schema, func() error { return resetSchema(ctx, url, schema) }, t)
	db, err := storage.Open(ctx, withSearchPath(url, schema))
	if err != nil {
		t.Fatalf("тестовая база не открылась: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	Truncate(t, db)
	return db
}

// resetOnce пересоздаёт схему один раз за процесс: каждый тест пересоздавать
// её заново слишком долго, а один раз на пакет достаточно.
func resetOnce(schema string, reset func() error, t *testing.T) {
	t.Helper()
	_, loaded := resetSchemas.LoadOrStore(schema, false)
	if !loaded {
		if err := reset(); err != nil {
			t.Fatalf("схема %s не пересоздалась: %v", schema, err)
		}
		resetSchemas.Store(schema, true)
	}
}

// resetSchemas — какие схемы уже пересозданы в этом процессе.
var resetSchemas sync.Map

// resetSchema пересоздаёт схему пакета с нуля.
//
// Подключение обычное, без storage.Open: тот держит миграции, а они
// блокируют таблицы общей схемы и конфликтуют с соседними пакетами.
func resetSchema(ctx context.Context, url, schema string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `CREATE SCHEMA `+quoteIdent(schema))
	return err
}

// withSearchPath добавляет схему в строку подключения.
//
// Имя схемы идёт без кавычек: это значение параметра, а не идентификатор
// в SQL. С кавычками Postgres искал бы схему вместе с ними и молча
// вернулся к public — тесты пакетов снова делили бы одни таблицы.
func withSearchPath(baseURL, schema string) string {
	separator := "?"
	if strings.Contains(baseURL, "?") {
		separator = "&"
	}
	return baseURL + separator + "search_path=" + url.QueryEscape(schema)
}

// quoteIdent — имя схемы в кавычках: в имени могут быть точки.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Truncate очищает все таблицы и перезапускает счётчики.
func Truncate(t *testing.T, db *storage.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.Pool.Exec(ctx, `TRUNCATE TABLE
		accounts, orders, trials, promo_activations, promocodes, promo_intents,
		referrals, pending_referral_bonuses, user_balance, user_referral_mode,
		withdrawal_requests, reminder_log, notification_settings, support_tickets,
		support_messages, support_notifications, web_support_requests,
		device_limit_sync_state, staff, staff_audit, web_sessions,
		telegram_link_codes, partners, partner_attributions, partner_rewards,
		partner_invite_codes, plans, device_packs, device_slot_grants,
		device_slot_renewals, app_settings, order_fulfillment_steps,
		order_notification_log, polls, poll_deliveries, poll_answers,
		funnel_daily, feature_requests, feature_request_votes, payment_inbox, bot_ui_preferences
		RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("не удалось очистить базу: %v", err)
	}
}
