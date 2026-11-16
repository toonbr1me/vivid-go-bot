package storage

import "context"

// TicketPage uses a stable ID tie-breaker so equal timestamps cannot hide rows.
type TicketPage struct {
	Tickets            []Ticket
	Page, Pages, Total int
}

func (db *DB) TicketsForSubjectPage(ctx context.Context, subjectID int64, page, size int) (TicketPage, error) {
	return db.ticketPage(ctx, `subject_id=$1`, `created_at DESC,id DESC`, subjectID, page, size)
}

func (db *DB) QueueTicketsPage(ctx context.Context, staffID int64, page, size int) (TicketPage, error) {
	return db.ticketPage(ctx, `status='open' AND $1::bigint IS NOT NULL`,
		`(assigned_to=$1) DESC,(assigned_to IS NULL) DESC,COALESCE(snooze_until,now()) ASC,created_at ASC,id ASC`, staffID, page, size)
}

func (db *DB) ticketPage(ctx context.Context, where, order string, owner int64, page, size int) (out TicketPage, err error) {
	if size < 1 || size > 100 {
		size = 10
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM support_tickets WHERE `+where, owner).Scan(&out.Total); err != nil {
		return
	}
	out.Pages = (out.Total + size - 1) / size
	if out.Pages == 0 {
		out.Pages = 1
	}
	if page == -1 || page >= out.Pages {
		page = out.Pages - 1
	}
	if page < 0 {
		page = 0
	}
	out.Page = page
	rows, err := db.Pool.Query(ctx, `SELECT `+ticketColumns+` FROM support_tickets WHERE `+where+` ORDER BY `+order+` LIMIT $2 OFFSET $3`, owner, size, page*size)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Tickets = []Ticket{}
	for rows.Next() {
		ticket, e := scanTicket(rows)
		if e != nil {
			return out, e
		}
		out.Tickets = append(out.Tickets, ticket)
	}
	return out, rows.Err()
}
