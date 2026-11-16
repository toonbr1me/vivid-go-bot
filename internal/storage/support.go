package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Ticket — обращение в поддержку.
type Ticket struct {
	ID                 int64
	SubjectID          *int64
	TelegramID         *int64
	Subject            string
	Status             string
	AssignedTo         *int64
	WorkState          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	OwnerReadMessageID int64
	LastStaffMessageID int64
	Context            string
}

func (t Ticket) Unread() bool { return t.LastStaffMessageID > t.OwnerReadMessageID }

// Message — сообщение переписки.
type Message struct {
	ID         int64
	TicketID   int64
	TelegramID *int64
	SubjectID  *int64
	Text       string
	FromAdmin  bool
	Internal   bool
	Assistant  bool
	FileID     *string
	FileType   *string
	CreatedAt  time.Time
}

const ticketColumns = `id, subject_id, telegram_id, subject, status, assigned_to,
	work_state, created_at, updated_at,owner_read_message_id,last_staff_message_id,context`

func scanTicket(row pgx.Row) (Ticket, error) {
	var ticket Ticket
	err := row.Scan(&ticket.ID, &ticket.SubjectID, &ticket.TelegramID, &ticket.Subject,
		&ticket.Status, &ticket.AssignedTo, &ticket.WorkState,
		&ticket.CreatedAt, &ticket.UpdatedAt, &ticket.OwnerReadMessageID, &ticket.LastStaffMessageID, &ticket.Context)
	if errors.Is(err, pgx.ErrNoRows) {
		return ticket, ErrNotFound
	}
	return ticket, err
}

