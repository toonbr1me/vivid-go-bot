package storage

import (
	"context"
	"errors"
	"strconv"
	"time"
)

var ErrTicketBusy = errors.New("обращение ведёт другой сотрудник")

type StaffTicket struct {
	Ticket
	Preview string
}
type StaffQueue struct {
	Tickets            []StaffTicket
	Scope              string
	Page, Pages, Total int
	Counts             map[string]int
}

func (db *DB) StaffQueue(ctx context.Context, staffID int64, scope string, page int) (out StaffQueue, err error) {
	filters := map[string]string{"free": "status='open' AND assigned_to IS NULL AND (snooze_until IS NULL OR snooze_until<=now())", "mine": "status='open' AND assigned_to=$1", "waiting": "status='open' AND (work_state='waiting_user' OR snooze_until>now())", "open": "status='open'", "closed": "status='closed'"}
	if _, ok := filters[scope]; !ok {
		scope = "free"
	}
	out.Scope = scope
	out.Counts = map[string]int{}
	for k, f := range filters {
		if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM support_tickets WHERE `+f+` AND $1::bigint IS NOT NULL`, staffID).Scan(&out.Total); err != nil {
			return
		}
		out.Counts[k] = out.Total
	}
	out.Total = out.Counts[scope]
	out.Pages = max(1, (out.Total+19)/20)
	out.Page = max(0, min(page, out.Pages-1))
	rows, e := db.Pool.Query(ctx, `SELECT `+ticketColumns+` FROM support_tickets WHERE `+filters[scope]+` AND $1::bigint IS NOT NULL ORDER BY updated_at DESC,id DESC LIMIT 20 OFFSET $2`, staffID, out.Page*20)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		t, e := scanTicket(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Tickets = append(out.Tickets, StaffTicket{Ticket: t})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	for i := range out.Tickets {
		err = db.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT left(message,160) FROM support_messages WHERE ticket_id=$1 AND NOT is_internal ORDER BY id DESC LIMIT 1),'')`, out.Tickets[i].ID).Scan(&out.Tickets[i].Preview)
		if err != nil {
			return
		}
	}
	return
}

// StaffTicketAction locks the ticket, validates its holder and commits the audit
// with the change. Notes are never passed to public history or notifications.
func (db *DB) StaffTicketAction(ctx context.Context, ticketID, actor int64, action, text string, target int64) error {
	return db.staffTicketAction(ctx, ticketID, actor, action, text, target, "", "")
}
func (db *DB) StaffTicketReply(ctx context.Context, ticketID, actor int64, text, fileID, fileType string) error {
	return db.staffTicketAction(ctx, ticketID, actor, "reply", text, 0, fileID, fileType)
}
func (db *DB) staffTicketAction(ctx context.Context, ticketID, actor int64, action, text string, target int64, fileID, fileType string) error {

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var holder *int64
	var lock *time.Time
	if err = tx.QueryRow(ctx, `SELECT status,assigned_to,locked_until FROM support_tickets WHERE id=$1 FOR UPDATE`, ticketID).Scan(&status, &holder, &lock); err != nil {
		return err
	}
	mine := holder != nil && *holder == actor
	busy := holder != nil && !mine && lock != nil && lock.After(time.Now())
	if busy {
		return ErrTicketBusy
	}
	if action != "reopen" && status != "open" {
		return errors.New("обращение закрыто")
	}
	switch action {
	case "take":
		_, err = tx.Exec(ctx, `UPDATE support_tickets SET assigned_to=$2,assigned_at=now(),locked_until=now()+interval '30 minutes',work_state='in_progress',snooze_until=NULL,support_mode='human' WHERE id=$1`, ticketID, actor)
	case "reply", "note":
		if (text == "" && fileID == "") || len([]rune(text)) > 4000 {
			return errors.New("сообщение: от 1 до 4000 символов")
		}
		_, err = tx.Exec(ctx, `INSERT INTO support_messages(ticket_id,telegram_id,message,is_from_admin,is_internal,file_id,file_type) VALUES($1,$2,$3,true,$4,$5,$6)`, ticketID, actor, text, action == "note", nullString(fileID), nullString(fileType))
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE support_tickets SET assigned_to=$2,assigned_at=now(),locked_until=now()+interval '30 minutes',support_mode='human',work_state=CASE WHEN $3 THEN 'in_progress' ELSE 'waiting_user' END WHERE id=$1`, ticketID, actor, action == "note")
		}
	case "transfer":
		if !mine {
			return ErrTicketBusy
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff WHERE telegram_id=$1 AND is_active AND role IN ('owner','admin','support'))`, target).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("сотрудник недоступен")
		}
		_, err = tx.Exec(ctx, `UPDATE support_tickets SET assigned_to=$2,assigned_at=now(),locked_until=now()+interval '30 minutes',work_state='in_progress',support_mode='human' WHERE id=$1`, ticketID, target)
	case "snooze":
		if !mine {
			return ErrTicketBusy
		}
		_, err = tx.Exec(ctx, `UPDATE support_tickets SET snooze_until=now()+interval '24 hours',work_state='waiting',locked_until=NULL,assigned_to=NULL WHERE id=$1`, ticketID)
	case "close":
		_, err = tx.Exec(ctx, `UPDATE support_tickets SET status='closed',work_state='closed',assigned_to=NULL,locked_until=NULL WHERE id=$1`, ticketID)
	case "reopen":
		if status != "closed" {
			return errors.New("обращение уже открыто")
		}
		_, err = tx.Exec(ctx, `UPDATE support_tickets SET status='open',work_state='new',assigned_to=NULL,locked_until=NULL,snooze_until=NULL,support_mode='human' WHERE id=$1`, ticketID)
	default:
		return errors.New("неизвестное действие")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE support_tickets SET updated_at=now() WHERE id=$1`, ticketID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO staff_audit(actor_id,action,target,details) VALUES($1,$2,$3,$4)`, actor, "tickets."+action, strconv.FormatInt(ticketID, 10), strconv.FormatInt(target, 10)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (db *DB) StaffTicketHistory(ctx context.Context, id int64) ([]Message, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id,ticket_id,subject_id,telegram_id,message,is_from_admin,is_internal,is_assistant,file_id,file_type,file_bot,created_at FROM support_messages WHERE ticket_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err = rows.Scan(&m.ID, &m.TicketID, &m.SubjectID, &m.TelegramID, &m.Text, &m.FromAdmin, &m.Internal, &m.Assistant, &m.FileID, &m.FileType, &m.FileBot, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Keep SQL audit targets independent of formatting in the UI.
