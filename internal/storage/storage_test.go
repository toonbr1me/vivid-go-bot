package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestMigrateSubjectsFillsMissingLinks(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	// Строки, как их оставила прошлая версия: telegram_id есть, subject_id нет.
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO trials (telegram_id, kind) VALUES (555, 'bot')`); err != nil {
		t.Fatalf("триал: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO user_balance (telegram_id, balance) VALUES (555, 25.00)`); err != nil {
		t.Fatalf("баланс: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO promo_activations (telegram_id, code) VALUES (555, 'OLD')`); err != nil {
		t.Fatalf("активация промокода: %v", err)
	}

	if err := db.MigrateSubjects(ctx); err != nil {
		t.Fatalf("перенос субъектов: %v", err)
	}

	var trials int
	if err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM trials WHERE subject_id IS NOT NULL`).Scan(&trials); err != nil {
		t.Fatalf("проверка триалов: %v", err)
	}
	if trials != 1 {
		t.Errorf("триалов с субъектом %d, ждали 1", trials)
	}
	var balances int
	if err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM user_balance WHERE subject_id IS NOT NULL`).Scan(&balances); err != nil {
		t.Fatalf("проверка балансов: %v", err)
	}
	if balances != 1 {
		t.Errorf("балансов с субъектом %d, ждали 1", balances)
	}
	subject, err := db.SubjectByTelegramID(ctx, 555)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if subject.ID == 0 {
		t.Fatal("субъект не заведён")
	}
	if subject.RefCode == nil || *subject.RefCode == "" {
		t.Error("у субъекта нет реферального кода")
	}
}

func TestMigrateSubjectsIsIdempotent(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO trials (telegram_id, kind) VALUES (556, 'bot')`); err != nil {
		t.Fatalf("триал: %v", err)
	}
	if err := db.MigrateSubjects(ctx); err != nil {
		t.Fatalf("первый перенос: %v", err)
	}
	if err := db.MigrateSubjects(ctx); err != nil {
		t.Fatalf("повторный перенос: %v", err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM trials`).Scan(&count); err != nil {
		t.Fatalf("проверка: %v", err)
	}
	if count != 1 {
		t.Errorf("строк %d, ждали 1: повторный перенос что-то продублировал", count)
	}
}

func TestPartnerInviteLinksSubjectAndPaysRewards(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	partner, err := db.CreatePartner(ctx, "Партнёр", "@partner", "", "")
	if err != nil {
		t.Fatalf("партнёр: %v", err)
	}
	code, err := db.CreatePartnerInviteCode(ctx, partner.ID, time.Hour)
	if err != nil {
		t.Fatalf("приглашение: %v", err)
	}
	partnerID, err := db.ConsumePartnerInviteCode(ctx, code)
	if err != nil {
		t.Fatalf("применение приглашения: %v", err)
	}
	if partnerID == nil || *partnerID != partner.ID {
		t.Fatalf("приглашение привело к %v, ждали %d", partnerID, partner.ID)
	}
	// Код одноразовый.
	again, err := db.ConsumePartnerInviteCode(ctx, code)
	if err != nil {
		t.Fatalf("повтор приглашения: %v", err)
	}
	if again != nil {
		t.Error("приглашение не должно работать дважды")
	}

	subject, err := db.EnsureTelegramSubject(ctx, 7001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	// Начисление, накопленное до привязки.
	if _, err := db.CreditPartnerReward(ctx, partner.ID, "order-1", &subject.ID,
		domain.FromRubles(150), nil); err != nil {
		t.Fatalf("начисление: %v", err)
	}
	linked, err := db.LinkPartnerSubject(ctx, partner.ID, subject.ID)
	if err != nil {
		t.Fatalf("привязка: %v", err)
	}
	if linked == nil || linked.SubjectID == nil {
		t.Fatal("партнёр не привязан к аккаунту")
	}
	balance, err := db.Balance(ctx, subject.ID)
	if err != nil {
		t.Fatalf("баланс: %v", err)
	}
	if balance != domain.FromRubles(150) {
		t.Errorf("баланс = %s, ждали 150", balance)
	}
	rewards, err := db.PartnerRewards(ctx, partner.ID, 10)
	if err != nil {
		t.Fatalf("начисления: %v", err)
	}
	if len(rewards) != 1 || rewards[0].Status != domain.PartnerRewardCredited {
		t.Errorf("начисление не зачислено: %+v", rewards)
	}
}

func TestPartnerRewardIsNotDoubled(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	partner, err := db.CreatePartner(ctx, "Партнёр", "", "", "")
	if err != nil {
		t.Fatalf("партнёр: %v", err)
	}
	subject, err := db.EnsureTelegramSubject(ctx, 7002)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.CreditPartnerReward(ctx, partner.ID, "order-1", &subject.ID,
			domain.FromRubles(50), nil); err != nil {
			t.Fatalf("начисление: %v", err)
		}
	}
	rewards, err := db.PartnerRewards(ctx, partner.ID, 10)
	if err != nil {
		t.Fatalf("начисления: %v", err)
	}
	if len(rewards) != 1 {
		t.Errorf("начислений %d, ждали 1: повторный вебхук задвоил награду", len(rewards))
	}
}

