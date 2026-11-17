package payments

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
)

// Отказы на экране устройств.
var (
	ErrDevicesDisabled    = errors.New("учёт устройств выключен")
	ErrDeviceTermTooShort = errors.New("для докупки устройств нужен хотя бы день подписки")
	ErrGrantNotRenewable  = errors.New("это начисление нельзя продлить")
	ErrGrantNotOwned      = errors.New("начисление принадлежит другому аккаунту")
	ErrUnknownDevicePack  = errors.New("пакет устройств не найден")
)

// devicePurchaseMinRemaining — меньше суток остатка не считаем: пакет
// докупается только к заметному сроку подписки.
const devicePurchaseMinRemaining = 24 * time.Hour

// unknownPaymentLinkLifetime — сколько живёт ссылка, если шлюз не сказал.
const unknownPaymentLinkLifetime = 30 * time.Minute

// DevicePurchase — докупка пакета из бота или с сайта.
type DevicePurchase struct {
	TelegramID   *int64
	AccountID    *int64
	Username     string
	PackID       string
	Option       string
	Method       string
	GrantOrderID string
	Web          bool
}

// DeviceQuotes считает пакеты для действующей подписки. Пусто — либо срок
// меньше суток, либо продавать нечего.
func (s *Service) DeviceQuotes(ctx context.Context, expiresAt *time.Time, now time.Time) ([]domain.DeviceQuote, error) {
	if !s.cfg.Current().DeviceLimitEnabled || expiresAt == nil {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if expiresAt.Sub(now) < devicePurchaseMinRemaining {
		return nil, nil
	}
	packs, err := s.db.DevicePacks(ctx)
	if err != nil {
		return nil, err
	}
	sellable := domain.SortedDevicePacks(packs)
	out := make([]domain.DeviceQuote, 0, len(sellable))
	for _, pack := range sellable {
		quote, err := s.quoteDevicePack(pack, *expiresAt, nil, now)
		if err != nil {
			continue
		}
		out = append(out, quote)
	}
	return out, nil
}

// quoteDevicePack считает цену пакета за непокрытый остаток подписки.
//
// Цена каталога — цена за 30 суток. Итог округляется вверх до целого рубля,
// чтобы на витрине и у шлюза всегда была одна и та же сумма. Для продления
// уже оплаченный промежуток (coveredUntil) второй раз не оплачивается.
func (s *Service) quoteDevicePack(
	pack domain.DevicePack, expiresAt time.Time, coveredUntil *time.Time, now time.Time,
) (domain.DeviceQuote, error) {
	start := now
	if coveredUntil != nil && coveredUntil.After(start) {
		start = *coveredUntil
	}
	seconds := expiresAt.Sub(start).Seconds()
	if seconds <= 0 {
		return domain.DeviceQuote{}, errors.New("слоты уже покрывают срок подписки")
	}
	billedDays := int(math.Ceil(seconds / 86400))
	if billedDays < 1 {
		billedDays = 1
	}
	rubles := math.Ceil(float64(pack.Price) / 100 * seconds / (30 * 86400))
	amount := domain.Money(int64(rubles) * 100)
	if amount < s.cfg.Current().MinPaymentAmount {
		amount = s.cfg.Current().MinPaymentAmount
	}
	return domain.DeviceQuote{
		Pack:       pack,
		Amount:     amount,
		ExpiresAt:  expiresAt.UTC(),
		BilledDays: billedDays,
	}, nil
}

// DeviceSlots — перечень начислений субъекта для экрана.
func (s *Service) DeviceSlots(ctx context.Context, subjectID int64, subscriptionExpiresAt *time.Time) ([]domain.DeviceSlotView, error) {
	grants, err := s.db.ListDeviceGrants(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return nil, nil
	}
	packs, err := s.db.DevicePacks(ctx)
	if err != nil {
		return nil, err
	}
	packByID := make(map[string]domain.DevicePack, len(packs))
	for _, pack := range packs {
		packByID[pack.ID] = pack
	}
	plans, err := s.Plans(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	views := make([]domain.DeviceSlotView, 0, len(grants))
	for _, grant := range grants {
		view := domain.DeviceSlotView{
			OrderID:    grant.OrderID,
			Devices:    grant.Devices,
			ExpiresAt:  grant.ExpiresAt,
			IsPlanSlot: grant.PlanID != nil,
		}
		packID := deref(grant.PackID)
		pack, hasPack := packByID[packID]
		switch {
		case grant.PlanID != nil:
			title := deref(grant.PlanID)
			if plan, ok := plans[title]; ok && plan.Title != "" {
				title = plan.Title
			}
			view.Title = "Тариф «" + title + "»"
			view.PackID = packID
		case hasPack:
			view.Title = pack.Title
			view.PackID = pack.ID
		default:
			view.Title = fmt.Sprintf("+%d устройств", grant.Devices)
			view.PackID = packID
		}
		view.Price = pack.Price
		view.Renewable = grant.ExpiresAt != nil && hasPack && pack.IsActive
		if view.Renewable && subscriptionExpiresAt != nil {
			if !grant.ExpiresAt.Before(*subscriptionExpiresAt) {
				view.Renewable = false
			} else if quote, err := s.quoteDevicePack(pack, *subscriptionExpiresAt, grant.ExpiresAt, now); err == nil {
				view.Price = quote.Amount
			}
		}
		views = append(views, view)
	}
	return views, nil
}

// CheckoutDevices создаёт счёт на пакет устройств или на продление слота.
func (s *Service) CheckoutDevices(ctx context.Context, purchase DevicePurchase) (domain.Checkout, error) {
	if !s.cfg.Current().DeviceLimitEnabled {
		return domain.Checkout{}, ErrDevicesDisabled
	}
	subject, err := s.subject(ctx, purchase.TelegramID, purchase.AccountID)
	if err != nil {
		return domain.Checkout{}, err
	}

	var grant *storage.DeviceGrant
	if purchase.GrantOrderID != "" {
		grant, err = s.db.DeviceGrantByOrder(ctx, purchase.GrantOrderID)
		if err != nil {
			return domain.Checkout{}, err
		}
		if grant == nil || grant.ExpiresAt == nil {
			return domain.Checkout{}, ErrGrantNotRenewable
		}
		if grant.SubjectID != subject.ID {
			return domain.Checkout{}, ErrGrantNotOwned
		}
	}

	// Пакет для продления берём из начисления: кнопка «продлить» знает
	// только начисление, а не каталог.
	packID := purchase.PackID
	if packID == "" && grant != nil {
		packID = deref(grant.PackID)
	}
	if packID == "" {
		return domain.Checkout{}, ErrUnknownDevicePack
	}
	pack, err := s.db.DevicePack(ctx, packID)
	if err != nil {
		return domain.Checkout{}, ErrUnknownDevicePack
	}
	if !pack.IsActive {
		return domain.Checkout{}, ErrPackUnavailable
	}
	if grant != nil && deref(grant.PackID) != pack.ID {
		return domain.Checkout{}, ErrGrantNotRenewable
	}

	if !s.registry.Enabled() {
		return domain.Checkout{}, ErrNoProviders
	}
	user, err := s.panelUser(ctx, subject.TelegramID, fallbackUsername(subject))
	if err != nil {
		return domain.Checkout{}, fmt.Errorf("%w: %v", ErrNoSubscription, err)
	}
	subscriptionExpiresAt := user.ExpireAt.Time
	now := time.Now().UTC()
	if subscriptionExpiresAt.IsZero() || subscriptionExpiresAt.Sub(now) < devicePurchaseMinRemaining {
		return domain.Checkout{}, ErrDeviceTermTooShort
	}

	var coveredUntil *time.Time
	if grant != nil {
		coveredUntil = grant.ExpiresAt
	}
	quote, err := s.quoteDevicePack(pack, subscriptionExpiresAt, coveredUntil, now)
	if err != nil {
		return domain.Checkout{}, err
	}

	provider, providerName, method, err := s.pickProvider(purchase.Option, purchase.Method, purchase.Web)
	if err != nil {
		return domain.Checkout{}, err
	}

	unlock := s.lockFor(subject.ID)
	defer unlock()
	if existing := s.findLiveDeviceOrder(ctx, subject, pack.ID, purchase.GrantOrderID, providerName, method, quote); existing != nil {
		return domain.Checkout{
			URL:        deref(existing.PaymentURL),
			OrderID:    existing.OrderID,
			Amount:     existing.Amount,
			ExpiresAt:  existing.PaymentExpiresAt,
			DeviceFrom: existing.DeviceExpiresAt,
			Reused:     true,
		}, nil
	}

	orderID := s.newOrderID(purchase.TelegramID, purchase.AccountID)
	ttlMinutes := s.cfg.Current().LinkTTLMinutes
	if remaining := int(quote.ExpiresAt.Sub(now).Minutes()); remaining < ttlMinutes {
		ttlMinutes = remaining
	}
	if ttlMinutes < 1 {
		ttlMinutes = 1
	}
	successURL, failURL := s.returnURLs()
	request := CreateRequest{
		OrderID:     orderID,
		Amount:      quote.Amount,
		Currency:    s.cfg.Current().Currency,
		Description: "Дополнительные устройства, " + pack.Title,
		ExpiresIn:   time.Duration(ttlMinutes) * time.Minute,
		SuccessURL:  successURL,
		FailURL:     failURL,
		Method:      method,
	}
	expiry := now.Add(time.Duration(ttlMinutes) * time.Minute)
	expires := &expiry
	devices := pack.Devices
	if grant != nil {
		devices = grant.Devices
	}
	order := domain.Order{
		OrderID:          orderID,
		Provider:         providerName,
		TelegramID:       subject.TelegramID,
		AccountID:        accountIDPtr(subject, Purchase{AccountID: purchase.AccountID}),
		Username:         ptrOrNil(purchase.Username),
		PlanID:           pack.ID,
		Kind:             domain.KindDevices,
		Amount:           quote.Amount,
		OriginalAmount:   quote.Amount,
		Currency:         s.cfg.Current().Currency,
		PaymentMethod:    ptrOrNil(method),
		Status:           domain.OrderPending,
		PaymentExpiresAt: expires,
		DeviceCount:      intPtr(devices),
		DeviceDays:       intPtr(quote.BilledDays),
		DeviceExpiresAt:  &quote.ExpiresAt,
		RenewGrantID:     ptrOrNil(purchase.GrantOrderID),
	}
	link, err := s.createPaymentOrder(ctx, provider, order, request)
	if err != nil {
		return domain.Checkout{}, err
	}
	expires = link.ExpiresAt
	from := now
	if coveredUntil != nil && coveredUntil.After(from) {
		from = *coveredUntil
	}
	return domain.Checkout{
		URL:        link.URL,
		OrderID:    orderID,
		Amount:     quote.Amount,
		ExpiresAt:  expires,
		Stars:      link.Stars,
		DeviceFrom: &from,
	}, nil
}

// findLiveDeviceOrder ищет тот же счёт, чтобы повторное нажатие продолжало
// оплату, а не открывало вторую.
func (s *Service) findLiveDeviceOrder(
	ctx context.Context, subject domain.Subject, packID, grantOrderID, providerName, method string, quote domain.DeviceQuote,
) *domain.Order {
	orders, err := s.db.LivePendingOrders(ctx, subject.TelegramID,
		accountIDPtr(subject, Purchase{}), domain.KindDevices)
	if err != nil {
		return nil
	}
	for i := range orders {
		order := orders[i]
		if order.PlanID != packID || order.Provider != providerName || order.Amount != quote.Amount {
			continue
		}
		if deref(order.RenewGrantID) != grantOrderID || deref(order.PaymentMethod) != method {
			continue
		}
		if order.DeviceExpiresAt == nil || !order.DeviceExpiresAt.Equal(quote.ExpiresAt) {
			continue
		}
		return &order
	}
	return nil
}

// provisionDevices начисляет купленные слоты и поднимает лимит в панели.
func (s *Service) provisionDevices(ctx context.Context, order domain.Order) (domain.ProvisionResult, error) {
	devices := 0
	if order.DeviceCount != nil {
		devices = *order.DeviceCount
	}
	if devices <= 0 {
		return domain.ProvisionResult{}, errors.New("в заказе нет числа устройств")
	}
	subject, err := s.subject(ctx, order.TelegramID, order.AccountID)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	var expiresAt *time.Time
	if order.DeviceExpiresAt != nil {
		expiresAt = order.DeviceExpiresAt
	} else if order.DeviceDays != nil && *order.DeviceDays > 0 {
		until := time.Now().UTC().AddDate(0, 0, *order.DeviceDays)
		expiresAt = &until
	}

	granted, err := s.db.GrantDeviceSlots(ctx, order.OrderID, subject.ID, devices,
		&order.PlanID, nil, expiresAt)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	// Перечитываем начисление: на повторном вебхуке срок нельзя считать
	// заново от текущего момента — он должен совпасть с сохранённым.
	deviceExpiresAt := expiresAt
	if grant, err := s.db.DeviceGrantByOrder(ctx, order.OrderID); err == nil && grant != nil {
		deviceExpiresAt = grant.ExpiresAt
	}

	result := domain.ProvisionResult{DeviceExpiresAt: deviceExpiresAt}
	if granted {
		result.DevicesAdded = devices
	}
	if limit, err := s.syncLimitForOrder(ctx, subject, order); err == nil && limit != nil {
		result.DeviceLimit = limit
	}
	if err := s.db.MarkProvisioned(ctx, order.OrderID, nil, nil); err != nil {
		return result, err
	}
	return result, nil
}

// provisionDeviceRenewal сдвигает срок существующего начисления.
func (s *Service) provisionDeviceRenewal(ctx context.Context, order domain.Order) (domain.ProvisionResult, error) {
	grantOrderID := deref(order.RenewGrantID)
	if grantOrderID == "" {
		return domain.ProvisionResult{}, errors.New("в заказе нет продлеваемого начисления")
	}
	subject, err := s.subject(ctx, order.TelegramID, order.AccountID)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	days := 0
	if order.DeviceDays != nil {
		days = *order.DeviceDays
	}
	if days < 1 {
		days = 1
	}

	grant, err := s.db.DeviceGrantByOrder(ctx, grantOrderID)
	if err != nil {
		return domain.ProvisionResult{}, err
	}
	if grant == nil {
		return domain.ProvisionResult{}, ErrGrantNotRenewable
	}
	target := order.DeviceExpiresAt
	if target == nil {
		base := time.Now().UTC()
		if grant.ExpiresAt != nil && grant.ExpiresAt.After(base) {
			base = *grant.ExpiresAt
		}
		until := base.AddDate(0, 0, days)
		target = &until
	}
	if _, err := s.db.RenewDeviceUntil(ctx, order.OrderID, grantOrderID, *target, days); err != nil {
		return domain.ProvisionResult{}, err
	}

	result := domain.ProvisionResult{DeviceRenewedUntil: target}
	if renewed, err := s.db.DeviceGrantByOrder(ctx, grantOrderID); err == nil && renewed != nil {
		result.DeviceRenewedUntil = renewed.ExpiresAt
	}
	if limit, err := s.syncLimitForOrder(ctx, subject, order); err == nil && limit != nil {
		result.DeviceLimit = limit
	}
	if err := s.db.MarkProvisioned(ctx, order.OrderID, nil, nil); err != nil {
		return result, err
	}
	return result, nil
}

// syncLimitForOrder выставляет лимит найденному пользователю панели. nil —
// пользователя ещё нет; слоты копятся, лимит применится при первой покупке
// подписки, и терять из-за этого оплаченное нельзя.
func (s *Service) syncLimitForOrder(ctx context.Context, subject domain.Subject, order domain.Order) (*int, error) {
	if !s.cfg.Current().DeviceLimitEnabled {
		return nil, nil
	}
	username := deref(order.Username)
	if username == "" || subject.RemnawaveUser != nil {
		username = fallbackUsername(subject)
	}
	user, err := s.panelUser(ctx, order.TelegramID, username)
	if err != nil {
		return nil, err
	}
	limit, err := s.SyncDeviceLimit(ctx, subject.ID, *user, false)
	if err != nil {
		return nil, err
	}
	return &limit, nil
}

// ManagedDeviceLimit — сколько устройств мы назначаем: база плюс докупленное.
// Это не то же самое, что действующий лимит: администратор мог поднять его
// вручную прямо в панели.
func (s *Service) ManagedDeviceLimit(ctx context.Context, subjectID int64) (int, error) {
	if !s.cfg.Current().DeviceLimitEnabled {
		return 0, nil
	}
	return s.db.SubjectDeviceLimit(ctx, subjectID, s.cfg.Current().DeviceLimitBase)
}

// SyncDeviceLimit выставляет в панели расчётный лимит и возвращает
// действующий. Ошибку панели не глотаем: вызывающий сам решает, срывать ли
// из-за неё выдачу заказа (не срывать — подписка уже оплачена) или повторять
// позже.
//
// allowLower=false (умолчание, выдача заказа) — только поднимает. Человеку
// могли выдать больше вручную, и отнимать это мы не вправе.
// allowLower=true (сметатель) — понижает лишь значение, которое бот сам
// последний раз записал: ручную правку даже внутри диапазона не трогаем.
func (s *Service) SyncDeviceLimit(ctx context.Context, subjectID int64, user remnawave.User, allowLower bool) (int, error) {
	if !s.cfg.Current().DeviceLimitEnabled || user.ID == 0 {
		return 0, nil
	}
	managed, err := s.ManagedDeviceLimit(ctx, subjectID)
	if err != nil {
		return 0, err
	}
	current := panelDeviceLimit(user)
	if current >= managed {
		if !allowLower {
			return current, nil
		}
		if current == managed {
			return current, nil
		}
		panelUserID, lastLimit, ok, err := s.db.DeviceLimitSyncState(ctx, subjectID)
		if err != nil {
			return current, err
		}
		if !ok || panelUserID != strconv.FormatInt(user.ID, 10) || lastLimit != current {
			return current, nil
		}
	}
	limit := managed
	limitPtr := &limit
	if _, err := s.panel.UpdateUser(ctx, remnawave.UpdateUserParams{
		UserID:          user.ID,
		HWIDDeviceLimit: &limitPtr,
	}); err != nil {
		return current, err
	}
	if err := s.db.SaveDeviceLimitSyncState(ctx, subjectID, strconv.FormatInt(user.ID, 10), managed); err != nil {
		return managed, err
	}
	return managed, nil
}

// SyncDeviceLimitForSubject находит пользователя панели по субъекту и
// выставляет ему лимит. nil — пользователя там нет.
func (s *Service) SyncDeviceLimitForSubject(ctx context.Context, subjectID int64) (*int, error) {
	if !s.cfg.Current().DeviceLimitEnabled {
		return nil, nil
	}
	subject, err := s.db.SubjectByID(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	username := fallbackUsername(subject)
	user, err := s.panelUser(ctx, subject.TelegramID, username)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	limit, err := s.SyncDeviceLimit(ctx, subjectID, *user, true)
	if err != nil {
		return nil, err
	}
	return &limit, nil
}

// panelDeviceLimit — значение, которое панель реально применяет. Ноль или
// отсутствие значения означает «панель возьмёт своё умолчание», и назвать
// число мы не можем.
func panelDeviceLimit(user remnawave.User) int {
	if user.HWIDDeviceLimit == nil || *user.HWIDDeviceLimit <= 0 {
		return 0
	}
	return *user.HWIDDeviceLimit
}

// lookupUsername выбирает имя пользователя панели для поиска.
func (s *Service) lookupUsername(explicit string, telegramID, accountID *int64) string {
	if telegramID != nil {
		return ""
	}
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if accountID != nil {
		return fmt.Sprintf("web_%d", *accountID)
	}
	return ""
}
