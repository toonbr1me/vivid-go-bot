package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Reserve at delivery as well as enrollment: yesterday's delayed queue must not
// bypass today's cap or send two campaigns to the same person in one day.
func (db *DB) reserveCampaignDelivery(ctx context.Context, d BroadcastDelivery) (bool, time.Duration, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)
	var id string
	var enabled bool
	var limit int
	err = tx.QueryRow(ctx, `SELECT id,enabled,daily_limit FROM campaigns WHERE id=(SELECT campaign_id FROM broadcasts WHERE id=$1) FOR UPDATE`, d.BroadcastID).Scan(&id, &enabled, &limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if !enabled {
		return false, 0, nil
	}
	var existing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM campaign_send_slots WHERE broadcast_id=$1 AND telegram_id=$2 AND reserved_at>now()-interval '24 hours')`, d.BroadcastID, d.TelegramID).Scan(&existing); err != nil {
		return false, 0, err
	}
	if !existing {
		var used int
		var retrySeconds float64
		if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(extract(epoch from min(reserved_at)+interval '24 hours'-now()),0)::float8 FROM campaign_send_slots WHERE campaign_id=$1 AND reserved_at>now()-interval '24 hours'`, id).Scan(&used, &retrySeconds); err != nil {
			return false, 0, err
		}
		if used >= limit {
			return false, max(time.Second, time.Duration(retrySeconds*float64(time.Second))+time.Second), nil
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO campaign_contacts(telegram_id,next_allowed_at,last_attempt_at,last_attempt_broadcast_id) VALUES($1,now()+interval '24 hours',now(),$2) ON CONFLICT(telegram_id) DO UPDATE SET last_attempt_at=now(),last_attempt_broadcast_id=$2,next_allowed_at=greatest(campaign_contacts.next_allowed_at,now()+interval '24 hours') WHERE campaign_contacts.last_attempt_at IS NULL OR campaign_contacts.last_attempt_at<=now()-interval '24 hours' OR campaign_contacts.last_attempt_broadcast_id=$2`, d.TelegramID, d.BroadcastID)
	if err != nil {
		return false, 0, err
	}
	if tag.RowsAffected() == 0 {
		var retrySeconds float64
		if err = tx.QueryRow(ctx, `SELECT extract(epoch from last_attempt_at+interval '24 hours'-now())::float8 FROM campaign_contacts WHERE telegram_id=$1`, d.TelegramID).Scan(&retrySeconds); err != nil {
			return false, 0, err
		}
		return false, max(time.Second, time.Duration(retrySeconds*float64(time.Second))+time.Second), nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO campaign_send_slots(broadcast_id,telegram_id,campaign_id) VALUES($1,$2,$3) ON CONFLICT(broadcast_id,telegram_id) DO UPDATE SET reserved_at=now()`, d.BroadcastID, d.TelegramID, id); err != nil {
		return false, 0, err
	}
	return true, 0, tx.Commit(ctx)
}