// Сессия сайта ищется по хешу токена. В запросе участвуют две таблицы с
// одинаковым created_at, поэтому колонки должны быть указаны с таблицей:
// иначе Postgres отвечает «неоднозначно» и вход на сайт не работает.
func TestSubjectBySessionFindsOwner(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 9301)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	token := "сессия-токен"
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])
	if err := db.CreateWebSession(ctx, hashHex, subject.ID, time.Hour); err != nil {
		t.Fatalf("сессия: %v", err)
	}
	found, err := db.SubjectBySession(ctx, hashHex)
	if err != nil {
		t.Fatalf("поиск по сессии: %v", err)
	}
	if found.ID != subject.ID {
		t.Errorf("владелец сессии = %d, ждали %d", found.ID, subject.ID)
	}
	// Чужой токен сессию не находит.
	if _, err := db.SubjectBySession(ctx, "чужой"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("ошибка поиска = %v, ждали «не найдено»", err)
	}
}

func TestReferralModeRoundTrip(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 8001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	mode, err := db.ReferralMode(ctx, subject.ID)
	if err != nil {
		t.Fatalf("чтение режима: %v", err)
	}
	if mode != "" {
		t.Errorf("режим по умолчанию = %q, ждали пустой", mode)
	}
	if err := db.SetReferralMode(ctx, subject.ID, "balance"); err != nil {
		t.Fatalf("запись режима: %v", err)
	}
	mode, err = db.ReferralMode(ctx, subject.ID)
	if err != nil {
		t.Fatalf("повторное чтение: %v", err)
	}
	if mode != "balance" {
		t.Errorf("режим = %q, ждали balance", mode)
	}
}

func TestExtraDeviceSlotsTakeMaxOfPlansAndSumOfPacks(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 9001)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	planID := "30d"
	expires := time.Now().Add(30 * 24 * time.Hour)
	if _, err := db.GrantDeviceSlots(ctx, "order-plan-1", subject.ID, 5, nil, &planID, &expires); err != nil {
		t.Fatalf("слоты тарифа: %v", err)
	}
	if _, err := db.GrantDeviceSlots(ctx, "order-plan-2", subject.ID, 3, nil, &planID, &expires); err != nil {
		t.Fatalf("второй тариф: %v", err)
	}
	packID := "dev1"
	if _, err := db.GrantDeviceSlots(ctx, "order-pack-1", subject.ID, 2, &packID, nil, &expires); err != nil {
		t.Fatalf("пакет: %v", err)
	}
	extra, err := db.ExtraDeviceSlots(ctx, subject.ID)
	if err != nil {
		t.Fatalf("подсчёт слотов: %v", err)
	}
	// Тарифные слоты берутся по максимуму (5), пакетные складываются (+2).
	if extra != 7 {
		t.Errorf("лишних слотов %d, ждали 7", extra)
	}
}

func TestPromoPerUserLimitIsCounted(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 9101)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	if err := db.CreatePromo(ctx, domain.PromoCode{
		Code: "ONCE", DiscountPercent: 10, PerUserLimit: 1, IsActive: true,
		RewardScope: domain.RewardScopeAll,
	}); err != nil {
		t.Fatalf("промокод: %v", err)
	}
	if err := db.RecordPromoActivation(ctx, subject.ID, "ONCE"); err != nil {
		t.Fatalf("активация: %v", err)
	}
	uses, err := db.PromoActivationsForCode(ctx, subject.ID, "ONCE")
	if err != nil {
		t.Fatalf("подсчёт: %v", err)
	}
	if uses != 1 {
		t.Errorf("применений %d, ждали 1", uses)
	}
	distinct, err := db.DistinctPromoCount(ctx, subject.ID)
	if err != nil {
		t.Fatalf("разные коды: %v", err)
	}
	if distinct != 1 {
		t.Errorf("разных кодов %d, ждали 1", distinct)
	}
}

func TestTicketLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	subject, err := db.EnsureTelegramSubject(ctx, 9201)
	if err != nil {
		t.Fatalf("субъект: %v", err)
	}
	ticket, err := db.CreateTicket(ctx, subject.ID, ptr(int64(9201)), "Не приходит доступ", "Текст", storage.SupportModeHuman)
	if err != nil {
		t.Fatalf("обращение: %v", err)
	}
	open, err := db.OpenTicketForSubject(ctx, subject.ID)
	if err != nil {
		t.Fatalf("открытое обращение: %v", err)
	}
	if open == nil || open.ID != ticket.ID {
		t.Fatalf("открытое обращение = %v, ждали #%d", open, ticket.ID)
	}
	if _, err := db.AddTicketMessage(ctx, ticket.ID, subject.ID, ptr(int64(9201)),
		"Дополнение", false, "", ""); err != nil {
		t.Fatalf("сообщение: %v", err)
	}
	messages, err := db.TicketMessages(ctx, ticket.ID, 10)
	if err != nil {
		t.Fatalf("переписка: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("сообщений %d, ждали 2", len(messages))
	}
	closed, err := db.CloseTicket(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("закрытие: %v", err)
	}
	if !closed {
		t.Error("обращение не закрылось")
	}
	open, err = db.OpenTicketForSubject(ctx, subject.ID)
	if err != nil {
		t.Fatalf("повторная проверка: %v", err)
	}
	if open != nil {
		t.Error("после закрытия открытого обращения быть не должно")
	}
}

func ptr[T any](value T) *T { return &value }
