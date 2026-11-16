package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// Отказы, которые обработчики показывают человеку отдельным текстом.
var (
	ErrTrialUsed        = errors.New("пробный период этого вида уже использован")
	ErrTrialWhileActive = errors.New("подписка ещё действует — пробный период не положен")
	ErrPromoRejected    = storage.ErrPromoRejected
	ErrNoProviders      = errors.New("платежи не настроены")
	ErrPlanUnavailable  = errors.New("тариф снят с продажи")
	ErrPackUnavailable  = errors.New("пакет устройств снят с продажи")
	ErrNoSubscription   = errors.New("нет действующей подписки")
)

// Panel — часть панели, которой пользуется выдача.
//
// Отдельный интерфейс вместо concrete-типа: тесты выдачи должны уметь
// проверить, что подписка продлилась ровно один раз, а панели под рукой у
// них нет.
type Panel interface {
	UsersByTelegramID(ctx context.Context, telegramID int64) ([]remnawave.User, error)
	UserByUsername(ctx context.Context, username string) (*remnawave.User, error)
	CreateUser(ctx context.Context, params remnawave.CreateUserParams) (remnawave.User, error)
	UpdateUser(ctx context.Context, params remnawave.UpdateUserParams) (remnawave.User, error)
	UpdateExpireAt(ctx context.Context, userID int64, expireAt time.Time, squads []string) (remnawave.User, error)
	HWIDDevices(ctx context.Context, userID int64) ([]remnawave.HWIDDevice, error)
	DeleteHWIDDevice(ctx context.Context, userID int64, hwid string) (bool, error)
}

// Service — единственное место, где деньги превращаются в доступ.
type Service struct {
	db       *storage.DB
	panel    Panel
	registry *Registry
	cfg      *config.Settings

	lockMu sync.Mutex
	locks  map[string]*sync.Mutex
}

// New создаёт службу. panel может быть nil: бот и сайт поднимаются и без
// панели, просто покупка не сможет выдать доступ.
func New(db *storage.DB, panel Panel, registry *Registry, cfg *config.Settings) *Service {
	return &Service{
		db:       db,
		panel:    panel,
		registry: registry,
		cfg:      cfg,
		locks:    map[string]*sync.Mutex{},
	}
}

// Registry возвращает шлюзы: витрине нужны способы оплаты и цена в звёздах.
func (s *Service) Registry() *Registry { return s.registry }

// Panel — клиент панели для экранов, которым нужны данные сверх подписки.
func (s *Service) Panel() Panel { return s.panel }

// Plans собирает пробные и платные тарифы.
func (s *Service) Plans(ctx context.Context) (map[string]domain.Plan, error) {
	paid, err := s.db.PaidPlans(ctx)
	if err != nil {
		return nil, err
	}
	return domain.Catalogue(s.cfg.TrialWebDays, s.cfg.TrialBotDays, paid), nil
}

// Plan резолвит тариф, включая снятый с продажи: на него ссылаются уже
// оплаченные заказы и история покупок.
func (s *Service) Plan(ctx context.Context, planID string) (domain.Plan, error) {
	plans, err := s.Plans(ctx)
	if err != nil {
		return domain.Plan{}, err
	}
	plan, ok := plans[planID]
	if !ok {
		return domain.Plan{}, fmt.Errorf("неизвестный тариф %q", planID)
	}
	return plan, nil
}

// SellablePlans — платные тарифы в порядке витрины.
func (s *Service) SellablePlans(ctx context.Context) ([]domain.Plan, error) {
	paid, err := s.db.PaidPlans(ctx)
	if err != nil {
		return nil, err
	}
	return domain.SortedPlans(paid), nil
}

// PaymentOptions — способы оплаты для экрана.
func (s *Service) PaymentOptions(web bool) []domain.PaymentOption {
	return s.registry.Options(web)
}

// Order читает заказ по идентификатору.
func (s *Service) Order(ctx context.Context, orderID string) (domain.Order, error) {
	return s.db.OrderByID(ctx, orderID)
}

// StarsAmount — сколько звёзд стоит сумма. Ноль — звёзды выключены.
func (s *Service) StarsAmount(amount domain.Money) int {
	return s.registry.StarsAmount(amount)
}

