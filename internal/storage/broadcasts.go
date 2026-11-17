package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
	"unicode/utf16"
)

type Broadcast struct {
	Skipped                                 int
	ID                                      string
	ActorID                                 int64
	Audience, Text, PromoCode, Status       string
	Total, Sent, Failed, Uncertain, Pending int
}
type BroadcastDelivery struct {
	BroadcastID                 string
	ActorID, TelegramID         int64
	Text, PromoCode, ClaimToken string
}

func operationToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

const audienceBase = `SELECT a.telegram_id FROM accounts a WHERE a.telegram_id>0 AND a.merged_into IS NULL AND
 (CASE $1
 WHEN 'all' THEN true
 WHEN 'active' THEN EXISTS(SELECT 1 FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.kind='subscription' AND o.status='provisioned' AND o.expire_at>now())
 WHEN 'inactive' THEN NOT EXISTS(SELECT 1 FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.kind='subscription' AND o.status='provisioned' AND o.expire_at>now())
 WHEN 'trial_only' THEN EXISTS(SELECT 1 FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.provider='trial' AND o.status='provisioned') AND NOT EXISTS(SELECT 1 FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.amount>0 AND o.status IN ('paid','provisioned'))
 WHEN 'abandoned' THEN EXISTS(SELECT 1 FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.status='pending' AND o.created_at<now()-interval '1 hour' AND o.created_at>now()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM orders paid WHERE (paid.account_id=a.id OR paid.telegram_id=a.telegram_id) AND paid.status IN ('paid','provisioned') AND paid.created_at>o.created_at))
 WHEN 'expired' THEN (SELECT max(o.expire_at) FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.kind='subscription' AND o.status='provisioned') BETWEEN now()-interval '30 days' AND now()-interval '3 days' AND NOT EXISTS(SELECT 1 FROM orders paid WHERE (paid.account_id=a.id OR paid.telegram_id=a.telegram_id) AND paid.kind='subscription' AND paid.status='paid') AND NOT EXISTS(SELECT 1 FROM recurring_agreements recurring WHERE recurring.subject_id=a.id AND recurring.enabled)
 WHEN 'not_connected' THEN a.connection_known AND a.connection_checked_at>now()-interval '24 hours' AND a.first_connected_at IS NULL AND EXISTS(SELECT 1 FROM orders o WHERE (o.account_id=a.id OR o.telegram_id=a.telegram_id) AND o.kind='subscription' AND o.status='provisioned' AND o.expire_at>now())
 ELSE false END)
 AND ($1 IN ('all','active','inactive') OR COALESCE((SELECT activity_notifications FROM notification_settings n WHERE n.telegram_id=a.telegram_id),true)) ORDER BY a.id`

