package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Poll — опрос для рассылки.
type Poll struct {
	ID             int64
	Question       string
	Options        []string
	IsAnonymous    bool
	AllowsMultiple bool
	Audience       string
	Status         string
	SentCount      int
	FailedCount    int
	CreatedBy      *int64
	CreatedAt      time.Time
	SentAt         *time.Time
	ClosedAt       *time.Time
}

// PollCounts — сколько человек выбрало каждый вариант.
type PollCounts struct {
	Options []string
	Counts  []int
	Voters  int
}

// CreatePoll заводит опрос.
func (db *DB) CreatePoll(ctx context.Context, poll Poll, createdBy *int64) (Poll, error) {
	row := db.Pool.QueryRow(ctx, `
		INSERT INTO polls (question, options, is_anonymous, allows_multiple_answers, audience, status, created_by)
		VALUES ($1, $2, $3, $4, $5, 'draft', $6)
		RETURNING id, question, options, is_anonymous, allows_multiple_answers, audience, status,
		          sent_count, failed_count, created_by, created_at, sent_at, closed_at`,
		poll.Question, poll.Options, poll.IsAnonymous, poll.AllowsMultiple, poll.Audience, createdBy)
	return scanPoll(row)
}

// GetPoll читает опрос.
func (db *DB) GetPoll(ctx context.Context, pollID int64) (*Poll, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT id, question, options, is_anonymous, allows_multiple_answers, audience, status,
		       sent_count, failed_count, created_by, created_at, sent_at, closed_at
		FROM polls WHERE id = $1`, pollID)
	poll, err := scanPoll(row)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &poll, nil
}

// ListPolls — последние опросы.
func (db *DB) ListPolls(ctx context.Context, limit int) ([]Poll, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, question, options, is_anonymous, allows_multiple_answers, audience, status,
		       sent_count, failed_count, created_by, created_at, sent_at, closed_at
		FROM polls ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Poll
	for rows.Next() {
		poll, err := scanPoll(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, poll)
	}
	return out, rows.Err()
}

// MarkPollSent закрывает рассылку опроса.
func (db *DB) MarkPollSent(ctx context.Context, pollID int64, sent, failed int) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE polls SET status = 'sent', sent_count = $2, failed_count = $3,
		       sent_at = now()
		WHERE id = $1`, pollID, sent, failed)
	return err
}

// RecordPollDelivery запоминает, кому ушёл опрос.
func (db *DB) RecordPollDelivery(ctx context.Context, tgPollID string, pollID, telegramID, messageID int64) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO poll_deliveries (tg_poll_id, poll_id, telegram_id, message_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tg_poll_id) DO NOTHING`, tgPollID, pollID, telegramID, messageID)
	return err
}

// RecordPollAnswer записывает голос.
func (db *DB) RecordPollAnswer(ctx context.Context, pollID, telegramID int64, options []int) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO poll_answers (poll_id, telegram_id, option_ids)
		VALUES ($1, $2, $3)
		ON CONFLICT (poll_id, telegram_id) DO UPDATE SET option_ids = EXCLUDED.option_ids`,
		pollID, telegramID, options)
	return err
}

// PollResults — итоги опроса.
func (db *DB) PollResults(ctx context.Context, pollID int64) (PollCounts, error) {
	poll, err := db.GetPoll(ctx, pollID)
	if err != nil {
		return PollCounts{}, err
	}
	counts := make([]int, len(poll.Options))
	rows, err := db.Pool.Query(ctx, `
		SELECT option_ids FROM poll_answers WHERE poll_id = $1`, pollID)
	if err != nil {
		return PollCounts{}, err
	}
	defer rows.Close()
	voters := 0
	for rows.Next() {
		var options []int32
		if err := rows.Scan(&options); err != nil {
			return PollCounts{}, err
		}
		voters++
		for _, option := range options {
			if option >= 0 && int(option) < len(counts) {
				counts[option]++
			}
		}
	}
	return PollCounts{Options: poll.Options, Counts: counts, Voters: voters}, err
}

// PollAudience — кому отправлять опрос.
//
// active — только люди с действующей подпиской; all — все, кто когда-либо
// пользовался ботом.
func (db *DB) PollAudience(ctx context.Context, audience string, limit int) ([]int64, error) {
	query := `
		SELECT telegram_id FROM accounts
		WHERE telegram_id IS NOT NULL
		  AND ($1 <> 'active' OR id IN (
		      SELECT subject_id FROM device_slot_grants WHERE subject_id IS NOT NULL
		      UNION SELECT subject_id FROM orders
		      WHERE subject_id IS NOT NULL AND status = 'provisioned'
		                AND expire_at > now()))
		ORDER BY id LIMIT $2`
	rows, err := db.Pool.Query(ctx, query, audience, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanPoll(row pgx.Row) (Poll, error) {
	var poll Poll
	err := row.Scan(&poll.ID, &poll.Question, &poll.Options, &poll.IsAnonymous,
		&poll.AllowsMultiple, &poll.Audience, &poll.Status, &poll.SentCount,
		&poll.FailedCount, &poll.CreatedBy, &poll.CreatedAt, &poll.SentAt, &poll.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Poll{}, ErrNotFound
	}
	return poll, err
}