// QuotePlans показывает цены с учётом промокода, ничего не записывая.
func (s *Service) QuotePlans(ctx context.Context, subjectID int64, promoCode string) ([]domain.PlanQuote, error) {
	plans, err := s.SellablePlans(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.PlanQuote, 0, len(plans))
	for _, plan := range plans {
		quote := domain.PlanQuote{Plan: plan, Price: plan.Price, Amount: plan.Price}
		if promoCode != "" {
			resolution, reject := s.resolvePromo(ctx, subjectID, promoCode, plan)
			if reject != nil {
				quote.Rejection = reject.Error()
			} else {
				quote.BonusDays = resolution.BonusDays
				quote.Amount = domain.QuotePrice(plan.Price, resolution.DiscountPercent,
					resolution.DiscountFixed, s.cfg.MinPaymentAmount)
			}
		}
		out = append(out, quote)
	}
	return out, nil
}

// Purchase — заказ подписки из бота или с сайта.
type Purchase struct {
	TelegramID *int64
	AccountID  *int64
	Username   string
	PlanID     string
	Method     string
	Option     string
	PromoCode  string
	ReferrerID *int64
	Web        bool
	FromWebapp bool
	Recurring  bool
}

// Checkout создаёт счёт или выдаёт бесплатный доступ, если промокод закрыл
// цену целиком.
func (s *Service) Checkout(ctx context.Context, purchase Purchase) (domain.Checkout, error) {
	plan, err := s.Plan(ctx, purchase.PlanID)
	if err != nil {
		return domain.Checkout{}, err
	}
	if !plan.IsTrial && !plan.IsActive {
		return domain.Checkout{}, ErrPlanUnavailable
	}
	subject, err := s.subject(ctx, purchase.TelegramID, purchase.AccountID)
	if err != nil {
		return domain.Checkout{}, err
	}
	if plan.IsTrial {
		if purchase.Recurring {
			return domain.Checkout{}, errors.New("пробный доступ не включает автосписания")
		}
		return s.checkoutTrial(ctx, subject, purchase, plan)
	}
	if !s.registry.Enabled() && purchase.PromoCode == "" {
		return domain.Checkout{}, ErrNoProviders
	}

	outcome, err := s.applyPromo(ctx, subject.ID, plan, purchase.PromoCode)
	if err != nil {
		return domain.Checkout{}, err
	}

	deviceAmount := domain.Money(0)
	total := outcome.Amount + deviceAmount
	orderID := s.newOrderID(purchase.TelegramID, purchase.AccountID)

	if total <= 0 {
		if purchase.Recurring {
			return domain.Checkout{}, errors.New("для бесплатной покупки выключите автопродление; способ оплаты не будет сохранён")
		}
		return s.settleFreeOrder(ctx, orderID, subject, purchase, plan, outcome)
	}

	provider, providerName, method, err := s.pickProvider(purchase.Option, purchase.Method, purchase.Web)
	if err != nil {
		return domain.Checkout{}, err
	}
	if purchase.Recurring && !s.RecurringSupported(domain.PaymentOption{Provider: providerName, Method: method}, plan.Days) {
		return domain.Checkout{}, errors.New("автопродление недоступно для этого способа оплаты")
	}

	unlock := s.lockFor(subject.ID)
	defer unlock()
	if existing := s.findLiveOrder(ctx, subject, "subscription", plan.ID, providerName,
		method, total, purchase.Recurring); existing != nil {
		return domain.Checkout{
			URL:       deref(existing.PaymentURL),
			OrderID:   existing.OrderID,
			Amount:    existing.Amount,
			ExpiresAt: existing.PaymentExpiresAt,
			Reused:    true,
		}, nil
	}

	successURL, failURL := s.returnURLs()
	request := CreateRequest{
		OrderID:     orderID,
		Amount:      total,
		Currency:    s.cfg.Currency,
		Description: "Подписка на цифровой сервис, " + plan.Title,
		ExpiresIn:   time.Duration(s.cfg.LinkTTLMinutes) * time.Minute,
		SuccessURL:  successURL,
		FailURL:     failURL,
		Method:      method,
		Recurring:   purchase.Recurring,
		PlanDays:    plan.Days,
	}
	if subject.TelegramID != nil {
		request.CustomerEmail = fmt.Sprintf("%d@telegram.org", *subject.TelegramID)
	} else if subject.Email != nil {
		request.CustomerEmail = *subject.Email
	}
	if purchase.Recurring && request.CustomerEmail == "" && providerName == "platega" {
		return domain.Checkout{}, errors.New("для подписки Platega нужен email")
	}
	expiry := time.Now().Add(time.Duration(s.cfg.LinkTTLMinutes) * time.Minute)
	expires := &expiry

	order := domain.Order{
		OrderID:          orderID,
		Provider:         providerName,
		TelegramID:       subject.TelegramID,
		AccountID:        accountIDPtr(subject, purchase),
		Username:         ptrOrNil(purchase.Username),
		PlanID:           plan.ID,
		Kind:             domain.KindSubscription,
		Amount:           total,
		OriginalAmount:   outcome.OriginalAmount + deviceAmount,
		Currency:         s.cfg.Currency,
		PaymentMethod:    ptrOrNil(method),
		Status:           domain.OrderPending,
		PaymentExpiresAt: expires,
		PromoCode:        ptrOrNil(outcome.Code),
		PlanDays:         intPtr(plan.Days),
		PromoBonusDays:   intPtr(outcome.BonusDays),
		ReferrerID:       purchase.ReferrerID,
		PartnerID:        outcome.PartnerID,
		RewardType:       outcome.RewardType,
		RewardValue:      outcome.RewardValue,
		RewardScope:      outcome.RewardScope,
		DeviceCount:      s.planExtraDevices(plan),
	}
	if purchase.Recurring {
		order.RecurringAgreementID = &orderID
	}
	link, err := s.createPaymentOrder(ctx, provider, order, request)
	if err != nil {
		return domain.Checkout{}, err
	}
	expires = link.ExpiresAt
	return domain.Checkout{
		URL:       link.URL,
		OrderID:   orderID,
		Amount:    total,
		ExpiresAt: expires,
		Stars:     link.Stars,
	}, nil
}

