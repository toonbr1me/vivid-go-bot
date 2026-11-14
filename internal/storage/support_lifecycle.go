package storage

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (db *DB) MarkTicketRead(ctx context.Context, id, subjectID, through int64) error {
	_, err := db.Pool.Exec(ctx, `UPDATE support_tickets SET owner_read_message_id=GREATEST(owner_read_message_id,LEAST(last_staff_message_id,$3)) WHERE id=$1 AND (subject_id=$2 OR telegram_id=(SELECT telegram_id FROM accounts WHERE id=$2))`, id, subjectID, through)
	return err
}

func (db *DB) ResolveOwnTicket(ctx context.Context, id, subjectID int64) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `UPDATE support_tickets SET status='closed',work_state='closed',updated_at=now(),locked_until=NULL,assigned_to=NULL WHERE id=$1 AND (subject_id=$2 OR telegram_id=(SELECT telegram_id FROM accounts WHERE id=$2)) AND status='open'`, id, subjectID)
	return tag.RowsAffected() > 0, err
}

// Lock against resolution so a stale reply cannot silently reopen a ticket.
func (db *DB) AddOwnerTicketMessage(ctx context.Context, id, subjectID int64, tgID *int64, text, fileID, fileType string) (Message, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)
	var lockedID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM support_tickets WHERE id=$1 AND (subject_id=$2 OR telegram_id=(SELECT telegram_id FROM accounts WHERE id=$2)) AND status='open' FOR UPDATE`, id, subjectID).Scan(&lockedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Message{}, ErrNotFound
		}
		return Message{}, err
	}
	m, err := addTicketMessage(ctx, tx, id, subjectID, tgID, text, false, fileID, fileType)
	if err != nil {
		return Message{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Message{}, err
	}
	return m, nil
}
