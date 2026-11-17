package payments

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

func (s *Service) ClaimGift(ctx context.Context, subject domain.Subject, code string) (domain.Order, error) {
	order, err := s.db.ClaimGift(ctx, code, subject)
	if err != nil {
		return order, err
	}
	// A durable redemption is successful even while the panel is unavailable.
	// The existing paid-order worker resumes the same redemption after restart.
	if _, err = s.Provision(ctx, order); err == nil {
		order, _ = s.db.OrderByID(ctx, order.OrderID)
	}
	return order, nil
}

func (s *Service) snapshotPlan(p domain.Plan) *domain.PlanProfile {
	global := s.paidProfile()
	traffic := global.TrafficLimitBytes
	if traffic == nil {
		traffic = new(int64)
	}
	if p.TrafficLimitBytes != nil {
		traffic = p.TrafficLimitBytes
	}
	squads := append([]string{}, global.InternalSquadUUIDs...)
	if p.InternalSquads != nil {
		squads = append([]string{}, p.InternalSquads...)
	}
	external := global.ExternalSquadUUID
	if p.ExternalSquadUUID != nil {
		external = *p.ExternalSquadUUID
	}
	base := s.cfg.Current().DeviceLimitBase
	if p.Devices > 0 && p.Devices < base {
		base = p.Devices
	}
	limit := *traffic
	return &domain.PlanProfile{BaseDevices: &base, TrafficLimitBytes: &limit, InternalSquads: squads, ExternalSquadUUID: &external}
}