// checkoutTrial выдаёт бесплатный период без платежа.
func (s *Service) checkoutTrial(ctx context.Context, subject domain.Subject, purchase Purchase, plan domain.Plan) (domain.Checkout, error) {
	expected := "bot"
	if purchase.Web && !purchase.FromWebapp {
		expected = "web"
	}
	if plan.TrialKind != expected {
		return domain.Checkout{}, errors.New("этот пробный период выдаётся в другом канале")
	}
	if plan.TrialKind == "bot" && subject.TelegramID == nil {
		return domain.Checkout{}, errors.New("для пробного периода нужен Telegram")
	}
	username := purchase.Username
	if username == "" {
		username = fallbackUsername(subject)
	}
	if err := s.GrantTrial(ctx, subject, username, plan); err != nil {
		return domain.Checkout{}, err
	}
	return domain.Checkout{
		OrderID: fmt.Sprintf("%s-%d", plan.ID, subject.ID),
		Trial:   true,
		Free:    true,
	}, nil
}

// Webapp — покупка пришла из мини-приложения Telegram. Поле выставляется
// вызывающим, а не угадывается по наличию Telegram: веб-аккаунт тоже может
// быть привязан.
// settleFreeOrder пишет заказ сразу оплаченным: платить нечего, но выдача
// идёт тем же путём, что и у оплаты балансом.
func (s *Service) settleFreeOrder(ctx context.Context, orderID string, subject domain.Subject, purchase Purchase, plan domain.Plan, outcome promoOutcome) (domain.Checkout, error) {
	order := domain.Order{
		OrderID:        orderID,
		Provider:       "promo",
		TelegramID:     subject.TelegramID,
		AccountID:      accountIDPtr(subject, purchase),
		Username:       ptrOrNil(purchase.Username),
		PlanID:         plan.ID,
		Kind:           domain.KindSubscription,
		Amount:         0,
		OriginalAmount: outcome.OriginalAmount,
		Currency:       s.cfg.Currency,
		PaymentMethod:  ptrOrNil("promo"),
		Status:         domain.OrderPaid,
		PromoCode:      ptrOrNil(outcome.Code),
		PlanDays:       intPtr(plan.Days),
		PromoBonusDays: intPtr(outcome.BonusDays),
		ReferrerID:     purchase.ReferrerID,
		PartnerID:      outcome.PartnerID,
		RewardType:     outcome.RewardType,
		RewardValue:    outcome.RewardValue,
		RewardScope:    outcome.RewardScope,
		DeviceCount:    s.planExtraDevices(plan),
	}
	created, err := s.db.CreateOrder(ctx, order)
	if err != nil {
		return domain.Checkout{}, err
	}
	if _, err := s.Provision(ctx, created); err != nil {
		// Заказ уже оплачен: фоновый повтор доведёт выдачу, а человеку
		// показываем успех — деньги не списывались.
		return domain.Checkout{OrderID: orderID, Free: true}, nil
	}
	return domain.Checkout{OrderID: orderID, Free: true}, nil
}

