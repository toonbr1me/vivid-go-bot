package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// ErrNotFound — строки нет.
var ErrNotFound = errors.New("не найдено")

const subjectColumns = `id, telegram_id, email, name, google_sub, remnawave_username,
	source, ref_code, merged_into, created_at`

func scanSubject(row pgx.Row) (domain.Subject, error) {
	var subject domain.Subject
	err := row.Scan(
		&subject.ID, &subject.TelegramID, &subject.Email, &subject.Name,
		&subject.GoogleSub, &subject.RemnawaveUser, &subject.Source,
		&subject.RefCode, &subject.MergedInto, &subject.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Subject{}, ErrNotFound
	}
	return subject, err
}

// EnsureTelegramSubject возвращает субъекта для Telegram-пользователя,
// создавая его при первом обращении. Субъект — единый владелец подписки,
// заказов и обращений, у веб-аккаунта он тот же самый.
func (db *DB) EnsureTelegramSubject(ctx context.Context, telegramID int64) (domain.Subject, error) {
	row := db.Pool.QueryRow(ctx, `
		INSERT INTO accounts (telegram_id, source, ref_code)
		VALUES ($1, 'telegram', $2)
		ON CONFLICT (telegram_id) DO UPDATE SET telegram_id = EXCLUDED.telegram_id
		RETURNING `+subjectColumns,
		telegramID, newRefCode())
	return scanSubject(row)
}

// SubjectByTelegramID ищет субъекта без создания.
func (db *DB) SubjectByTelegramID(ctx context.Context, telegramID int64) (domain.Subject, error) {
	row := db.Pool.QueryRow(ctx, `SELECT `+subjectColumns+` FROM accounts WHERE telegram_id = $1`, telegramID)
	return scanSubject(row)
}

// SubjectByRefCode ищет пригласившего по реферальному коду.
func (db *DB) SubjectByRefCode(ctx context.Context, code string) (domain.Subject, error) {
	row := db.Pool.QueryRow(ctx, `SELECT `+subjectColumns+` FROM accounts WHERE ref_code = $1`, code)
	return scanSubject(row)
}

// SubjectByID читает субъекта по идентификатору.
func (db *DB) SubjectByID(ctx context.Context, id int64) (domain.Subject, error) {
	return subjectByID(ctx, db.Pool, id)
}

func subjectByID(ctx context.Context, q Querier, id int64) (domain.Subject, error) {
	row := q.QueryRow(ctx, `SELECT `+subjectColumns+` FROM accounts WHERE id = $1`, id)
	return scanSubject(row)
}

// SubjectByGoogleSub ищет веб-аккаунт по Google sub, не создавая его.
func (db *DB) SubjectByGoogleSub(ctx context.Context, googleSub string) (domain.Subject, error) {
	row := db.Pool.QueryRow(ctx, `SELECT `+subjectColumns+` FROM accounts WHERE google_sub = $1`, googleSub)
	return scanSubject(row)
}

// UpsertGoogleSubject создаёт или обновляет веб-аккаунт по Google sub.
func (db *DB) UpsertGoogleSubject(ctx context.Context, googleSub, email, name string) (domain.Subject, error) {
	row := db.Pool.QueryRow(ctx, `
		INSERT INTO accounts (google_sub, email, name, source, ref_code, last_login_at)
		VALUES ($1, $2, $3, 'web', $4, now())
		ON CONFLICT (google_sub) DO UPDATE
		   SET email = EXCLUDED.email, name = EXCLUDED.name, last_login_at = now()
		RETURNING `+subjectColumns,
		googleSub, email, name, newRefCode())
	return scanSubject(row)
}

// AttachTelegram привязывает Telegram к веб-аккаунту.
func (db *DB) AttachTelegram(ctx context.Context, subjectID, telegramID int64) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE accounts SET telegram_id = $2 WHERE id = $1 AND telegram_id IS NULL`,
		subjectID, telegramID)
	return err
}

// SetRemnawaveUsername запоминает имя подписки в панели.
func (db *DB) SetRemnawaveUsername(ctx context.Context, subjectID int64, username string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE accounts SET remnawave_username = $2 WHERE id = $1`, subjectID, username)
	return err
}

// CreateWebSession сохраняет только хеш токена.
func (db *DB) CreateWebSession(ctx context.Context, tokenHash string, subjectID int64, ttl time.Duration) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO web_sessions (token_hash, account_id, expires_at)
		VALUES ($1, $2, $3)`, tokenHash, subjectID, time.Now().Add(ttl))
	return err
}

// SubjectBySession ищет владельца живой сессии.
//
// Колонки указаны с таблицей: у accounts и web_sessions есть одинаковые
// created_at, и без уточнения Postgres отвечает «неоднозначно».
func (db *DB) SubjectBySession(ctx context.Context, tokenHash string) (domain.Subject, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT a.id, a.telegram_id, a.email, a.name, a.google_sub, a.remnawave_username,
		       a.source, a.ref_code, a.merged_into, a.created_at
		FROM accounts a
		 JOIN web_sessions s ON s.account_id = a.id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash)
	return scanSubject(row)
}

// DeleteWebSession завершает сессию.
func (db *DB) DeleteWebSession(ctx context.Context, tokenHash string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM web_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// CreateLinkCode выдаёт одноразовый код привязки Telegram. Старые коды
// субъекта удаляются: одновременно живым должен быть ровно один.
func (db *DB) CreateLinkCode(ctx context.Context, subjectID int64) (string, error) {
	if _, err := db.Pool.Exec(ctx, `
		DELETE FROM telegram_link_codes WHERE account_id = $1`,
		subjectID); err != nil {
		return "", err
	}
	code := randomHex(8)
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO telegram_link_codes (code, account_id, expires_at)
		VALUES ($1, $2, $3)`, code, subjectID, time.Now().Add(30*time.Minute))
	return code, err
}

// PeekLinkCode ищет живой код, не помечая его использованным. Нужен, чтобы
// проверить привязку до того, как код сгорит.
func (db *DB) PeekLinkCode(ctx context.Context, code string) (*int64, error) {
	var accountID int64
	err := db.Pool.QueryRow(ctx, `
		SELECT account_id FROM telegram_link_codes
		WHERE code = $1 AND used_at IS NULL AND expires_at > now()`, code).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &accountID, nil
}

// ConsumeLinkCode забирает код; nil — код не найден или истёк.
func (db *DB) ConsumeLinkCode(ctx context.Context, code string) (*int64, error) {
	var accountID int64
	err := db.Pool.QueryRow(ctx, `
		UPDATE telegram_link_codes SET used_at = now()
		WHERE code = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING account_id`, code).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &accountID, nil
}

func newRefCode() string {
	return randomHex(5)
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "ref" + time.Now().Format("150405")
	}
	return hex.EncodeToString(buf)
}
