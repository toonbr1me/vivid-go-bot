package payments

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

// fakePanel — поддельная панель: считает, сколько раз продлили подписку.
type fakePanel struct {
	mu         sync.Mutex
	users      map[int64]remnawave.User
	created    int
	expireSet  []time.Time
	resetCalls int
}

func newFakePanel() *fakePanel {
	return &fakePanel{users: map[int64]remnawave.User{}}
}

func (p *fakePanel) UsersByTelegramID(_ context.Context, telegramID int64) ([]remnawave.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	user, ok := p.users[telegramID]
	if !ok {
		return nil, nil
	}
	return []remnawave.User{user}, nil
}

func (p *fakePanel) UserByUsername(_ context.Context, username string) (*remnawave.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, user := range p.users {
		if user.Username == username {
			copied := user
			return &copied, nil
		}
	}
	return nil, nil
}

func (p *fakePanel) CreateUser(_ context.Context, params remnawave.CreateUserParams) (remnawave.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.created++
	user := remnawave.User{
		ID:       int64(p.created),
		Username: params.Username,
		ExpireAt: remnawave.Timestamp{Time: params.ExpireAt},
	}
	if params.TelegramID != nil {
		p.users[*params.TelegramID] = user
	} else {
		p.users[-user.ID] = user
	}
	p.expireSet = append(p.expireSet, params.ExpireAt)
	return user, nil
}

func (p *fakePanel) UpdateUser(_ context.Context, params remnawave.UpdateUserParams) (remnawave.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for telegramID, user := range p.users {
		if user.ID != params.UserID {
			continue
		}
		if params.ExpireAt != nil {
			user.ExpireAt = remnawave.Timestamp{Time: *params.ExpireAt}
			p.expireSet = append(p.expireSet, *params.ExpireAt)
		}
		if params.TrafficLimitBytes != nil {
			user.TrafficLimitBytes = *params.TrafficLimitBytes
		}
		if params.TelegramID != nil {
			user.TelegramID = params.TelegramID
		}
		if params.InternalSquads != nil {
			user.ActiveInternalSquads = *params.InternalSquads
		}
		p.users[telegramID] = user
		return user, nil
	}
	return remnawave.User{}, nil
}

func (p *fakePanel) ResetTraffic(_ context.Context, userID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetCalls++
	for key, user := range p.users {
		if user.ID == userID {
			user.TrafficUsedBytes = 0
			p.users[key] = user
		}
	}
	return nil
}

func (p *fakePanel) UpdateExpireAt(_ context.Context, userID int64, expireAt time.Time, _ []string) (remnawave.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for telegramID, user := range p.users {
		if user.ID != userID {
			continue
		}
		user.ExpireAt = remnawave.Timestamp{Time: expireAt}
		p.users[telegramID] = user
		p.expireSet = append(p.expireSet, expireAt)
		return user, nil
	}
	return remnawave.User{}, nil
}

func (p *fakePanel) HWIDDevices(context.Context, int64) ([]remnawave.HWIDDevice, error) {
	return nil, nil
}

func (p *fakePanel) DeleteHWIDDevice(context.Context, int64, string) (bool, error) {
	return true, nil
}

// expireUpdates — сколько раз панели выставляли срок.
func (p *fakePanel) expireUpdates() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.expireSet)
}

// lastExpiry — последний выставленный срок.
func (p *fakePanel) lastExpiry() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.expireSet) == 0 {
		return time.Time{}
	}
	return p.expireSet[len(p.expireSet)-1]
}

// testProvisionService — служба с поддельной панелью и каталогом.
func testProvisionService(t *testing.T) (*Service, *storage.DB, *fakePanel) {
	t.Helper()
	db := testdb.Open(t)
	panel := newFakePanel()
	cfg := &config.Settings{
		Currency:          "RUB",
		MinPaymentAmount:  domain.FromRubles(1),
		ReferralMode:      "days",
		RefBalancePercent: 10,
		DeviceLimitBase:   2,
	}
	svc := New(db, panel, &Registry{providers: map[string]Provider{}}, cfg)
	return svc, db, panel
}