// promoOutcome — замороженная скидка и вознаграждение партнёра.
type promoOutcome struct {
	Amount         domain.Money
	OriginalAmount domain.Money
	Code           string
	BonusDays      int
	PartnerID      *int64
	RewardType     *string
	RewardValue    domain.Money
	RewardScope    *string
}

// applyPromo считает цену со скидкой и запоминает параметры вознаграждения.
func (s *Service) applyPromo(ctx context.Context, subjectID int64, plan domain.Plan, code string) (promoOutcome, error) {
	out := promoOutcome{Amount: plan.Price, OriginalAmount: plan.Price}
	if strings.TrimSpace(code) == "" {
		return out, nil
	}
	resolution, reject := s.resolvePromo(ctx, subjectID, code, plan)
	if reject != nil {
		return promoOutcome{}, reject
	}
	out.Code = resolution.Code
	out.Amount = domain.QuotePrice(plan.Price, resolution.DiscountPercent,
		resolution.DiscountFixed, s.cfg.MinPaymentAmount)
	out.BonusDays = resolution.BonusDays
	out.PartnerID = resolution.PartnerID
	if resolution.RewardType != "" {
		out.RewardType = &resolution.RewardType
	}
	out.RewardValue = resolution.RewardValue
	if resolution.RewardScope != "" {
		out.RewardScope = &resolution.RewardScope
	}
	return out, nil
}

// resolvePromo проверяет код до создания счёта: после отправки суммы шлюзу
// поменять её уже нельзя.
func (s *Service) resolvePromo(ctx context.Context, subjectID int64, raw string, plan domain.Plan) (domain.PromoResolution, error) {
	code, ok := domain.NormalizePromoCode(raw)
	if !ok {
		return domain.PromoResolution{}, fmt.Errorf("%w: неверный формат", ErrPromoRejected)
	}
	promo, err := s.db.PromoCode(ctx, code)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return domain.PromoResolution{}, fmt.Errorf("%w: код не найден", ErrPromoRejected)
		}
		return domain.PromoResolution{}, err
	}
	now := time.Now()
	if !promo.IsActive || !promo.Live(now) || promo.Exhausted() {
		return domain.PromoResolution{}, fmt.Errorf("%w: код неактивен или истёк", ErrPromoRejected)
	}
	if !promo.AppliesToPlan(plan.ID) {
		return domain.PromoResolution{}, fmt.Errorf("%w: не действует на этот тариф", ErrPromoRejected)
	}
	uses, err := s.db.PromoActivationsForCode(ctx, subjectID, code)
	if err != nil {
		return domain.PromoResolution{}, err
	}
	if promo.PerUserLimit > 0 && uses >= promo.PerUserLimit {
		return domain.PromoResolution{}, fmt.Errorf("%w: лимит применений исчерпан", ErrPromoRejected)
	}
	if s.cfg.PromoMaxCodesPerUser > 0 && uses == 0 {
		distinct, err := s.db.DistinctPromoCount(ctx, subjectID)
		if err != nil {
			return domain.PromoResolution{}, err
		}
		if distinct >= s.cfg.PromoMaxCodesPerUser {
			return domain.PromoResolution{}, fmt.Errorf("%w: превышен общий лимит кодов", ErrPromoRejected)
		}
	}

	percent := promo.DiscountPercent
	fixed := promo.DiscountFixed
	if uses > 0 {
		percent = promo.RepeatDiscountPct
		fixed = promo.RepeatDiscountFixed
		if percent == 0 && fixed == 0 {
			return domain.PromoResolution{}, fmt.Errorf("%w: код уже использован", ErrPromoRejected)
		}
	}
	if percent == 0 && fixed == 0 && promo.BonusDays == 0 {
		return domain.PromoResolution{}, fmt.Errorf("%w: код не даёт скидки", ErrPromoRejected)
	}
	return domain.PromoResolution{
		Code:            code,
		DiscountPercent: percent,
		DiscountFixed:   fixed,
		BonusDays:       promo.BonusDays,
		PartnerID:       promo.PartnerID,
		RewardType:      promo.RewardType,
		RewardValue:     promo.RewardValue,
		RewardScope:     promo.RewardScope,
	}, nil
}