// OpenTicketForSubject — текущее открытое обращение, если оно есть.
func (db *DB) OpenTicketForSubject(ctx context.Context, subjectID int64) (*Ticket, error) {
	ticket, err := scanTicket(db.Pool.QueryRow(ctx, `
		SELECT `+ticketColumns+` FROM support_tickets
		WHERE subject_id = $1 AND status = 'open'
		ORDER BY created_at DESC LIMIT 1`, subjectID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

// TicketByID читает обращение.
func (db *DB) TicketByID(ctx context.Context, id int64) (Ticket, error) {
	return scanTicket(db.Pool.QueryRow(ctx, `SELECT `+ticketColumns+` FROM support_tickets WHERE id = $1`, id))
}

// CreateTicket открывает обращение с первым сообщением.
//
// mode — кто отвечает: живой сотрудник или помощник. Пустая строка —
// сотрудник, как раньше.
func (db *DB) CreateTicket(ctx context.Context, subjectID int64, telegramID *int64, subject, text, mode string) (Ticket, error) {
	return db.CreateTicketContent(ctx, subjectID, telegramID, subject, text, mode, "", "", "")
}

func (db *DB) CreateTicketContent(ctx context.Context, subjectID int64, telegramID *int64, subject, text, mode, contextText, fileID, fileType string) (Ticket, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback(ctx)
	if mode != SupportModeAssistant {
		mode = SupportModeHuman
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO support_tickets (subject_id, telegram_id, subject, status, support_mode, context)
		VALUES ($1, $2, $3, 'open', $4, $5)
		RETURNING `+ticketColumns, subjectID, telegramID, subject, mode, contextText)
	ticket, err := scanTicket(row)
	if err != nil {
		return ticket, err
	}
	if text != "" || fileID != "" {
		if _, err := addTicketMessage(ctx, tx, ticket.ID, subjectID, telegramID, text, false, fileID, fileType); err != nil {
			return ticket, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

// AddTicketMessage дописывает сообщение и трогает updated_at.
func (db *DB) AddTicketMessage(
	ctx context.Context, ticketID, subjectID int64, telegramID *int64,
	text string, fromAdmin bool, fileID, fileType string,
) (Message, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)
	message, err := addTicketMessage(ctx, tx, ticketID, subjectID, telegramID, text, fromAdmin, fileID, fileType)
	if err != nil {
		return Message{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Message{}, err
	}
	return message, nil
}

func addTicketMessage(ctx context.Context, q Querier, ticketID, subjectID int64, telegramID *int64, text string, fromAdmin bool, fileID, fileType string) (Message, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO support_messages (ticket_id, subject_id, telegram_id, message, is_from_admin, file_id, file_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, ticket_id, subject_id, telegram_id, message, is_from_admin,
		          is_internal, is_assistant, file_id, file_type, created_at`,
		ticketID, subjectID, telegramID, text, fromAdmin, nullString(fileID), nullString(fileType))
	var message Message
	err := row.Scan(&message.ID, &message.TicketID, &message.SubjectID, &message.TelegramID,
		&message.Text, &message.FromAdmin, &message.Internal, &message.Assistant,
		&message.FileID, &message.FileType, &message.CreatedAt)
	if err != nil {
		return message, err
	}
	_, err = q.Exec(ctx,
		`UPDATE support_tickets SET updated_at = now(), work_state = 'new' WHERE id = $1`, ticketID)
	return message, err
}

// TicketMessages читает переписку без внутренних заметок.
func (db *DB) TicketMessages(ctx context.Context, ticketID int64, limit int) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, ticket_id, subject_id, telegram_id, message, is_from_admin,
		       is_internal, is_assistant, file_id, file_type, created_at
		FROM support_messages
		WHERE ticket_id = $1 AND NOT is_internal
		ORDER BY created_at DESC,id DESC LIMIT $2`, ticketID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.TicketID, &message.SubjectID,
			&message.TelegramID, &message.Text, &message.FromAdmin, &message.Internal,
			&message.Assistant, &message.FileID, &message.FileType, &message.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, message)
	}
	return out, rows.Err()
}

// TicketHistory returns the complete public conversation, oldest first.
// Screen pagination must not make older saved messages inaccessible.
func (db *DB) TicketHistory(ctx context.Context, ticketID int64) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, ticket_id, subject_id, telegram_id, message, is_from_admin,
		       is_internal, is_assistant, file_id, file_type, created_at
		FROM support_messages WHERE ticket_id=$1 AND NOT is_internal
		ORDER BY created_at,id`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.TicketID, &m.SubjectID, &m.TelegramID, &m.Text,
			&m.FromAdmin, &m.Internal, &m.Assistant, &m.FileID, &m.FileType, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TicketsForSubject — все обращения пользователя.
func (db *DB) TicketsForSubject(ctx context.Context, subjectID int64, limit int) ([]Ticket, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT `+ticketColumns+` FROM support_tickets
		WHERE subject_id = $1 ORDER BY created_at DESC LIMIT $2`, subjectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ticket)
	}
	return out, rows.Err()
}

// QueueTickets — очередь сотрудника: сначала невзятые, потом свои.
func (db *DB) QueueTickets(ctx context.Context, staffID int64, limit int) ([]Ticket, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT `+ticketColumns+` FROM support_tickets
		WHERE status = 'open'
		ORDER BY (assigned_to = $1) DESC,
		         (assigned_to IS NULL) DESC,
		         COALESCE(snooze_until, now()) ASC,
		         created_at ASC
		LIMIT $2`, staffID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ticket)
	}
	return out, rows.Err()
}

// TakeTicket захватывает обращение на 30 минут.
func (db *DB) TakeTicket(ctx context.Context, ticketID, staffID int64) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE support_tickets
		SET assigned_to = $2, assigned_at = now(),
		    locked_until = now() + interval '30 minutes', work_state = 'in_progress'
		WHERE id = $1 AND status = 'open'
		  AND (assigned_to IS NULL OR assigned_to = $2
		       OR locked_until IS NULL OR locked_until < now())`,
		ticketID, staffID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ReleaseTicket снимает захват.
func (db *DB) ReleaseTicket(ctx context.Context, ticketID int64) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE support_tickets SET assigned_to = NULL, assigned_at = NULL,
		       locked_until = NULL, work_state = 'new' WHERE id = $1`, ticketID)
	return err
}