// paidOrder — заказ, оплаченный и готовый к выдаче.
func paidOrder(t *testing.T, db *storage.DB, telegramID int64, amount domain.Money) domain.Order {
	t.Helper()
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, telegramID)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	created, err := db.CreateOrder(ctx, domain.Order{
		OrderID:    "tg-" + itoa(telegramID) + "-test",
		Provider:   "wata",
		TelegramID: &telegramID,
		AccountID:  &subject.ID,
		Username:   ptr("tg_" + itoa(telegramID)),
		PlanID:     "30d",
		Kind:       domain.KindSubscription,
		Amount:     amount,
		Currency:   "RUB",
		Status:     domain.OrderPaid,
		PlanDays:   ptr(30),
	})
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	return created
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func TestProvisionExtendsSubscriptionOnce(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()

	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	order := paidOrder(t, db, 1001, domain.FromRubles(100))

	if _, err := svc.Provision(ctx, order); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	first := panel.expireUpdates()
	if first == 0 {
		t.Fatal("панель не получила срок подписки")
	}
	firstExpiry := panel.lastExpiry()
	updated, err := db.OrderByID(ctx, order.OrderID)
	if err != nil {
		t.Fatalf("заказ: %v", err)
	}
	if updated.Status != domain.OrderProvisioned {
		t.Errorf("статус = %s, ждали %s", updated.Status, domain.OrderProvisioned)
	}
	if updated.ExpireAt == nil {
		t.Fatal("срок подписки не записан")
	}

	// Повторная выдача (повторный вебхук) не должна продлевать снова:
	// срок в панели должен остаться тем же.
	if _, err := svc.Provision(ctx, updated); err != nil {
		t.Fatalf("повторная выдача: %v", err)
	}
	if last := panel.lastExpiry(); !last.Equal(firstExpiry) {
		t.Errorf("после повтора срок = %v, до повтора был %v — подписка продлена второй раз",
			last, firstExpiry)
	}
}

func TestProvisionCreditsReferralDays(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()

	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	referrer, err := db.EnsureTelegramSubject(ctx, 2002)
	if err != nil {
		t.Fatalf("пригласивший: %v", err)
	}
	buyer, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("покупатель: %v", err)
	}
	if err := db.CreateReferral(ctx, referrer.ID, buyer.ID); err != nil {
		t.Fatalf("реферал: %v", err)
	}
	order := paidOrder(t, db, 1001, domain.FromRubles(100))

	result, err := svc.Provision(ctx, order)
	if err != nil {
		t.Fatalf("выдача: %v", err)
	}
	if !result.ReferralBonusGiven {
		t.Error("реферальная награда не начислена")
	}
	if result.BonusDays != 7 {
		t.Errorf("награда = %d дней, ждали 7", result.BonusDays)
	}
	_ = panel
}

func TestProvisionCreditsPartnerReward(t *testing.T) {
	svc, db, panel := testProvisionService(t)
	ctx := context.Background()

	if err := db.SavePlan(ctx, domain.Plan{
		ID: "30d", Title: "Месяц", Days: 30, Price: domain.FromRubles(100), IsActive: true,
	}); err != nil {
		t.Fatalf("тариф: %v", err)
	}
	partner, err := db.CreatePartner(ctx, "Партнёр", "@partner", "", "")
	if err != nil {
		t.Fatalf("партнёр: %v", err)
	}
	buyer, err := db.EnsureTelegramSubject(ctx, 1001)
	if err != nil {
		t.Fatalf("покупатель: %v", err)
	}
	order := paidOrder(t, db, 1001, domain.FromRubles(100))
	order.PartnerID = &partner.ID
	order.RewardType = ptr(domain.RewardTypePercent)
	order.RewardValue = domain.FromRubles(10)
	order.RewardScope = ptr(domain.RewardScopeAll)
	if err := db.SetOrderReward(ctx, order.OrderID, partner.ID, domain.RewardTypePercent,
		domain.FromRubles(10), domain.RewardScopeAll); err != nil {
		t.Fatalf("сохранение награды: %v", err)
	}
	_ = buyer

	if _, err := svc.Provision(ctx, order); err != nil {
		t.Fatalf("выдача: %v", err)
	}
	rewards, err := db.PartnerRewards(ctx, partner.ID, 10)
	if err != nil {
		t.Fatalf("начисления: %v", err)
	}
	if len(rewards) != 1 {
		t.Fatalf("начислений %d, ждали 1", len(rewards))
	}
	want := domain.FromRubles(10)
	if rewards[0].Amount != want {
		t.Errorf("начисление = %s, ждали %s", rewards[0].Amount, want)
	}

	// Повторная выдача не должна задвоить начисление.
	if _, err := svc.Provision(ctx, order); err != nil {
		t.Fatalf("повторная выдача: %v", err)
	}
	rewards, err = db.PartnerRewards(ctx, partner.ID, 10)
	if err != nil {
		t.Fatalf("начисления: %v", err)
	}
	if len(rewards) != 1 {
		t.Errorf("после повтора начислений %d, ждали 1", len(rewards))
	}
	_ = panel
}
