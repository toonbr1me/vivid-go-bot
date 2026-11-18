package storage

import (
	"context"
	"time"
)

// RecordJourney deduplicates retries/reloads within a day. It stores no links,
// device identifiers, query strings or arbitrary client-supplied metadata.
func (db *DB) RecordJourney(ctx context.Context, id int64, step, channel, platform string) error {
	_, err := db.Pool.Exec(ctx, `WITH added AS (
	 INSERT INTO customer_journey(subject_id,step,channel,platform) VALUES($1,$2,$3,$4)
	 ON CONFLICT DO NOTHING RETURNING 1)
	 INSERT INTO funnel_daily(day,step,hits) SELECT current_date,$2,count(*) FROM added HAVING count(*)>0
	 ON CONFLICT(day,step) DO UPDATE SET hits=funnel_daily.hits+EXCLUDED.hits`, id, step, channel, platform)
	return err
}

type ConnectionHistory struct {
	Known            bool
	FirstConnectedAt *time.Time
}

func (db *DB) ObserveConnection(ctx context.Context, id int64, first *time.Time) error {
	_, err := db.Pool.Exec(ctx, `WITH previous AS (
	 SELECT id,first_connected_at FROM accounts WHERE id=$1 FOR UPDATE
	), observed AS (
	 UPDATE accounts a SET connection_known=true,connection_checked_at=now(),
	 first_connected_at=CASE WHEN $2::timestamptz IS NOT NULL THEN LEAST(a.first_connected_at,$2) ELSE a.first_connected_at END
	 FROM previous p WHERE a.id=p.id RETURNING p.first_connected_at)
	 INSERT INTO funnel_daily(day,step,hits) SELECT current_date,'first_connected',count(*) FROM observed WHERE first_connected_at IS NULL AND $2::timestamptz IS NOT NULL HAVING count(*)>0
	 ON CONFLICT(day,step) DO UPDATE SET hits=funnel_daily.hits+EXCLUDED.hits`, id, first)
	return err
}

func (db *DB) CustomerConnection(ctx context.Context, id int64) (h ConnectionHistory, err error) {
	err = db.Pool.QueryRow(ctx, `SELECT connection_known,first_connected_at FROM accounts WHERE id=$1`, id).Scan(&h.Known, &h.FirstConnectedAt)
	return
}

type ExperienceStats struct {
	Viewed, Trial, Setup, Import, Connected, Paid, Repeat, Tickets, PaidOrders int64
	ConnectMinutes                                                             *float64
	TicketsPerHundred                                                          float64
}

func (db *DB) CustomerExperience(ctx context.Context, days int) (h ExperienceStats, err error) {
	err = db.Pool.QueryRow(ctx, `WITH paid AS (
	 SELECT COALESCE(account_id,(SELECT id FROM accounts a WHERE a.telegram_id=o.telegram_id AND a.merged_into IS NULL LIMIT 1)) AS owner
	 FROM orders o WHERE kind='subscription' AND status IN ('paid','provisioned') AND amount>0 AND provider<>'trial' AND created_at>now()-make_interval(days=>$1)
	), journey AS (SELECT * FROM customer_journey WHERE day>=current_date-$1::int)
	SELECT
	 (SELECT count(DISTINCT subject_id) FROM journey WHERE step='subscription_viewed'),
	 (SELECT count(DISTINCT COALESCE(account_id,(SELECT id FROM accounts a WHERE a.telegram_id=o.telegram_id AND a.merged_into IS NULL LIMIT 1))) FROM orders o WHERE provider='trial' AND status='provisioned' AND created_at>now()-make_interval(days=>$1)),
	 (SELECT count(DISTINCT subject_id) FROM journey WHERE step='setup_started'),
	 (SELECT count(DISTINCT subject_id) FROM journey WHERE step='import_requested'),
	 (SELECT count(*) FROM accounts WHERE first_connected_at>now()-make_interval(days=>$1)),
	 (SELECT count(DISTINCT owner) FROM paid),
	 (SELECT count(*) FROM (SELECT owner FROM paid WHERE owner IS NOT NULL GROUP BY owner HAVING count(*)>1) r),
	 (SELECT count(*) FROM support_tickets WHERE created_at>now()-make_interval(days=>$1)),
	 (SELECT count(*) FROM paid),
	 (SELECT percentile_cont(0.5) WITHIN GROUP(ORDER BY EXTRACT(EPOCH FROM (first_connected_at-created_at))/60)
	 FROM accounts WHERE first_connected_at>=created_at AND first_connected_at>now()-make_interval(days=>$1))`, days).Scan(&h.Viewed, &h.Trial, &h.Setup, &h.Import, &h.Connected, &h.Paid, &h.Repeat, &h.Tickets, &h.PaidOrders, &h.ConnectMinutes)
	if h.PaidOrders > 0 {
		h.TicketsPerHundred = float64(h.Tickets) * 100 / float64(h.PaidOrders)
	}
	return
}
