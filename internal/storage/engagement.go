package storage

import (
	"context"
	"errors"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"time"
)

func (db *DB) NotConnectedCandidates(ctx context.Context, hours int) ([]domain.Subject, error) {
	rows, err := db.Pool.Query(ctx, `SELECT a.id FROM accounts a WHERE a.telegram_id>0 AND a.merged_into IS NULL AND a.first_connected_at IS NULL AND EXISTS(SELECT 1 FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.kind='subscription' AND o.status='provisioned' AND o.expire_at>now() AND o.provisioned_at<now()-make_interval(hours=>$1) AND o.provisioned_at>now()-interval '7 days') ORDER BY a.connection_checked_at NULLS FIRST,a.id LIMIT 50`, hours)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []domain.Subject
	for _, id := range ids {
		s, e := db.SubjectByID(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, nil
}
func (db *DB) RecordConnectionCheck(ctx context.Context, id int64, known, connected bool) error {
	if known {
		var first *time.Time
		if connected {
			now := time.Now()
			first = &now
		}
		return db.ObserveConnection(ctx, id, first)
	}
	_, err := db.Pool.Exec(ctx, `UPDATE accounts SET connection_known=$2,connection_checked_at=now(),first_connected_at=CASE WHEN $3 THEN COALESCE(first_connected_at,now()) ELSE first_connected_at END WHERE id=$1`, id, known, connected)
	return err
}

func (db *DB) ExpiringDeviceNotifications(ctx context.Context) ([]domain.Order, error) {
	return db.queryOrders(ctx, `SELECT `+orderColumns+` FROM orders WHERE kind='devices' AND status='provisioned' AND device_expires_at>now() AND device_expires_at<now()+interval '3 days' AND telegram_id IS NOT NULL ORDER BY device_expires_at LIMIT 100`)
}

// Exported to keep Telegram and the website on exactly the same segments.
func ValidAudience(s string) bool {
	switch s {
	case "all", "active", "inactive", "trial_only", "abandoned", "expired", "not_connected":
		return true
	}
	return false
}

type JobHealth struct {
	Name        string
	LastAttempt time.Time
	LastSuccess *time.Time
	Failures    int
}

func (db *DB) RecordJobResult(ctx context.Context, name string, success bool) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO job_health(name,last_success_at,failures) VALUES($1,CASE WHEN $2 THEN now() END,CASE WHEN $2 THEN 0 ELSE 1 END) ON CONFLICT(name) DO UPDATE SET last_attempt_at=now(),last_success_at=CASE WHEN $2 THEN now() ELSE job_health.last_success_at END,failures=CASE WHEN $2 THEN 0 ELSE job_health.failures+1 END,alerted_at=CASE WHEN $2 THEN NULL ELSE job_health.alerted_at END`, name, success)
	return err
}
func (db *DB) JobHealth(ctx context.Context) ([]JobHealth, error) {
	rows, err := db.Pool.Query(ctx, `SELECT name,last_attempt_at,last_success_at,failures FROM job_health ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobHealth
	for rows.Next() {
		var j JobHealth
		if err = rows.Scan(&j.Name, &j.LastAttempt, &j.LastSuccess, &j.Failures); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (db *DB) ClaimJobAlert(ctx context.Context, name string) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `UPDATE job_health SET alerted_at=now() WHERE name=$1 AND failures>=3 AND (alerted_at IS NULL OR alerted_at<now()-interval '1 hour')`, name)
	return tag.RowsAffected() > 0, err
}

func (db *DB) RecordJobStart(ctx context.Context, name string, interval time.Duration) error {
	_, err := db.Pool.Exec(ctx, `INSERT INTO job_health(name,interval_seconds) VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET last_attempt_at=now(),interval_seconds=excluded.interval_seconds`, name, interval.Seconds())
	return err
}
func (db *DB) CheckOperationalBacklog(ctx context.Context) error {
	var delayed bool
	err := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE status='paid' AND provisioned_at IS NULL AND created_at<now()-interval '10 minutes') OR EXISTS(SELECT 1 FROM payment_inbox WHERE processed_at IS NULL AND created_at<now()-interval '15 minutes') OR EXISTS(SELECT 1 FROM job_health WHERE last_attempt_at<now()-greatest(interval_seconds*3,1800)*interval '1 second')`).Scan(&delayed)
	if err != nil {
		return err
	}
	if delayed {
		return errors.New("обнаружена задержка выдачи, платёжных событий или фоновых задач")
	}
	return nil
}
