package storage

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Operations counts the whole queue, independently of recent-order pagination.
type Operations struct {
	AwaitingAccess, StuckPayments, RecurringErrors int
}

const recurringAttentionWhere = `(enabled AND last_error<>'') OR
 (NOT enabled AND stop_reason<>'user' AND NOT EXISTS(SELECT 1 FROM recurring_agreements newer
 WHERE newer.subject_id=recurring_agreements.subject_id AND newer.consent_at>recurring_agreements.consent_at))`

func (db *DB) Operations(ctx context.Context) (s Operations, err error) {
	err = db.Pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM orders WHERE status='paid' AND provisioned_at IS NULL),
 (SELECT count(*) FROM orders WHERE status='pending' AND created_at < now()-interval '30 minutes'),
 (SELECT count(*) FROM recurring_agreements WHERE `+recurringAttentionWhere+`)`).Scan(&s.AwaitingAccess, &s.StuckPayments, &s.RecurringErrors)
	return
}

func (db *DB) AttentionOrders(ctx context.Context, limit int) ([]domain.Order, error) {
	rows, err := db.Pool.Query(ctx, `SELECT `+orderColumns+` FROM orders WHERE (status='paid' AND provisioned_at IS NULL) OR (status='pending' AND created_at < now()-interval '30 minutes') ORDER BY (status='paid') DESC,created_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Order{}
	for rows.Next() {
		o, e := scanOrder(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (db *DB) RecurringIssues(ctx context.Context, limit int) ([]domain.RecurringAgreement, error) {
	rows, err := db.Pool.Query(ctx, `SELECT `+agreementColumns+` FROM recurring_agreements WHERE `+recurringAttentionWhere+` ORDER BY updated_at DESC,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RecurringAgreement{}
	for rows.Next() {
		a, e := scanAgreement(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
