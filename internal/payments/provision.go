package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// GrantTrial выдаёт бесплатный период.
//
// Заявка и заказ сохраняются одной транзакцией до выдачи. После ошибки
// продолжается тот же заказ с теми же днями и абсолютным сроком.
func (s *Service) GrantTrial(ctx context.Context, subject domain.Subject, username string, plan domain.Plan) error {
	kind := plan.TrialKind
	if kind == "" {
		kind = "bot"
	}
	orderID := fmt.Sprintf("trial-%s-%d", kind, subject.ID)
	// Незавершённый триал продолжается даже после ошибки или перезапуска.
	if existing, err := s.db.OrderByID(ctx, orderID); err == nil {
		if existing.ProvisionedAt != nil {
			return ErrTrialUsed
		}
		_, err = s.Provision(ctx, existing)
		return err
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	current, err := s.currentExpiry(ctx, subject.TelegramID, username)
	if err != nil {
		return err
	}
	if current.After(time.Now()) {
		return ErrTrialWhileActive
	}
	order, err := s.db.CreateTrialOrder(ctx, subject, kind, domain.Order{
		OrderID: orderID, Provider: "trial", TelegramID: subject.TelegramID,
		AccountID: &subject.ID, Username: ptrOrNil(username), PlanID: plan.ID,
		Kind: domain.KindSubscription, Currency: s.cfg.Current().Currency, Status: domain.OrderPaid,
		PaymentMethod: ptrOrNil("trial"), PlanDays: intPtr(plan.Days),
	})
	if errors.Is(err, storage.ErrTrialUsed) {
		return ErrTrialUsed
	}
	if err != nil {
		return err
	}
	_, err = s.Provision(ctx, order)
	return err
}

// EnsureUserExpire продлевает подписку, создавая пользователя панели при
// необходимости. Возвращает новый срок окончания.
func (s *Service) EnsureUserExpire(ctx context.Context, telegramID *int64, username string, days int, profile domain.ProvisioningProfile) (time.Time, error) {
	current, err := s.currentExpiry(ctx, telegramID, username)
	if err != nil {
		return time.Time{}, err
	}
	return s.ensureUserExpireAt(ctx, telegramID, username, extendExpireAt(current, days), profile)
}

// currentExpiry — срок подписки в панели. Нулевое время: пользователя там нет.
func (s *Service) currentExpiry(ctx context.Context, telegramID *int64, username string) (time.Time, error) {
	if !s.panelReady() {
		return time.Time{}, errors.New("панель управления не настроена")
	}
	users, err := s.findPanelUsers(ctx, telegramID, username)
	if err != nil {
		return time.Time{}, err
	}
	user := remnawave.PickUser(users)
	if user == nil {
		return time.Time{}, nil
	}
	return user.ExpireAt.Time, nil
}

// ensureUserExpireAt ставит абсолютный срок подписки.
//
// Срок передаётся, а не вычисляется здесь: повтор выдачи с тем же сроком
// ничего не меняет, поэтому панель не продлевается второй раз.
func (s *Service) ensureUserExpireAt(ctx context.Context, telegramID *int64, username string, target time.Time, profile domain.ProvisioningProfile) (time.Time, error) {
	if !s.panelReady() {
		return time.Time{}, errors.New("панель управления не настроена")
	}
	canonical := username
	if telegramID != nil {
		canonical = fmt.Sprintf("tg_%d", *telegramID)
	}
	users, err := s.findPanelUsers(ctx, telegramID, username)
	if err != nil {
		return time.Time{}, err
	}
	if len(users) > 0 {
		user := remnawave.PickUser(users)
		if user == nil {
			return time.Time{}, errors.New("панель вернула пустого пользователя")
		}
		if user.ID == 0 {
			return time.Time{}, errors.New("у пользователя панели нет идентификатора")
		}
		limitMatches := profile.TrafficLimitBytes == nil || user.TrafficLimitBytes != nil && *user.TrafficLimitBytes == *profile.TrafficLimitBytes || user.TrafficLimitBytes == nil && profile.TrafficLimitBytes != nil && *profile.TrafficLimitBytes == 0
		groupsMatch := len(profile.InternalSquadUUIDs) == 0 || slices.Equal(user.ActiveInternalSquads, profile.InternalSquadUUIDs)
		externalMatches := profile.ExternalSquadUUID == "" || user.Raw["externalSquadUuid"] != nil
		if !user.ExpireAt.Time.Before(target) && limitMatches && groupsMatch && externalMatches {
			return user.ExpireAt.Time, nil
		}
		if user.ExpireAt.Time.After(target) {
			target = user.ExpireAt.Time
		}
		params := remnawave.UpdateUserParams{UserID: user.ID, ExpireAt: &target, InternalSquads: squadsPtr(profile)}
		// A configured limit is an absolute value, so retrying is safe.
		if profile.TrafficLimitBytes != nil {
			params.TrafficLimitBytes = &profile.TrafficLimitBytes
		}
		if profile.ExternalSquadUUID != "" && user.Raw["externalSquadUuid"] == nil {
			params.ExternalSquadUUID = &profile.ExternalSquadUUID
		}
		if _, err := s.panel.UpdateUser(ctx, params); err != nil {
			return time.Time{}, err
		}
		return target, nil
	}

	type createAttempt struct {
		username string
		squad    string
	}
	attempts := []createAttempt{{username, profile.ExternalSquadUUID}}
	if profile.ExternalSquadUUID != "" {
		attempts = append(attempts, createAttempt{username, ""})
	}
	if canonical != "" && canonical != username {
		attempts = append(attempts, createAttempt{canonical, profile.ExternalSquadUUID})
		if profile.ExternalSquadUUID != "" {
			attempts = append(attempts, createAttempt{canonical, ""})
		}
	}

	var lastErr error
	for _, attempt := range attempts {
		if attempt.username == "" {
			continue
		}
		created, err := s.panel.CreateUser(ctx, remnawave.CreateUserParams{
			Username:          attempt.username,
			TelegramID:        telegramID,
			ExpireAt:          target,
			TrafficLimitBytes: profile.TrafficLimitBytes,
			ExternalSquadUUID: attempt.squad,
			InternalSquads:    profile.InternalSquadUUIDs,
			HWIDDeviceLimit:   profile.DeviceLimit,
		})
		if err != nil {
			lastErr = err
			continue
		}
		if attempt.squad == "" && profile.ExternalSquadUUID != "" && created.ID != 0 {
			if _, err := s.panel.UpdateUser(ctx, remnawave.UpdateUserParams{
				UserID:            created.ID,
				ExternalSquadUUID: &profile.ExternalSquadUUID,
				InternalSquads:    squadsPtr(profile),
			}); err == nil {
				return target, nil
			}
		}
		return target, nil
	}

	// После таймаута create мог уже выполниться. Одно стабильное имя
	// позволяет подобрать запись и не плодить дубликаты.
	if recovered, err := s.panel.UserByUsername(ctx, canonical); err == nil && recovered != nil {
		if recovered.ID != 0 {
			if _, err := s.panel.UpdateExpireAt(ctx, recovered.ID, target, profile.InternalSquadUUIDs); err == nil {
				return target, nil
			}
		}
	}
	if lastErr != nil {
		return time.Time{}, lastErr
	}
	return time.Time{}, errors.New("не удалось создать пользователя панели")
}

// provisionSubscription продлевает доступ по оплаченному заказу.
//
// Выдача идёт по шагам журнала: каждый шаг выполняется один раз, а панели
// передаётся абсолютный срок, посчитанный на первом проходе. Повторный вебхук
// или фоновая попытка не выдают второй период, не активируют промокод
// дважды и не начисляют реферальную награду дважды.
func (s *Service) provisionSubscription(ctx context.Context, order domain.Order) (domain.ProvisionResult, error) {
	plan, err := s.Plan(ctx, order.PlanID)
	if order.PlanID == "gift" && order.PlanDays != nil {
		plan = domain.Plan{ID: "gift", Days: *order.PlanDays}
		err = nil
	}
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	if plan.IsTrial && order.Provider != "trial" {
		return domain.ProvisionResult{}, errors.New("пробный период не выдаётся через оплату")
	}
	subject, err := s.subject(ctx, order.TelegramID, order.AccountID)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	unlock, err := s.db.LockSubject(ctx, subject.ID)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	defer unlock()
	// Перечитываем после блокировки: другой обработчик мог закончить выдачу.
	order, err = s.db.OrderByID(ctx, order.OrderID)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	if order.ProvisionedAt != nil {
		return domain.ProvisionResult{ExpireAt: order.ExpireAt}, nil
	}
	if order.Status != domain.OrderPaid {
		return domain.ProvisionResult{}, errors.New("заказ не оплачен")
	}
	username := deref(order.Username)
	if username == "" || subject.RemnawaveUser != nil {
		username = fallbackUsername(subject)
	}
	journal := s.db.Journal(order.OrderID)

	days, err := s.reserveDays(ctx, journal, order, plan, subject)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	target, err := s.reserveTarget(ctx, journal, order, subject, username, days)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	profile := s.paidProfile()
	if order.PlanProfile != nil {
		if order.PlanProfile.TrafficLimitBytes != nil {
			profile.TrafficLimitBytes = order.PlanProfile.TrafficLimitBytes
		}
		if order.PlanProfile.InternalSquads != nil {
			profile.InternalSquadUUIDs = order.PlanProfile.InternalSquads
		}
		if order.PlanProfile.ExternalSquadUUID != nil {
			profile.ExternalSquadUUID = *order.PlanProfile.ExternalSquadUUID
		}
	}
	upgrade := false
	if order.Provider == "trial" {
		profile = s.trialProfile(plan.TrialKind)
	} else if order.PlanID != "gift" {
		raw, err := journal.Once(ctx, "trial_upgrade", func(ctx context.Context, tx storage.Tx) (any, error) {
			return tx.TrialUpgradeNeeded(ctx, subject, order.OrderID)
		})
		if err != nil {
			return domain.ProvisionResult{}, err
		}
		if err := json.Unmarshal(raw, &upgrade); err != nil {
			return domain.ProvisionResult{}, err
		}
		if upgrade && profile.TrafficLimitBytes == nil {
			profile.TrafficLimitBytes = new(int64)
		}
	}
	if s.cfg.Current().DeviceLimitEnabled {
		managed, e := s.ManagedDeviceLimit(ctx, subject.ID)
		if e != nil {
			return domain.ProvisionResult{}, e
		}
		profile.DeviceLimit = &managed
	}
	expireAt, err := s.ensureUserExpireAt(ctx, order.TelegramID, username, target, profile)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	if upgrade && s.cfg.Current().TrialResetOnPaid {
		if err := s.resetTrialTraffic(ctx, journal, subject, username); err != nil {
			return domain.ProvisionResult{}, err
		}
	}
	if order.AccountID != nil {
		_ = s.db.SetRemnawaveUsername(ctx, *order.AccountID, username)
	}
	added, err := s.grantPlanDevices(ctx, journal, order, subject, target)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	if err := s.activatePromo(ctx, journal, order, subject); err != nil {
		return domain.ProvisionResult{}, err
	}
	var deviceLimit *int
	if s.cfg.Current().DeviceLimitEnabled {
		if user, err := s.panelUser(ctx, order.TelegramID, username); err == nil {
			if limit, err := s.SyncDeviceLimit(ctx, subject.ID, *user, false); err == nil {
				deviceLimit = &limit
			}
		}
	}
	// Не держим блокировку покупателя при работе с пригласившим:
	// взаимные приглашения иначе образовали бы цикл блокировок.
	unlock()
	var reward referralReward
	if order.Provider != "trial" && order.PlanID != "gift" && order.Provider != "gift_claim" {
		reward, err = s.rewardReferrer(ctx, journal, order, subject)
		if err != nil {
			return domain.ProvisionResult{}, err
		}
		if err := s.creditPartner(ctx, journal, order, subject); err != nil {
			return domain.ProvisionResult{}, err
		}
	}
	if err := s.db.MarkProvisioned(ctx, order.OrderID, &expireAt, &days); err != nil {
		return domain.ProvisionResult{}, err
	}
	result := domain.ProvisionResult{
		ExpireAt:           &expireAt,
		ReferralBonusGiven: reward.given,
		ReferrerID:         reward.referrerID,
		BonusDays:          reward.days,
		RefBalanceAmount:   reward.balance,
		DevicesAdded:       added,
		DeviceLimit:        deviceLimit,
		IsRenewal:          order.RenewGrantID != nil,
	}
	if user, err := s.panelUser(ctx, order.TelegramID, username); err == nil {
		current := s.cfg.Current()
		if url := serviceSubscriptionURL(current.APIBaseURL, current.SubscriptionBaseURL, *user); url != "" {
			result.SubscriptionURL = &url
		}
		result.DevicesRenewed = len(order.DeviceRenewals)
	}
	return result, nil
}

func (s *Service) resetTrialTraffic(ctx context.Context, journal *storage.Journal, subject domain.Subject, username string) error {
	resetter, ok := s.panel.(interface {
		ResetTraffic(context.Context, int64) error
	})
	if !ok {
		return errors.New("панель не поддерживает сброс пробного трафика")
	}
	user, err := s.panelUser(ctx, subject.TelegramID, username)
	if err != nil {
		return err
	}
	claimed, err := journal.ClaimTrialTrafficReset(ctx, subject)
	if err != nil || !claimed {
		return err
	}
	resetErr := resetter.ResetTraffic(ctx, user.ID)
	// ResetTraffic has no idempotency key. A timeout is recorded for review;
	// repeating it could erase traffic already used after the paid upgrade.
	result := "done"
	if resetErr != nil {
		result = "review"
		slog.Warn("сброс пробного трафика требует проверки; повтор отключён", "subject", subject.ID, "err", resetErr)
	}
	_, err = journal.Once(ctx, "trial_traffic_reset_result", func(context.Context, storage.Tx) (any, error) { return result, nil })
	return err
}

// reserveDays — сколько дней начисляем.
//
// Срок тарифа берётся из заказа: тариф могли переписать, а купленное
// останется прежним. Накопленные дни пригласившего забираются здесь же и
// только один раз — повтор выдачи не должен съедать копилку дважды.
func (s *Service) reserveDays(
	ctx context.Context, journal *storage.Journal, order domain.Order,
	plan domain.Plan, subject domain.Subject,
) (int, error) {
	raw, err := journal.Once(ctx, "subscription_days", func(ctx context.Context, tx storage.Tx) (any, error) {
		pending, err := tx.TakePendingBonus(ctx, subject.ID)
		if err != nil {
			return nil, err
		}
		days := plan.Days
		if order.PlanDays != nil && *order.PlanDays > 0 {
			days = *order.PlanDays
		}
		if order.PromoBonusDays != nil && *order.PromoBonusDays > 0 {
			days += *order.PromoBonusDays
		}
		days += pending
		if days <= 0 {
			return nil, errors.New("у заказа нет срока подписки")
		}
		return days, nil
	})
	if err != nil {
		return 0, err
	}
	var days int
	if err := json.Unmarshal(raw, &days); err != nil {
		return 0, err
	}
	if days <= 0 {
		return 0, errors.New("у заказа нет срока подписки")
	}
	return days, nil
}

// reserveTarget — абсолютный срок подписки.
//
// Он считается один раз: повтор берёт тот же срок и не продлевает панель
// второй раз.
func (s *Service) reserveTarget(
	ctx context.Context, journal *storage.Journal, order domain.Order, subject domain.Subject, username string, days int,
) (time.Time, error) {
	raw, err := journal.Once(ctx, "subscription_target", func(ctx context.Context, tx storage.Tx) (any, error) {
		current, err := s.currentExpiry(ctx, order.TelegramID, username)
		if err != nil {
			return nil, err
		}
		reserved, err := tx.ReservedExpiry(ctx, subject)
		if err != nil {
			return nil, err
		}
		if reserved.After(current) {
			current = reserved
		}
		return extendExpireAt(current, days), nil
	})
	if err != nil {
		return time.Time{}, err
	}
	var target time.Time
	// Python journals store {target, prior}; Go stores the timestamp itself.
	// Reuse either representation so recovery never allocates another period.
	if len(raw) > 0 && raw[0] == '{' {
		var legacy struct {
			Target time.Time `json:"target"`
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return time.Time{}, err
		}
		target = legacy.Target
	} else {
		if err := json.Unmarshal(raw, &target); err != nil {
			return time.Time{}, err
		}
	}
	if target.IsZero() {
		return time.Time{}, errors.New("не удалось посчитать срок подписки")
	}
	return target, nil
}

// grantPlanDevices — слоты, пришедшие вместе с тарифом.
func (s *Service) grantPlanDevices(
	ctx context.Context, journal *storage.Journal, order domain.Order,
	subject domain.Subject, target time.Time,
) (int, error) {
	if order.DeviceCount == nil || *order.DeviceCount <= 0 {
		return 0, nil
	}
	raw, err := journal.Once(ctx, "plan_device_slots", func(ctx context.Context, tx storage.Tx) (any, error) {
		granted, err := tx.GrantDeviceSlots(ctx, order.OrderID, subject.ID,
			*order.DeviceCount, nil, &order.PlanID, &target)
		if err != nil {
			return nil, err
		}
		return granted, nil
	})
	if err != nil {
		return 0, err
	}
	var granted bool
	if err := json.Unmarshal(raw, &granted); err != nil {
		return 0, err
	}
	if !granted {
		return 0, nil
	}
	return *order.DeviceCount, nil
}

// activatePromo — учёт применения промокода.
//
// Счётчик применений и журнал меняются один раз, даже если выдача
// повторяется: иначе промокод «сгорает» быстрее, чем человек его использует.
func (s *Service) activatePromo(
	ctx context.Context, journal *storage.Journal, order domain.Order, subject domain.Subject,
) error {
	_, err := journal.Once(ctx, "promo_activation", func(ctx context.Context, tx storage.Tx) (any, error) {
		if order.PromoCode == nil || *order.PromoCode == "" {
			return false, nil
		}
		if err := tx.UsePromo(ctx, *order.PromoCode); err != nil {
			return nil, err
		}
		if err := tx.RecordPromoActivation(ctx, subject.ID, *order.PromoCode); err != nil {
			return nil, err
		}
		if err := tx.ConsumePromoReservation(ctx, order.OrderID); err != nil {
			return nil, err
		}
		return true, nil
	})
	return err
}

// creditPartner начисляет партнёру программы вознаграждение за заказ.
//
// Правила задаёт промокод партнёра: процент от тарифа или фиксированная
// сумма, необязательно — только за первую покупку клиента. Начисление
// привязано к заказу, поэтому повторный вебхук не задваивает его.
func (s *Service) creditPartner(ctx context.Context, journal *storage.Journal, order domain.Order, buyer domain.Subject) error {
	if order.PartnerID == nil || *order.PartnerID == 0 {
		return nil
	}
	unlock, err := s.db.LockSubject(ctx, buyer.ID)
	if err != nil {
		return err
	}
	defer unlock()
	_, err = journal.Once(ctx, "partner_credit", func(ctx context.Context, tx storage.Tx) (any, error) {
		partnerID := *order.PartnerID
		rewardType := deref(order.RewardType)
		rewardValue := order.RewardValue
		scope := deref(order.RewardScope)

		if scope == domain.RewardScopeFirst {
			count, err := tx.PartnerRewardCount(ctx, partnerID, buyer.ID)
			if err != nil {
				return nil, err
			}
			if count > 0 {
				return false, nil
			}
		}
		amount := domain.PartnerRewardAmount(rewardType, rewardValue, order.Amount, order.RenewalAmount)
		if amount <= 0 {
			return false, nil
		}
		partner, err := tx.GetPartner(ctx, partnerID)
		if err != nil {
			return nil, err
		}
		if partner == nil {
			return false, nil
		}
		if _, err := tx.CreditPartnerReward(ctx, partnerID, order.OrderID,
			&buyer.ID, amount, partner.SubjectID); err != nil {
			return nil, err
		}
		return true, nil
	})
	return err
}

// referralReward — что получил пригласивший.
type referralReward struct {
	given      bool
	referrerID *int64
	days       int
	balance    domain.Money
}

// referralTarget — замороженная реферальная награда.
type referralTarget struct {
	SubjectID  int64        `json:"subject_id"`
	TelegramID *int64       `json:"telegram_id"`
	Days       int          `json:"days"`
	Amount     domain.Money `json:"amount"`
	UserID     int64        `json:"user_id"`
	ExpireAt   *time.Time   `json:"expire_at"`
}

// rewardReferrer начисляет вознаграждение за покупку приглашённого.
//
// Дни получает панель пригласившего, а если у него ещё нет подписки — копилка
// отложенных дней. Деньги идут на баланс только при выбранном режиме: два
// вознаграждения сразу человек не выбирал.
//
// Сумма и абсолютный срок считаются один раз и лежат в журнале: повтор
// выдачи не продлит пригласившего второй раз и не начислит награду дважды.
func (s *Service) rewardReferrer(
	ctx context.Context, journal *storage.Journal, order domain.Order, buyer domain.Subject,
) (referralReward, error) {
	reward := referralReward{}
	ref, err := s.db.ReferralByReferred(ctx, buyer.ID)
	if err != nil {
		return reward, err
	}
	if ref != nil && ref.ReferrerSubjectID != 0 {
		unlock, err := s.db.LockSubject(ctx, ref.ReferrerSubjectID)
		if err != nil {
			return reward, err
		}
		defer unlock()
	}
	raw, err := journal.Once(ctx, "referral_target", func(ctx context.Context, tx storage.Tx) (any, error) {
		ref, err := tx.ReferralByReferred(ctx, buyer.ID)
		if err != nil {
			return nil, err
		}
		if ref == nil || ref.ReferrerSubjectID == 0 {
			return nil, nil
		}
		referrer, err := tx.SubjectByID(ctx, ref.ReferrerSubjectID)
		if err != nil {
			return nil, err
		}
		mode, err := tx.ReferralMode(ctx, referrer.ID)
		if err != nil {
			return nil, err
		}
		if mode == "" {
			mode = s.cfg.Current().ReferralMode
		}
		target := referralTarget{SubjectID: referrer.ID, TelegramID: referrer.TelegramID}
		if mode == "days" || mode == "both" {
			switch ref.RewardCount {
			case 0:
				target.Days = 7
			case 1:
				target.Days = 3
			}
		}
		if (mode == "balance" || mode == "both") && s.cfg.Current().RefBalancePercent > 0 {
			base := order.Amount - order.RenewalAmount
			if base > 0 {
				target.Amount = domain.Money(int64(base) * int64(s.cfg.Current().RefBalancePercent) / 100)
			}
		}
		if target.Days > 0 && referrer.TelegramID != nil {
			users, err := s.findPanelUsers(ctx, referrer.TelegramID, "")
			if err == nil {
				if user := remnawave.PickUser(users); user != nil {
					target.UserID = user.ID
					base := user.ExpireAt.Time
					reserved, err := tx.ReservedExpiry(ctx, referrer)
					if err != nil {
						return nil, err
					}
					if reserved.After(base) {
						base = reserved
					}
					expire := extendExpireAt(base, target.Days)
					target.ExpireAt = &expire
				}
			}
		}
		return target, nil
	})
	if err != nil {
		return reward, err
	}
	var target referralTarget
	if err := json.Unmarshal(raw, &target); err != nil {
		return reward, err
	}
	if target.SubjectID == 0 {
		if order.ReferrerID != nil {
			reward.referrerID = order.ReferrerID
		}
		return reward, nil
	}
	reward.referrerID = target.TelegramID
	reward.days = target.Days
	done, err := journal.Completed(ctx, "referral_credit")
	if err != nil {
		return reward, err
	}
	if done {
		reward.balance = target.Amount
		reward.given = target.Days > 0 || target.Amount > 0
		return reward, nil
	}

	// Панель пригласившего: абсолютный срок, повтор ничего не меняет.
	if target.Days > 0 && target.UserID != 0 && target.ExpireAt != nil && s.panelReady() {
		users, err := s.findPanelUsers(ctx, target.TelegramID, "")
		if err == nil {
			if user := remnawave.PickUser(users); user != nil && user.ID == target.UserID {
				final := *target.ExpireAt
				if user.ExpireAt.Time.After(final) {
					final = user.ExpireAt.Time
				}
				if _, err := s.panel.UpdateExpireAt(ctx, target.UserID, final,
					s.paidProfile().InternalSquadUUIDs); err == nil {
					reward.given = true
				}
			}
		}
	}

	// Начисление в базе — один раз на заказ.
	if _, err := journal.Once(ctx, "referral_credit", func(ctx context.Context, tx storage.Tx) (any, error) {
		if target.Days > 0 {
			if !reward.given {
				// Панель не ответила — дни копятся и уйдут при первой
				// покупке пригласившего.
				if err := tx.AddPendingBonus(ctx, target.SubjectID, target.Days); err != nil {
					return nil, err
				}
			}
			if err := tx.IncrementReferralReward(ctx, buyer.ID); err != nil {
				return nil, err
			}
		}
		if target.Amount > 0 {
			if _, err := tx.AddBalance(ctx, target.SubjectID, target.Amount); err != nil {
				return nil, err
			}
		}
		return true, nil
	}); err != nil {
		return reward, err
	}
	if target.Days > 0 {
		reward.given = true
	}
	if target.Amount > 0 {
		reward.balance = target.Amount
		reward.given = true
	}
	return reward, nil
}

// accessRunning — есть ли у человека неистёкший доступ в панели.
func (s *Service) accessRunning(ctx context.Context, telegramID int64) bool {
	if !s.panelReady() {
		return false
	}
	users, err := s.panel.UsersByTelegramID(ctx, telegramID)
	if err != nil {
		return false
	}
	for _, user := range users {
		if user.IsActive() {
			return true
		}
	}
	return false
}

func (s *Service) findPanelUsers(ctx context.Context, telegramID *int64, lookupUsername string) ([]remnawave.User, error) {
	if !s.panelReady() {
		return nil, errors.New("панель управления не настроена")
	}
	var users []remnawave.User
	if telegramID != nil {
		var err error
		users, err = s.panel.UsersByTelegramID(ctx, *telegramID)
		if err != nil {
			return nil, err
		}
		if linked := remnawave.PickUser(users); linked != nil && (lookupUsername == "" || lookupUsername == fmt.Sprintf("tg_%d", *telegramID) || lookupUsername == linked.Username) {
			return users, nil
		}
	}
	if lookupUsername != "" {
		user, err := s.panel.UserByUsername(ctx, lookupUsername)
		if err != nil {
			return nil, err
		}
		if user != nil {
			if telegramID != nil && user.TelegramID != nil && *user.TelegramID != *telegramID {
				return nil, errors.New("подписка привязана к другому Telegram")
			}
			if linked := remnawave.PickUser(users); linked != nil && linked.ID != user.ID {
				return nil, errors.New("обнаружены разные подписки веб-аккаунта и Telegram: обратитесь в поддержку")
			}
			return []remnawave.User{*user}, nil
		}
	}
	if telegramID == nil && lookupUsername == "" {
		return nil, errors.New("нужно имя пользователя панели")
	}
	return users, nil
}

func (s *Service) panelUser(ctx context.Context, telegramID *int64, username string) (*remnawave.User, error) {
	users, err := s.findPanelUsers(ctx, telegramID, username)
	if err != nil {
		return nil, err
	}
	user := remnawave.PickUser(users)
	if user == nil {
		return nil, storage.ErrNotFound
	}
	return user, nil
}

// extendExpireAt — срок продления: остаток не сгорает, истёкший срок не
// тянется в прошлое.
func extendExpireAt(current time.Time, days int) time.Time {
	now := time.Now().UTC()
	base := now
	if !current.IsZero() && current.After(now) {
		base = current.UTC()
	}
	return base.AddDate(0, 0, days)
}

func squadsPtr(profile domain.ProvisioningProfile) *[]string {
	if len(profile.InternalSquadUUIDs) == 0 {
		return nil
	}
	squads := append([]string(nil), profile.InternalSquadUUIDs...)
	return &squads
}
