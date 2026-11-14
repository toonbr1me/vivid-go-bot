package storage

import (
	"context"
	"fmt"
)

// subjectMigrationLock — advisory-ключ стартовой миграции. Под ним
// договариваются копии приложения: выполняет её ровно одна.
const subjectMigrationLock = 0x5253424D // 'RSBM'

// subjectBackfill — таблица, у которой старые строки привязываются к
// субъекту: откуда берём telegram_id, куда кладём subject_id и на какие
// колонки ложится уникальный индекс (пусто — индекса нет, конфликт
// невозможен).
type subjectBackfill struct {
	table     string
	sourceCol string
	targetCol string
	unique    []string
}

// subjectBackfills — перенос subject_id на базе, оставшейся от прошлой
// версии.
//
// Уникальный индекс мешает заполнению: слияние субъектов оставляет старую
// строку в trials с чужим telegram_id, и новая запись претендует на тот же
// subject_id и kind. Без проверки такая строка обрывает миграцию
// исключением, а сервис падает на ней снова и снова: миграция выполняется
// при каждом старте. Пропущенная строка остаётся с NULL — это
// восстановимо; неподнимающийся сервис — нет.
var subjectBackfills = []subjectBackfill{
	{"trials", "telegram_id", "subject_id", []string{"kind"}},
	{"user_balance", "telegram_id", "subject_id", nil},
	{"user_referral_mode", "telegram_id", "subject_id", nil},
	{"promo_activations", "telegram_id", "subject_id", nil},
	{"pending_referral_bonuses", "referrer_id", "subject_id", nil},
	{"withdrawal_requests", "telegram_id", "subject_id", nil},
	{"referrals", "referrer_id", "referrer_subject_id", nil},
	{"referrals", "referred_id", "referred_subject_id", nil},
}

// MigrateSubjects приводит старую базу к текущей схеме.
//
// Субъект — единый владелец подписки, заказов, баланса и обращений. В базе,
// оставшейся от прошлой версии, у строк есть только telegram_id: без
// переноса они остались бы не связанными с субъектом, и человек получил бы
// второй пробный период, пустой баланс и пустую историю заказов.
//
// Миграция идемпотентна: второй проход не находит ничего и меняет одну
// пустую строку.
func (db *DB) MigrateSubjects(ctx context.Context) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("миграция субъектов: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, subjectMigrationLock); err != nil {
		return fmt.Errorf("миграция субъектов: %w", err)
	}

	// 1. Субъект каждому, кто известен базе. Telegram-колонка уникальна, так
	// что повторный проход ничего не создаёт.
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (telegram_id, source)
		SELECT telegram_id, 'telegram' FROM (
			SELECT telegram_id FROM orders WHERE telegram_id IS NOT NULL
			UNION SELECT telegram_id FROM trials WHERE telegram_id IS NOT NULL
			UNION SELECT telegram_id FROM user_balance WHERE telegram_id IS NOT NULL
			UNION SELECT telegram_id FROM user_referral_mode WHERE telegram_id IS NOT NULL
			UNION SELECT telegram_id FROM promo_activations WHERE telegram_id IS NOT NULL
			UNION SELECT referrer_id FROM pending_referral_bonuses WHERE referrer_id IS NOT NULL
			UNION SELECT telegram_id FROM withdrawal_requests WHERE telegram_id IS NOT NULL
			UNION SELECT referrer_id FROM referrals WHERE referrer_id IS NOT NULL
			UNION SELECT referred_id FROM referrals WHERE referred_id IS NOT NULL
			UNION SELECT telegram_id FROM support_tickets WHERE telegram_id IS NOT NULL
		) AS known
		WHERE telegram_id IS NOT NULL
		ON CONFLICT (telegram_id) DO NOTHING`); err != nil {
		return fmt.Errorf("миграция субъектов: %w", err)
	}

	// 2. Проставляем subject_id там, где он ещё пуст. Уже заполненный не
	// трогаем: после слияния субъектов он указывает не на того, кого нашли
	// бы по telegram_id, и это правильный ответ.
	for _, item := range subjectBackfills {
		guard := ""
		if item.unique != nil {
			extra := ""
			for _, col := range item.unique {
				extra += fmt.Sprintf(" AND d.%s = t.%s", col, col)
			}
			guard = fmt.Sprintf(`
			  AND NOT EXISTS (
			      SELECT 1 FROM %s d WHERE d.%s = a.id%s
			  )`, item.table, item.targetCol, extra)
		}
		query := fmt.Sprintf(`
			UPDATE %s AS t SET %s = a.id
			FROM accounts a
			WHERE a.telegram_id = t.%s
			  AND t.%s IS NULL%s`,
			item.table, item.targetCol, item.sourceCol, item.targetCol, guard)
		if _, err := tx.Exec(ctx, query); err != nil {
			return fmt.Errorf("миграция субъектов (%s.%s): %w", item.table, item.targetCol, err)
		}
	}

	// 3. Заказы и обращения относятся к субъекту по telegram_id: у веб-аккаунта
	// он есть только после привязки, поэтому account_id заполняется там, где
	// субъект найден.
	if _, err := tx.Exec(ctx, `
		UPDATE orders o SET account_id = a.id
		FROM accounts a
		WHERE o.account_id IS NULL AND a.telegram_id = o.telegram_id`); err != nil {
		return fmt.Errorf("миграция субъектов (orders.account_id): %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE support_tickets t SET subject_id = a.id
		FROM accounts a
		WHERE t.subject_id IS NULL AND a.telegram_id = t.telegram_id`); err != nil {
		return fmt.Errorf("миграция субъектов (support_tickets.subject_id): %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE support_messages m SET subject_id = a.id
		FROM accounts a
		WHERE m.subject_id IS NULL AND a.telegram_id = m.telegram_id`); err != nil {
		return fmt.Errorf("миграция субъектов (support_messages.subject_id): %w", err)
	}

	// 4. Реферальный код каждому субъекту, у кого его нет: без него некуда
	// вести приглашения.
	if err := assignMissingRefCodes(ctx, tx); err != nil {
		return fmt.Errorf("миграция субъектов: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("миграция субъектов: %w", err)
	}
	return nil
}

// assignMissingRefCodes выдаёт субъектам, у которых нет кода, свободный код.
// Занятость проверяется в том же UPDATE: иначе уникальный индекс оборвал бы
// транзакцию целиком и повторять было бы уже нечего.
func assignMissingRefCodes(ctx context.Context, tx Querier) error {
	rows, err := tx.Query(ctx, `SELECT id FROM accounts WHERE ref_code IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		for attempt := 0; attempt < 10; attempt++ {
			code := newRefCode()
			tag, err := tx.Exec(ctx, `
				UPDATE accounts SET ref_code = $1
				WHERE id = $2 AND ref_code IS NULL
				  AND NOT EXISTS (SELECT 1 FROM accounts WHERE ref_code = $1)`, code, id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				break
			}
		}
	}
	return nil
}
