package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type PanelEvent struct {
	ID, Event, DeviceLabel, Status, ClaimToken string
	TelegramID                                 int64
}

// Store before acknowledging the webhook, so a restart cannot lose an accepted
// event. A raw-body digest makes retries idempotent without storing the payload.
func (db *DB) RecordPanelEvent(ctx context.Context, e PanelEvent) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	status := "ignored"
	if e.Event == "user.first_connected" {
		status = "counted"
	}
	if e.Event == "user_hwid_devices.added" && e.TelegramID > 0 {
		var known bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE telegram_id=$1 AND merged_into IS NULL)`, e.TelegramID).Scan(&known); err != nil {
			return err
		}
		if known {
			status = "pending"
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO panel_events(id,event,telegram_id,device_label,status) VALUES($1,$2,NULLIF($3,0),$4,$5) ON CONFLICT(id) DO NOTHING`, e.ID, e.Event, e.TelegramID, e.DeviceLabel, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 && status == "counted" {
		if _, err = tx.Exec(ctx, `WITH connected AS (
		 UPDATE accounts SET first_connected_at=now(),connection_known=true,connection_checked_at=now()
		 WHERE telegram_id=$1 AND merged_into IS NULL AND first_connected_at IS NULL RETURNING id)
		 INSERT INTO funnel_daily(day,step,hits) SELECT current_date,'first_connected',count(*) FROM connected HAVING count(*)>0
		 ON CONFLICT(day,step) DO UPDATE SET hits=funnel_daily.hits+EXCLUDED.hits`, e.TelegramID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (db *DB) ClaimPanelEvent(ctx context.Context) (e PanelEvent, err error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return e, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT id,event,telegram_id,device_label,claim_token FROM panel_events WHERE status='pending' AND available_at<=now() ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&e.ID, &e.Event, &e.TelegramID, &e.DeviceLabel, &e.ClaimToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("panel-notify:%d", e.TelegramID)); err != nil {
		return e, err
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM panel_notification_cooldowns WHERE telegram_id=$1 AND next_allowed_at>now())`, e.TelegramID).Scan(&allowed); err != nil {
		return e, err
	}
	// A retry after an explicit 429 belongs to the already reserved window.
	allowed = allowed || e.ClaimToken != ""
	e.Status = "ignored"
	if allowed {
		e.Status = "sending"
		e.ClaimToken = operationToken()
		if _, err = tx.Exec(ctx, `INSERT INTO panel_notification_cooldowns(telegram_id,next_allowed_at) VALUES($1,now()+interval '5 minutes') ON CONFLICT(telegram_id) DO UPDATE SET next_allowed_at=EXCLUDED.next_allowed_at`, e.TelegramID); err != nil {
			return e, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE panel_events SET status=$2,claim_token=$3,claimed_at=now() WHERE id=$1`, e.ID, e.Status, e.ClaimToken); err != nil {
		return e, err
	}
	return e, tx.Commit(ctx)
}
func (db *DB) FinishPanelEvent(ctx context.Context, e PanelEvent, status string, retry time.Duration) error {
	if status != "sent" && status != "failed" && status != "uncertain" && status != "pending" {
		return fmt.Errorf("invalid panel delivery state")
	}
	_, err := db.Pool.Exec(ctx, `UPDATE panel_events SET status=$3,available_at=now()+$4*interval '1 second' WHERE id=$1 AND claim_token=$2 AND status='sending'`, e.ID, e.ClaimToken, status, retry.Seconds())
	return err
}
func (db *DB) RecoverPanelEvents(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `UPDATE panel_events SET status='uncertain' WHERE status='sending' AND claimed_at<now()-interval '5 minutes'`)
	return err
}