// RedeemGiftPromo выдаёт дни по коду без покупки.
func (s *Service) RedeemGiftPromo(ctx context.Context, subject domain.Subject, raw string) (int, error) {
	code, ok := domain.NormalizePromoCode(raw)
	if !ok {
		return 0, fmt.Errorf("%w: неверный формат", ErrPromoRejected)
	}
	// Повтор после сбоя продолжает уже зарезервированную выдачу.
	orders, err := s.db.OrderHistoryForSubject(ctx, subject, 100)
	if err != nil {
		return 0, err
	}
	for _, order := range orders {
		if order.Provider == "promo_gift" && deref(order.PromoCode) == code && order.Status == domain.OrderPaid {
			if _, err := s.Provision(ctx, order); err != nil {
				return 0, err
			}
			return *order.PlanDays, nil
		}
	}
	promo, err := s.db.PromoCode(ctx, code)
	if err != nil {
		return 0, fmt.Errorf("%w: код не найден", ErrPromoRejected)
	}
	if !promo.IsGiftPromo() {
		return 0, fmt.Errorf("%w: это не подарочный код", ErrPromoRejected)
	}
	if !promo.IsActive || !promo.Live(time.Now()) {
		return 0, fmt.Errorf("%w: код неактивен или истёк", ErrPromoRejected)
	}
	if promo.Exhausted() {
		return 0, fmt.Errorf("%w: код исчерпан", ErrPromoRejected)
	}

	// Подарок не должен выдаваться дважды одному человеку.
	uses, err := s.db.PromoActivationsForCode(ctx, subject.ID, code)
	if err != nil {
		return 0, err
	}
	if promo.PerUserLimit > 0 {
		if uses >= promo.PerUserLimit {
			return 0, fmt.Errorf("%w: лимит применений исчерпан", ErrPromoRejected)
		}
	} else if uses > 0 {
		// Без своего лимита подарок остаётся одноразовым: повторной ставки у
		// него нет, и второй раз он не даёт ничего.
		return 0, fmt.Errorf("%w: промокод уже был использован", ErrPromoRejected)
	}
	if s.cfg.PromoMaxCodesPerUser > 0 {
		distinct, err := s.db.DistinctPromoCount(ctx, subject.ID)
		if err != nil {
			return 0, err
		}
		if distinct >= s.cfg.PromoMaxCodesPerUser {
			return 0, fmt.Errorf("%w: превышен общий лимит кодов", ErrPromoRejected)
		}
	}

	username := fallbackUsername(subject)
	order, err := s.db.CreateOrder(ctx, domain.Order{
		OrderID: s.newOrderID(subject.TelegramID, &subject.ID), Provider: "promo_gift",
		TelegramID: subject.TelegramID, AccountID: &subject.ID, Username: &username,
		PlanID: "gift", Kind: domain.KindSubscription, Currency: s.cfg.Currency,
		Status: domain.OrderPaid, PromoCode: &code, PlanDays: intPtr(promo.GrantDays),
	})
	if err != nil {
		return 0, err
	}
	if _, err := s.Provision(ctx, order); err != nil {
		return 0, err
	}
	return promo.GrantDays, nil
}

