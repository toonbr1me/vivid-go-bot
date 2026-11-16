package payments

import (
	"context"
	"errors"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

var ErrPermission = errors.New("permission denied")

func (s *Service) CompensateTicket(ctx context.Context, actor, ticketID int64) (domain.Order, bool, error) {
	if !s.db.Permission(ctx, actor, "support.compensate", s.cfg.AdminIDs) {
		return domain.Order{}, false, ErrPermission
	}
	ticket, err := s.db.TicketByID(ctx, ticketID)
	if err != nil {
		return domain.Order{}, false, err
	}
	var subject domain.Subject
	if ticket.SubjectID != nil {
		subject, err = s.db.SubjectByID(ctx, *ticket.SubjectID)
	} else if ticket.TelegramID != nil {
		subject, err = s.db.EnsureTelegramSubject(ctx, *ticket.TelegramID)
	} else {
		err = ErrNoSubscription
	}
	if err != nil {
		return domain.Order{}, false, err
	}
	if subject.TelegramID == nil && subject.RemnawaveUser == nil {
		return domain.Order{}, false, ErrNoSubscription
	}
	unlimited := s.cfg.AdminIDs[actor]
	if member, e := s.db.StaffMemberByID(ctx, actor); e != nil {
		return domain.Order{}, false, e
	} else if member != nil && member.IsActive && member.Role == "owner" {
		unlimited = true
	}
	o, created, err := s.db.ReserveCompensation(ctx, actor, ticketID, subject, s.cfg.SupportCompensationDailyDays, unlimited, s.cfg.Currency)
	if err != nil {
		return o, created, err
	}
	// The worker resumes issuance after a timeout/crash using this same order.
	if updated, _ := s.CheckOrder(ctx, o.OrderID); updated.OrderID != "" {
		o = updated
	}
	return o, created, nil
}
