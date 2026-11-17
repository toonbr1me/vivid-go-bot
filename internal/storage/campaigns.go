package storage

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jackc/pgx/v5"
)

var ErrCampaignChanged = errors.New("кампания изменилась: обновите предпросмотр")

type Campaign struct {
	ID                                                   string
	ActorID                                              int64
	Title, Audience, Text, PromoCode, LastError          string
	Enabled                                              bool
	Revision, DailyLimit, CooldownDays, SendFrom, SendTo int
	StartsAt                                             time.Time
	LastCheckedAt                                        *time.Time
}
type CampaignPreview struct {
	Campaign                                                      Campaign
	Eligible, Enrolled, Sent, Failed, Uncertain, Pending, Skipped int
	Batches                                                       []CampaignBatch
}
type CampaignBatch struct {
	ID, Status           string
	CreatedAt            time.Time
	Total, Sent, Skipped int
}

const campaignColumns = `id,actor_id,title,audience,message,promo_code,enabled,revision,daily_limit,cooldown_days,send_from,send_to,starts_at,last_checked_at,last_error`

func BehavioralAudience(s string) bool {
	return s == "trial_only" || s == "abandoned" || s == "expired" || s == "not_connected"
}
func ValidateCampaign(c Campaign) error {
	if !BehavioralAudience(c.Audience) || strings.TrimSpace(c.Title) == "" || len([]rune(c.Title)) > 80 || strings.TrimSpace(c.Text) == "" || len(utf16.Encode([]rune(c.Text))) > 3000 {
		return errors.New("укажите название до 80 символов, поведенческий сегмент и текст до 3000 символов")
	}
	if c.DailyLimit < 1 || c.DailyLimit > 10000 || c.CooldownDays != 0 && (c.CooldownDays < 7 || c.CooldownDays > 365) || c.SendFrom < 0 || c.SendTo > 24 || c.SendTo <= c.SendFrom || c.StartsAt.IsZero() {
		return errors.New("проверьте лимит, период повторения, часы и дату начала")
	}
	return nil
}
func scanCampaign(row pgx.Row) (c Campaign, err error) {
	err = row.Scan(&c.ID, &c.ActorID, &c.Title, &c.Audience, &c.Text, &c.PromoCode, &c.Enabled, &c.Revision, &c.DailyLimit, &c.CooldownDays, &c.SendFrom, &c.SendTo, &c.StartsAt, &c.LastCheckedAt, &c.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
func (db *DB) Campaign(ctx context.Context, id string) (Campaign, error) {
	return scanCampaign(db.Pool.QueryRow(ctx, `SELECT `+campaignColumns+` FROM campaigns WHERE id=$1`, id))
}
func (db *DB) Campaigns(ctx context.Context) ([]Campaign, error) {
	rows, err := db.Pool.Query(ctx, `SELECT `+campaignColumns+` FROM campaigns ORDER BY created_at DESC,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Campaign{}
	for rows.Next() {
		c, e := scanCampaign(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Edits always pause sending. The reviewed revision must be enabled explicitly.
func (db *DB) SaveCampaign(ctx context.Context, c Campaign, actor int64) (Campaign, error) {
	if err := ValidateCampaign(c); err != nil {
		return c, err
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)
	if c.ID == "" {
		c.ID = operationToken()
		_, err = tx.Exec(ctx, `INSERT INTO campaigns(id,actor_id,title,audience,message,promo_code,daily_limit,cooldown_days,send_from,send_to,starts_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, c.ID, actor, c.Title, c.Audience, c.Text, c.PromoCode, c.DailyLimit, c.CooldownDays, c.SendFrom, c.SendTo, c.StartsAt)
	} else {
		current, e := scanCampaign(tx.QueryRow(ctx, `SELECT `+campaignColumns+` FROM campaigns WHERE id=$1 FOR UPDATE`, c.ID))
		if e != nil {
			return c, e
		}
		if current.Revision != c.Revision {
			return c, ErrCampaignChanged
		}
		_, err = tx.Exec(ctx, `UPDATE campaigns SET actor_id=$2,title=$3,audience=$4,message=$5,promo_code=$6,daily_limit=$7,cooldown_days=$8,send_from=$9,send_to=$10,starts_at=$11,enabled=false,last_error='',revision=revision+1,updated_at=now() WHERE id=$1`, c.ID, actor, c.Title, c.Audience, c.Text, c.PromoCode, c.DailyLimit, c.CooldownDays, c.SendFrom, c.SendTo, c.StartsAt)
		if err == nil {
			err = cancelCampaignPending(ctx, tx, c.ID)
		}
	}
	if err != nil {
		return c, err
	}
	if err = campaignAudit(ctx, tx, actor, c.ID, "campaign.save"); err != nil {
		return c, err
	}
	if err = tx.Commit(ctx); err != nil {
		return c, err
	}
	return db.Campaign(ctx, c.ID)
}

func (db *DB) SetCampaignEnabled(ctx context.Context, id string, revision int, actor int64, enabled bool) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := scanCampaign(tx.QueryRow(ctx, `SELECT `+campaignColumns+` FROM campaigns WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if c.Revision != revision {
		return ErrCampaignChanged
	}
	if c.Enabled == enabled {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE campaigns SET enabled=$2,actor_id=$3,last_error='',revision=revision+1,updated_at=now() WHERE id=$1`, id, enabled, actor); err != nil {
		return err
	}
	if !enabled {
		if err = cancelCampaignPending(ctx, tx, id); err != nil {
			return err
		}
	}
	action := "campaign.pause"
	if enabled {
		action = "campaign.enable"
	}
	if err = campaignAudit(ctx, tx, actor, id, action); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func campaignAudit(ctx context.Context, tx pgx.Tx, actor int64, id, action string) error {
	_, err := tx.Exec(ctx, `INSERT INTO staff_audit(actor_id,action,target,details) VALUES($1,$2,$3,'Campaign revision reviewed')`, actor, action, id)
	return err
}
func cancelCampaignPending(ctx context.Context, tx pgx.Tx, id string) error {
	if _, err := tx.Exec(ctx, `UPDATE broadcast_deliveries d SET status='skipped' FROM broadcasts b WHERE b.id=d.broadcast_id AND b.campaign_id=$1 AND d.status='pending'`, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE broadcasts SET status='cancelled' WHERE campaign_id=$1 AND status IN ('queued','sending')`, id)
	return err
}

const campaignCandidates = `SELECT a.telegram_id FROM (` + audienceBase + `) a
 WHERE NOT EXISTS(SELECT 1 FROM campaign_enrollments e WHERE e.campaign_id=$2 AND e.telegram_id=a.telegram_id AND ($3::int=0 OR e.last_enrolled_at>now()-make_interval(days=>$3)))
 AND NOT EXISTS(SELECT 1 FROM campaign_contacts c WHERE c.telegram_id=a.telegram_id AND c.next_allowed_at>now()) ORDER BY a.telegram_id`

func (db *DB) CampaignPreview(ctx context.Context, id string) (p CampaignPreview, err error) {
	p.Campaign, err = db.Campaign(ctx, id)
	if err != nil {
		return
	}
	c := p.Campaign
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM (`+campaignCandidates+`) candidates`, c.Audience, c.ID, c.CooldownDays).Scan(&p.Eligible); err != nil {
		return
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM campaign_enrollments WHERE campaign_id=$1`, id).Scan(&p.Enrolled); err != nil {
		return
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE d.status='sent'),count(*) FILTER(WHERE d.status='failed'),count(*) FILTER(WHERE d.status='uncertain'),count(*) FILTER(WHERE d.status IN ('pending','sending')),count(*) FILTER(WHERE d.status='skipped') FROM broadcast_deliveries d JOIN broadcasts b ON b.id=d.broadcast_id WHERE b.campaign_id=$1`, id).Scan(&p.Sent, &p.Failed, &p.Uncertain, &p.Pending, &p.Skipped); err != nil {
		return
	}
	rows, e := db.Pool.Query(ctx, `SELECT b.id,b.status,b.created_at,count(d.telegram_id),count(*) FILTER(WHERE d.status='sent'),count(*) FILTER(WHERE d.status='skipped') FROM broadcasts b LEFT JOIN broadcast_deliveries d ON d.broadcast_id=b.id WHERE b.campaign_id=$1 GROUP BY b.id ORDER BY b.created_at DESC,b.id LIMIT 20`, id)
	if e != nil {
		return p, e
	}
	defer rows.Close()
	for rows.Next() {
		var b CampaignBatch
		if err = rows.Scan(&b.ID, &b.Status, &b.CreatedAt, &b.Total, &b.Sent, &b.Skipped); err != nil {
			return
		}
		p.Batches = append(p.Batches, b)
	}
	err = rows.Err()
	return
}

func campaignActorAllowed(ctx context.Context, tx pgx.Tx, actor int64, owners map[int64]bool) (bool, error) {
	if owners[actor] {
		return true, nil
	}
	var role string
	var active bool
	err := tx.QueryRow(ctx, `SELECT role,is_active FROM staff WHERE telegram_id=$1 FOR SHARE`, actor).Scan(&role, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return active && RoleCan(role, "broadcast.send"), err
}

// A locked campaign and recipient-wide contact rows prevent duplicate batches
// across workers and limit a person to one campaign message per 24 hours.
func (db *DB) ProcessCampaigns(ctx context.Context, owners map[int64]bool, now time.Time) (int, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id FROM campaigns WHERE enabled ORDER BY last_checked_at NULLS FIRST,id LIMIT 20`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, id := range ids {
		n, err := db.processCampaign(ctx, id, owners, now)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
func (db *DB) processCampaign(ctx context.Context, id string, owners map[int64]bool, now time.Time) (int, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	c, err := scanCampaign(tx.QueryRow(ctx, `SELECT `+campaignColumns+` FROM campaigns WHERE id=$1 AND enabled FOR UPDATE SKIP LOCKED`, id))
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE campaigns SET last_checked_at=now() WHERE id=$1`, id); err != nil {
		return 0, err
	}
	allowed, err := campaignActorAllowed(ctx, tx, c.ActorID, owners)
	if err != nil {
		return 0, err
	}
	if !allowed {
		if _, err = tx.Exec(ctx, `UPDATE campaigns SET enabled=false,last_error='У автора отозваны права рассылки',revision=revision+1 WHERE id=$1`, id); err != nil {
			return 0, err
		}
		if err = cancelCampaignPending(ctx, tx, id); err != nil {
			return 0, err
		}
		return 0, tx.Commit(ctx)
	}
	if now.Before(c.StartsAt) || now.Hour() < c.SendFrom || now.Hour() >= c.SendTo {
		return 0, tx.Commit(ctx)
	}
	if c.PromoCode != "" {
		var live bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM promocodes WHERE code=$1 AND is_active AND (expires_at IS NULL OR expires_at>now()) AND (max_uses=0 OR used_count<max_uses) AND (starts_at IS NULL OR starts_at<=now()))`, c.PromoCode).Scan(&live); err != nil {
			return 0, err
		}
		if !live {
			if _, err = tx.Exec(ctx, `UPDATE campaigns SET enabled=false,last_error='Промокод недоступен',revision=revision+1 WHERE id=$1`, id); err != nil {
				return 0, err
			}
			if err = cancelCampaignPending(ctx, tx, id); err != nil {
				return 0, err
			}
			return 0, tx.Commit(ctx)
		}
	}
	var enrolled int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM broadcast_deliveries d JOIN broadcasts b ON b.id=d.broadcast_id WHERE b.campaign_id=$1 AND b.created_at>now()-interval '24 hours'`, id).Scan(&enrolled); err != nil {
		return 0, err
	}
	limit := min(100, c.DailyLimit-enrolled)
	if limit <= 0 {
		return 0, tx.Commit(ctx)
	}
	rows, err := tx.Query(ctx, campaignCandidates+` LIMIT $4`, c.Audience, c.ID, c.CooldownDays, limit)
	if err != nil {
		return 0, err
	}
	var candidates []int64
	for rows.Next() {
		var recipient int64
		if err = rows.Scan(&recipient); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, recipient)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	recipients := []int64{}
	for _, recipient := range candidates {
		tag, e := tx.Exec(ctx, `INSERT INTO campaign_contacts(telegram_id,next_allowed_at) VALUES($1,now()+interval '24 hours') ON CONFLICT(telegram_id) DO UPDATE SET next_allowed_at=excluded.next_allowed_at WHERE campaign_contacts.next_allowed_at<=now()`, recipient)
		if e != nil {
			return 0, e
		}
		if tag.RowsAffected() > 0 {
			recipients = append(recipients, recipient)
		}
	}
	if len(recipients) == 0 {
		return 0, tx.Commit(ctx)
	}
	batch := operationToken()
	if _, err = tx.Exec(ctx, `INSERT INTO broadcasts(id,actor_id,audience,message,promo_code,status,confirmed_at,campaign_id) VALUES($1,$2,$3,$4,$5,'queued',now(),$6)`, batch, c.ActorID, c.Audience, c.Text, c.PromoCode, id); err != nil {
		return 0, err
	}
	for _, recipient := range recipients {
		if _, err = tx.Exec(ctx, `INSERT INTO broadcast_deliveries(broadcast_id,telegram_id) VALUES($1,$2)`, batch, recipient); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO campaign_enrollments(campaign_id,telegram_id,last_broadcast_id) VALUES($1,$2,$3) ON CONFLICT(campaign_id,telegram_id) DO UPDATE SET last_enrolled_at=now(),last_broadcast_id=excluded.last_broadcast_id`, id, recipient, batch); err != nil {
			return 0, err
		}
	}
	return len(recipients), tx.Commit(ctx)
}

func (db *DB) BroadcastDeliveryAllowed(ctx context.Context, d BroadcastDelivery) (bool, error) {
	var audience, promoCode string
	var campaign *string
	var enabled bool
	err := db.Pool.QueryRow(ctx, `SELECT b.audience,b.campaign_id,COALESCE(c.enabled,true),b.promo_code FROM broadcasts b LEFT JOIN campaigns c ON c.id=b.campaign_id WHERE b.id=$1`, d.BroadcastID).Scan(&audience, &campaign, &enabled, &promoCode)
	if err != nil {
		return false, err
	}
	if campaign != nil && !enabled {
		return false, nil
	}
	if promoCode != "" {
		promo, e := db.PromoCode(ctx, promoCode)
		if errors.Is(e, ErrNotFound) {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		if !promo.IsActive || !promo.Live(time.Now()) || promo.Exhausted() {
			return false, nil
		}
	}
	if !BehavioralAudience(audience) {
		return true, nil
	}
	var eligible bool
	err = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (`+audienceBase+`) a WHERE a.telegram_id=$2)`, audience, d.TelegramID).Scan(&eligible)
	return eligible, err
}

// Quiet hours defer an enrolled message instead of consuming it as skipped.
func (db *DB) BroadcastDeliveryReady(ctx context.Context, d BroadcastDelivery, now time.Time) (bool, time.Duration, error) {
	allowed, err := db.BroadcastDeliveryAllowed(ctx, d)
	if err != nil || !allowed {
		return false, 0, err
	}
	var from, to int
	var start time.Time
	err = db.Pool.QueryRow(ctx, `SELECT c.send_from,c.send_to,c.starts_at FROM campaigns c JOIN broadcasts b ON b.campaign_id=c.id WHERE b.id=$1`, d.BroadcastID).Scan(&from, &to, &start)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if !now.Before(start) && now.Hour() >= from && now.Hour() < to {
		return db.reserveCampaignDelivery(ctx, d)
	}
	next := now
	if next.Before(start) {
		next = start.In(now.Location())
	}
	if next.Hour() < from {
		next = time.Date(next.Year(), next.Month(), next.Day(), from, 0, 0, 0, next.Location())
	} else if next.Hour() >= to {
		next = time.Date(next.Year(), next.Month(), next.Day()+1, from, 0, 0, 0, next.Location())
	}
	return false, max(time.Second, next.Sub(now)), nil
}
