package storage

import (
	"context"
	"errors"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"time"
)

var ErrGiftUnavailable = errors.New("подарок недоступен или уже активирован")

type Gift struct {
	ID, PlanID, PurchaseOrderID, Status string
	BuyerID                             int64
	Days, Devices                       int
	RecipientID                         *int64
	RedemptionOrderID                   *string
	CreatedAt                           time.Time
	ClaimedAt                           *time.Time
}

func (db *DB) Gifts(ctx context.Context, buyer int64) ([]Gift, error) {
	rows, err := db.Pool.Query(ctx, `SELECT g.id,g.plan_id,g.purchase_order_id,o.status,g.buyer_id,g.days,g.devices,g.recipient_id,g.redemption_order_id,g.created_at,g.claimed_at FROM subscription_gifts g JOIN orders o ON o.order_id=g.purchase_order_id WHERE g.buyer_id=$1 ORDER BY g.created_at DESC`, buyer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Gift
	for rows.Next() {
		var g Gift
		if err = rows.Scan(&g.ID, &g.PlanID, &g.PurchaseOrderID, &g.Status, &g.BuyerID, &g.Days, &g.Devices, &g.RecipientID, &g.RedemptionOrderID, &g.CreatedAt, &g.ClaimedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (db *DB) ClaimGift(ctx context.Context, code string, subject domain.Subject) (domain.Order, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)
	var buyer int64
	var recipient *int64
	var redemption *string
	var status, planID, purchaseID string
	var days, devices int
	var profile *domain.PlanProfile
	err = tx.QueryRow(ctx, `SELECT g.buyer_id,g.recipient_id,g.redemption_order_id,o.status,g.plan_id,g.purchase_order_id,g.days,g.devices,g.profile FROM subscription_gifts g JOIN orders o ON o.order_id=g.purchase_order_id WHERE g.id=$1 FOR UPDATE OF g`, code).Scan(&buyer, &recipient, &redemption, &status, &planID, &purchaseID, &days, &devices, &profile)
	if err != nil {
		return domain.Order{}, ErrGiftUnavailable
	}
	if buyer == subject.ID {
		return domain.Order{}, errors.New("свой подарок активировать нельзя")
	}
	if recipient != nil {
		if *recipient != subject.ID || redemption == nil {
			return domain.Order{}, ErrGiftUnavailable
		}
		return scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE order_id=$1`, *redemption))
	}
	if status != domain.OrderPaid && status != domain.OrderProvisioned {
		return domain.Order{}, ErrGiftUnavailable
	}
	id := "gift-claim-" + operationToken()
	username := ""
	if subject.RemnawaveUser != nil {
		username = *subject.RemnawaveUser
	}
	order, err := createOrder(ctx, tx, domain.Order{OrderID: id, Provider: "gift_claim", AccountID: &subject.ID, TelegramID: subject.TelegramID, Username: nullStringPtr(username), PlanID: planID, Kind: domain.KindSubscription, Status: domain.OrderPaid, Currency: "RUB", PlanDays: &days, DeviceCount: &devices, PlanProfile: profile})
	if err != nil {
		return domain.Order{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE orders SET plan_profile=$2 WHERE order_id=$1`, id, profile); err != nil {
		return domain.Order{}, err
	}
	order.PlanProfile = profile
	if _, err = tx.Exec(ctx, `UPDATE subscription_gifts SET recipient_id=$2,redemption_order_id=$3,claimed_at=now() WHERE id=$1`, code, subject.ID, id); err != nil {
		return domain.Order{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return order, nil
}

func (db *DB) ReadyGift(ctx context.Context, orderID string) error {
	tag, err := db.Pool.Exec(ctx, `UPDATE orders SET status='provisioned',provisioned_at=COALESCE(provisioned_at,now()),updated_at=now() WHERE order_id=$1 AND kind='gift' AND status IN ('paid','provisioned')`, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("подарок не оплачен")
	}
	return nil
}