// CloseTicket закрывает обращение.
func (db *DB) CloseTicket(ctx context.Context, ticketID int64) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE support_tickets SET status = 'closed', work_state = 'closed',
		       assigned_to = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1 AND status = 'open'`, ticketID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ReopenTicket возвращает закрытое обращение в очередь.
func (db *DB) ReopenTicket(ctx context.Context, ticketID int64) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE support_tickets SET status = 'open', work_state = 'new',
		       updated_at = now() WHERE id = $1`, ticketID)
	return err
}

// SnoozeTicket откладывает обращение.
func (db *DB) SnoozeTicket(ctx context.Context, ticketID int64, until time.Time) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE support_tickets SET snooze_until = $2, work_state = 'waiting'
		WHERE id = $1`, ticketID, until)
	return err
}

// OpenTicketCount — число открытых обращений.
func (db *DB) OpenTicketCount(ctx context.Context) (int, error) {
	var count int
	err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM support_tickets WHERE status = 'open'`).Scan(&count)
	return count, err
}

// ReleaseExpiredLocks снимает просроченные захваты.
func (db *DB) ReleaseExpiredLocks(ctx context.Context) ([]Ticket, error) {
	rows, err := db.Pool.Query(ctx, `
		UPDATE support_tickets
		SET assigned_to = NULL, assigned_at = NULL, locked_until = NULL, work_state = 'new'
		WHERE status = 'open' AND locked_until IS NOT NULL AND locked_until < now()
		RETURNING `+ticketColumns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ticket)
	}
	return out, rows.Err()
}

// SaveWebSupportRequest сохраняет обращение с формы сайта.
func (db *DB) SaveWebSupportRequest(ctx context.Context, name, contact, message string) (WebSupportRequest, error) {
	var item WebSupportRequest
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO web_support_requests (name, contact, message)
		VALUES ($1, $2, $3)
		RETURNING id, name, contact, message, status, created_at`,
		name, contact, message).Scan(
		&item.ID, &item.Name, &item.Contact, &item.Message, &item.Status, &item.CreatedAt)
	return item, err
}

// WebSupportRequest — заявка с сайта.
type WebSupportRequest struct {
	ID        int64
	Name      string
	Contact   string
	Message   string
	Status    string
	CreatedAt time.Time
}

// WebSupportRequests читает заявки.
func (db *DB) WebSupportRequests(ctx context.Context, status string, limit int) ([]WebSupportRequest, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, name, contact, message, status, created_at
		FROM web_support_requests WHERE ($1 = '' OR status = $1)
		ORDER BY created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebSupportRequest
	for rows.Next() {
		var item WebSupportRequest
		if err := rows.Scan(&item.ID, &item.Name, &item.Contact, &item.Message,
			&item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// NotificationSettings — что человек хочет получать.
type NotificationSettings struct {
	Expiry   bool
	Traffic  bool
	Activity bool
}

// NotificationSettingsFor читает настройки; по умолчанию всё включено.
func (db *DB) NotificationSettingsFor(ctx context.Context, telegramID int64) (NotificationSettings, error) {
	settings := NotificationSettings{Expiry: true, Traffic: true, Activity: true}
	err := db.Pool.QueryRow(ctx, `
		SELECT expiry_notifications, traffic_notifications, activity_notifications
		FROM notification_settings WHERE telegram_id = $1`, telegramID).Scan(
		&settings.Expiry, &settings.Traffic, &settings.Activity)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings, nil
	}
	return settings, err
}

// SaveNotificationSettings сохраняет настройки уведомлений.
func (db *DB) SaveNotificationSettings(ctx context.Context, telegramID int64, settings NotificationSettings) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO notification_settings (telegram_id, expiry_notifications, traffic_notifications, activity_notifications)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (telegram_id) DO UPDATE
		   SET expiry_notifications = EXCLUDED.expiry_notifications,
		       traffic_notifications = EXCLUDED.traffic_notifications,
		       activity_notifications = EXCLUDED.activity_notifications`,
		telegramID, settings.Expiry, settings.Traffic, settings.Activity)
	return err
}