// Provision выдаёт оплаченный заказ: подписку, устройства или продление.
func (s *Service) Provision(ctx context.Context, order domain.Order) (domain.ProvisionResult, error) {
	switch order.Kind {
	case domain.KindDevices:
		if order.RenewGrantID != nil && *order.RenewGrantID != "" {
			return s.provisionDeviceRenewal(ctx, order)
		}
		return s.provisionDevices(ctx, order)
	default:
		return s.provisionSubscription(ctx, order)
	}
}

// HandleEvent применяет уведомление шлюза. Возвращает false, если событие не
// изменило заказ: повторный вебхук не должен выдавать второй период.
func (s *Service) HandleEvent(ctx context.Context, providerName string, event Event) (bool, error) {
	if event.OrderID == "" {
		return false, nil
	}
	order, err := s.db.OrderByID(ctx, event.OrderID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if providerName != "" && order.Provider != providerName {
		return false, fmt.Errorf("заказ %s принадлежит шлюзу %s", order.OrderID, order.Provider)
	}
	if event.Amount != nil && *event.Amount != order.Amount {
		return false, fmt.Errorf("сумма %s не совпала с заказом %s", event.Amount.String(), order.Amount.String())
	}
	if event.Currency != "" && !strings.EqualFold(event.Currency, order.Currency) {
		return false, fmt.Errorf("валюта %s не совпала с заказом %s", event.Currency, order.Currency)
	}
	if event.Status == StatusPaid && providerName == "yookassa" && order.RecurringAgreementID != nil && event.SavedMethodID != "" {
		if err := s.db.SetRecurringExternal(ctx, *order.RecurringAgreementID, event.SavedMethodID, true); err != nil {
			return false, err
		}
	}
	switch event.Status {
	case StatusPaid:
		paidAt := time.Now()
		if event.PaidAt != nil {
			paidAt = *event.PaidAt
		}
		updated, err := s.db.MarkPaid(ctx, order.OrderID, event.TransactionID, paidAt)
		if err != nil {
			return false, err
		}
		if order.ProvisionedAt != nil {
			// Заказ уже выдан: повторный вебхук ничего не меняет.
			return false, nil
		}
		if !updated && order.Status != domain.OrderPaid {
			return false, nil
		}
		if _, err := s.Provision(ctx, order); err != nil {
			return true, err
		}
		return true, nil
	case StatusFailed, StatusCancelled:
		if order.Status == domain.OrderPending {
			if err := s.db.MarkOrderStatus(ctx, order.OrderID, domain.OrderFailed); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

// ProvisionPending доводит оплаченные, но не выданные заказы. Фоновая задача
// зовёт его после сбоя панели: человек уже заплатил.
func (s *Service) ProvisionPending(ctx context.Context, limit int) (int, error) {
	orders, err := s.db.PendingProvisioning(ctx, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, order := range orders {
		if _, err := s.Provision(ctx, order); err != nil {
			_ = s.db.ScheduleProvisionRetry(ctx, order.OrderID, err.Error())
			continue
		}
		done++
	}
	return done, nil
}

// pickProvider выбирает шлюз по паре «шлюз × способ», а без неё — первый.
func (s *Service) pickProvider(option, method string, web bool) (Provider, string, string, error) {
	if option != "" {
		providerName, resolvedMethod, err := s.registry.ResolveOption(option, web)
		if err != nil {
			return nil, "", "", err
		}
		if resolvedMethod != "" {
			method = resolvedMethod
		}
		if providerName != "" {
			provider, ok := s.registry.Get(providerName)
			if !ok {
				return nil, "", "", fmt.Errorf("провайдер %q не подключён", providerName)
			}
			return provider, providerName, method, nil
		}
	}
	provider, ok := s.registry.First(web)
	if !ok {
		return nil, "", "", ErrNoProviders
	}
	return provider, provider.Name(), method, nil
}

// findLiveOrder ищет тот же счёт, чтобы повторное нажатие не открывало
// второй платёж.
func (s *Service) findLiveOrder(ctx context.Context, subject domain.Subject, kind, planID, providerName, method string, amount domain.Money, recurring ...bool) *domain.Order {
	orders, err := s.db.LivePendingOrders(ctx, subject.TelegramID, accountIDPtr(subject, Purchase{}), kind)
	if err != nil {
		return nil
	}
	for i := range orders {
		order := orders[i]
		want := len(recurring) > 0 && recurring[0]
		if (order.RecurringAgreementID != nil) != want {
			continue
		}
		if order.PlanID != planID || order.Provider != providerName || order.Amount != amount {
			continue
		}
		if deref(order.PaymentMethod) != method {
			continue
		}
		return &order
	}
	return nil
}

func (s *Service) lockFor(subjectID int64) func() {
	key := fmt.Sprintf("subject:%d", subjectID)
	s.lockMu.Lock()
	lock, ok := s.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		s.locks[key] = lock
	}
	s.lockMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// subject разрешает владельца заказа.
func (s *Service) subject(ctx context.Context, telegramID, accountID *int64) (domain.Subject, error) {
	if telegramID != nil {
		return s.db.EnsureTelegramSubject(ctx, *telegramID)
	}
	if accountID != nil {
		return s.db.SubjectByID(ctx, *accountID)
	}
	return domain.Subject{}, errors.New("нужен telegram_id или account_id")
}

// EnsureSubject — публичная обёртка для обработчиков.
func (s *Service) EnsureSubject(ctx context.Context, telegramID *int64, accountID *int64) (domain.Subject, error) {
	return s.subject(ctx, telegramID, accountID)
}

func (s *Service) newOrderID(telegramID *int64, accountID *int64) string {
	if telegramID != nil {
		return fmt.Sprintf("tg-%d-%s", *telegramID, randomHex(6))
	}
	if accountID != nil {
		return fmt.Sprintf("web-%d-%s", *accountID, randomHex(6))
	}
	return fmt.Sprintf("ord-%s", randomHex(8))
}

func (s *Service) returnURLs() (string, string) {
	base := strings.TrimRight(s.cfg.SiteBaseURL, "/")
	if !s.cfg.SiteEnabled || !strings.HasPrefix(base, "https://") {
		base = ""
	}
	success := s.cfg.SuccessRedirect
	if success == "" && base != "" {
		success = base + "/pay/success"
	}
	fail := s.cfg.FailRedirect
	if fail == "" && base != "" {
		fail = base + "/pay/fail"
	}
	return success, fail
}

// ReturnURLs нужен веб-слою для собственных страниц успеха.
func (s *Service) ReturnURLs() (string, string) { return s.returnURLs() }

func (s *Service) paidProfile() domain.ProvisioningProfile {
	return domain.ProvisioningProfile{
		ExternalSquadUUID:  s.cfg.ExternalSquadUUID,
		InternalSquadUUIDs: s.cfg.InternalSquads,
		TrafficLimitBytes:  s.cfg.TrafficLimitBytes,
	}
}

func (s *Service) trialProfile(kind string) domain.ProvisioningProfile {
	profile := s.paidProfile()
	squads := s.cfg.TrialWebSquads
	if kind == "bot" {
		squads = s.cfg.TrialBotSquads
	}
	if len(squads) > 0 {
		profile.InternalSquadUUIDs = squads
	}
	if s.cfg.TrialTrafficBytes != nil {
		profile.TrafficLimitBytes = s.cfg.TrialTrafficBytes
	}
	return profile
}

func (s *Service) planExtraDevices(plan domain.Plan) *int {
	if !s.cfg.DeviceLimitEnabled || plan.Devices <= 0 {
		return nil
	}
	extra := plan.Devices - s.cfg.DeviceLimitBase
	if extra < 0 {
		extra = 0
	}
	return &extra
}

func fallbackUsername(subject domain.Subject) string {
	if subject.RemnawaveUser != nil && strings.TrimSpace(*subject.RemnawaveUser) != "" {
		return *subject.RemnawaveUser
	}
	if subject.TelegramID != nil {
		return fmt.Sprintf("tg_%d", *subject.TelegramID)
	}
	return fmt.Sprintf("web_%d", subject.ID)
}

func accountIDPtr(subject domain.Subject, purchase Purchase) *int64 {
	if purchase.AccountID != nil {
		return purchase.AccountID
	}
	if subject.TelegramID == nil {
		id := subject.ID
		return &id
	}
	return nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ptrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intPtr(value int) *int { return &value }

func randomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
