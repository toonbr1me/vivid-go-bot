package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Режимы работы с обращением.
const (
	// SupportModeHuman — отвечает живой сотрудник.
	SupportModeHuman = "human"
	// SupportModeAssistant — отвечает помощник на базе модели.
	SupportModeAssistant = "assistant"
)

// AITicket — тикет, взятый помощником в работу.
type AITicket struct {
	ID          int64
	SubjectID   *int64
	TelegramID  *int64
	Subject     string
	SupportMode string
	AICursor    int64
	AIReplies   int
	Claim       string
}

// ClaimAITicket берёт одно обращение в работу.
//
// Захват атомарный: SKIP LOCKED не даёт двум помощникам ответить на одно
// обращение одновременно.
func (db *DB) ClaimAITicket(ctx context.Context, claim string, lease time.Duration) (*AITicket, error) {
	row := db.Pool.QueryRow(ctx, `
		UPDATE support_tickets SET ai_claim = $1,
		       ai_locked_until = now() + make_interval(secs => $2)
		WHERE id = (
			SELECT t.id FROM support_tickets t
			WHERE t.status = 'open' AND t.support_mode = 'assistant'
			  AND t.assigned_to IS NULL
			  AND (t.ai_locked_until IS NULL OR t.ai_locked_until < now())
			  AND EXISTS (SELECT 1 FROM support_messages m
			              WHERE m.ticket_id = t.id AND NOT m.is_from_admin
			                AND NOT m.is_internal AND m.id > t.ai_cursor)
			ORDER BY t.updated_at
			FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, subject_id, telegram_id, subject, support_mode, ai_cursor, ai_replies, ai_claim`,
		claim, lease.Seconds())
	var ticket AITicket
	err := row.Scan(&ticket.ID, &ticket.SubjectID, &ticket.TelegramID, &ticket.Subject,
		&ticket.SupportMode, &ticket.AICursor, &ticket.AIReplies, &ticket.Claim)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

// FinishAITicket публикует ответ помощника.
//
// Ответ не появляется, если за это время вмешался сотрудник или вопрос
// изменился: тогда показывать устаревший ответ нельзя.
func (db *DB) FinishAITicket(ctx context.Context, ticketID int64, claim string, cursor int64, text string, toHuman bool) (bool, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var currentMode string
	var currentClaim *string
	var currentStatus string
	var assigned *int64
	var currentSubjectID *int64
	err = tx.QueryRow(ctx, `
		SELECT support_mode, ai_claim, status, assigned_to, subject_id
		FROM support_tickets WHERE id = $1 FOR UPDATE`, ticketID).Scan(
		&currentMode, &currentClaim, &currentStatus, &assigned, &currentSubjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	released := currentClaim == nil || *currentClaim != claim
	if released || currentMode != SupportModeAssistant || currentStatus != "open" || assigned != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE support_tickets SET ai_claim = NULL, ai_locked_until = NULL
			WHERE id = $1`, ticketID); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	var latest int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(id), 0) FROM support_messages
		WHERE ticket_id = $1 AND NOT is_from_admin AND NOT is_internal`, ticketID).Scan(&latest); err != nil {
		return false, err
	}
	if latest != cursor {
		// Вопрос изменился, пока модель думала: отвечать не на то.
		if _, err := tx.Exec(ctx, `
			UPDATE support_tickets SET ai_claim = NULL, ai_locked_until = NULL
			WHERE id = $1`, ticketID); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO support_messages (ticket_id, subject_id, message, is_from_admin, is_assistant)
		VALUES ($1, $2, $3, TRUE, TRUE)`, ticketID, currentSubjectID, text); err != nil {
		return false, err
	}
	mode := SupportModeAssistant
	if toHuman {
		mode = SupportModeHuman
	}
	if _, err := tx.Exec(ctx, `
		UPDATE support_tickets SET ai_cursor = $2, ai_replies = ai_replies + 1,
		       support_mode = $3, ai_claim = NULL, ai_locked_until = NULL, updated_at = now()
		WHERE id = $1`, ticketID, cursor, mode); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// HandoffAITicket передаёт обращение живому сотруднику.
func (db *DB) HandoffAITicket(ctx context.Context, ticketID int64, note string) (bool, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE support_tickets SET support_mode = 'human', ai_claim = NULL,
		       ai_locked_until = NULL, work_state = 'new', updated_at = now()
		WHERE id = $1 AND support_mode = 'assistant'`, ticketID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if note != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO support_messages (ticket_id, subject_id, message, is_from_admin, is_assistant)
			SELECT id, subject_id, $2, TRUE, TRUE FROM support_tickets WHERE id = $1`,
			ticketID, note); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// SetTicketSupportMode переключает режим работы с обращением.
func (db *DB) SetTicketSupportMode(ctx context.Context, ticketID int64, mode string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE support_tickets SET support_mode = $2, ai_claim = NULL,
		       ai_locked_until = NULL, updated_at = now()
		WHERE id = $1`, ticketID, mode)
	return err
}

// CloseIdleAITickets transfers stale assistant conversations to a person.
// Silence is never evidence that a customer's issue has been resolved.
func (db *DB) CloseIdleAITickets(ctx context.Context, hours int, text string) (int, error) {
	tickets, err := db.HandoffIdleAITickets(ctx, hours, text)
	return len(tickets), err
}

func (db *DB) HandoffIdleAITickets(ctx context.Context, hours int, text string) ([]AITicket, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		UPDATE support_tickets t SET support_mode = 'human', work_state = 'new',
		       ai_claim = NULL, ai_locked_until = NULL, updated_at = now()
		WHERE t.status = 'open' AND t.support_mode = 'assistant'
		  AND t.updated_at < now() - make_interval(hours => $1)
		  AND (t.ai_locked_until IS NULL OR t.ai_locked_until <= now())
		RETURNING t.id, t.subject_id, t.telegram_id`, hours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tickets []AITicket
	for rows.Next() {
		var ticket AITicket
		if err := rows.Scan(&ticket.ID, &ticket.SubjectID, &ticket.TelegramID); err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for _, ticket := range tickets {
		if text != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO support_messages (ticket_id, subject_id, message, is_from_admin, is_assistant)
				VALUES ($1, $2, $3, TRUE, TRUE)`, ticket.ID, ticket.SubjectID, text); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return tickets, nil
}