func validAudience(s string) bool { return ValidAudience(s) }
func (db *DB) CreateBroadcast(ctx context.Context, actor int64, audience, text, promo string) (Broadcast, error) {
	if !validAudience(audience) || strings.TrimSpace(text) == "" || len(utf16.Encode([]rune(text))) > 3000 {
		return Broadcast{}, fmt.Errorf("invalid broadcast")
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return Broadcast{}, err
	}
	defer tx.Rollback(ctx)
	id := operationToken()
	if _, err = tx.Exec(ctx, `INSERT INTO broadcasts(id,actor_id,audience,message,promo_code) VALUES($1,$2,$3,$4,$5)`, id, actor, audience, text, promo); err != nil {
		return Broadcast{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO broadcast_deliveries(broadcast_id,telegram_id) SELECT $2,telegram_id FROM (`+audienceBase+`) recipients`, audience, id); err != nil {
		return Broadcast{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Broadcast{}, err
	}
	return db.Broadcast(ctx, id, actor)
}
func (db *DB) Broadcast(ctx context.Context, id string, actor int64) (b Broadcast, err error) {
	err = db.Pool.QueryRow(ctx, `SELECT b.id,b.actor_id,b.audience,b.message,b.promo_code,b.status,count(d.telegram_id),
 count(*) FILTER(WHERE d.status='sent'),count(*) FILTER(WHERE d.status='failed'),count(*) FILTER(WHERE d.status='uncertain'),count(*) FILTER(WHERE d.status IN('pending','sending')),count(*) FILTER(WHERE d.status='skipped')
 FROM broadcasts b LEFT JOIN broadcast_deliveries d ON d.broadcast_id=b.id WHERE b.id=$1 AND b.actor_id=$2 GROUP BY b.id`, id, actor).Scan(&b.ID, &b.ActorID, &b.Audience, &b.Text, &b.PromoCode, &b.Status, &b.Total, &b.Sent, &b.Failed, &b.Uncertain, &b.Pending, &b.Skipped)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
func (db *DB) ConfirmBroadcast(ctx context.Context, id string, actor int64) (bool, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE broadcasts SET status='queued',confirmed_at=now() WHERE id=$1 AND actor_id=$2 AND status='draft' AND created_at>now()-interval '20 minutes' AND EXISTS(SELECT 1 FROM broadcast_deliveries WHERE broadcast_id=$1)`, id, actor)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO staff_audit(actor_id,action,target,amount,details) VALUES($1,'broadcast.send',$2,0,'Confirmed recipient snapshot')`, actor, id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
func (db *DB) CancelBroadcast(ctx context.Context, id string, actor int64) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `UPDATE broadcasts SET status='cancelled' WHERE id=$1 AND actor_id=$2 AND status='draft'`, id, actor)
	return tag.RowsAffected() > 0, err
}
func (db *DB) ClaimBroadcast(ctx context.Context) (d BroadcastDelivery, err error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT b.id,b.actor_id,d.telegram_id,b.message,b.promo_code FROM broadcast_deliveries d JOIN broadcasts b ON b.id=d.broadcast_id WHERE b.status IN('queued','sending') AND d.status='pending' AND d.available_at<=now() ORDER BY b.created_at,d.telegram_id FOR UPDATE OF d SKIP LOCKED LIMIT 1`).Scan(&d.BroadcastID, &d.ActorID, &d.TelegramID, &d.Text, &d.PromoCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.ClaimToken = operationToken()
	if _, err = tx.Exec(ctx, `UPDATE broadcast_deliveries SET status='sending',claimed_at=now(),claim_token=$3 WHERE broadcast_id=$1 AND telegram_id=$2`, d.BroadcastID, d.TelegramID, d.ClaimToken); err != nil {
		return d, err
	}
	if _, err = tx.Exec(ctx, `UPDATE broadcasts SET status='sending' WHERE id=$1 AND status='queued'`, d.BroadcastID); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}
func (db *DB) FinishBroadcastDelivery(ctx context.Context, d BroadcastDelivery, status string, retry time.Duration) error {
	if status != "sent" && status != "failed" && status != "uncertain" && status != "pending" && status != "skipped" {
		return fmt.Errorf("invalid delivery state")
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE broadcast_deliveries SET status=$4,available_at=now()+$5*interval '1 second' WHERE broadcast_id=$1 AND telegram_id=$2 AND claim_token=$3 AND status='sending'`, d.BroadcastID, d.TelegramID, d.ClaimToken, status, retry.Seconds()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE broadcasts SET status='completed' WHERE id=$1 AND status IN('queued','sending') AND NOT EXISTS(SELECT 1 FROM broadcast_deliveries WHERE broadcast_id=$1 AND status IN('pending','sending'))`, d.BroadcastID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (db *DB) RecoverBroadcastDeliveries(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `UPDATE broadcast_deliveries SET status='uncertain' WHERE status='sending' AND claimed_at<now()-interval '5 minutes'; UPDATE broadcasts b SET status='completed' WHERE status IN('queued','sending') AND NOT EXISTS(SELECT 1 FROM broadcast_deliveries d WHERE d.broadcast_id=b.id AND d.status IN('pending','sending'))`)
	return err
}
func (db *DB) BlockBroadcast(ctx context.Context, id string) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE broadcasts SET status='cancelled' WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE broadcast_deliveries SET status='failed' WHERE broadcast_id=$1 AND status IN('pending','sending')`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (db *DB) RecentBroadcasts(ctx context.Context, actor int64) ([]Broadcast, error) {
	rows, err := db.Pool.Query(ctx, `SELECT b.id,b.status,count(d.telegram_id) FROM broadcasts b LEFT JOIN broadcast_deliveries d ON d.broadcast_id=b.id WHERE b.actor_id=$1 GROUP BY b.id ORDER BY b.created_at DESC LIMIT 8`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Broadcast{}
	for rows.Next() {
		var b Broadcast
		if err := rows.Scan(&b.ID, &b.Status, &b.Total); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
