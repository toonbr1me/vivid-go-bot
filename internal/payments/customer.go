package payments

import (
	"context"
	"errors"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"time"
)

func (s *Service) CustomerGuidance(ctx context.Context, subject domain.Subject, access domain.Access, channel string) (domain.CustomerGuidance, error) {
	focus, err := s.db.CustomerFocusOrder(ctx, subject)
	if err != nil {
		return domain.CustomerGuidance{}, err
	}
	var recurring *domain.RecurringAgreement
	a, err := s.db.LatestRecurringForSubject(ctx, subject.ID)
	if err == nil {
		recurring = &a
	} else if !errors.Is(err, storage.ErrNotFound) {
		return domain.CustomerGuidance{}, err
	}
	used, err := s.db.HasTrial(ctx, subject.ID, channel)
	if err != nil {
		return domain.CustomerGuidance{}, err
	}
	days := s.cfg.TrialBotDays
	if channel == "web" {
		days = s.cfg.TrialWebDays
	}
	return domain.GuideCustomer(access, focus, recurring, !used && days > 0, time.Now()), nil
}
