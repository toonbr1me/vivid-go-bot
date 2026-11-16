// Package jobs — фоновые задачи: выдача оплаченного, напоминания и
// пересчёт лимита устройств. Они не держат деньги в руках, а лишь доводят
// до конца то, что уже записано в базе.
package jobs

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/support"
)

// Sender доставляет уведомление человеку.
type Sender interface {
	SendText(ctx context.Context, telegramID int64, text string) error
}

// Run запускает все задачи и блокируется до отмены контекста.
func Run(ctx context.Context, db *storage.DB, pay *payments.Service, sender Sender, cfg *config.Settings) error {
	return RunWithAssistant(ctx, db, pay, sender, cfg, nil)
}

// RunWithAssistant запускает задачи вместе с помощником поддержки.
// Помощника может не быть: тогда он просто не отвечает.
func RunWithAssistant(ctx context.Context, db *storage.DB, pay *payments.Service, sender Sender, cfg *config.Settings, assistant *support.Assistant) error {
	group, ctx := errgroup.WithContext(ctx)
	if worker, ok := sender.(interface {
		ProcessBroadcasts(context.Context, int) error
	}); ok {
		group.Go(func() error {
			return every(ctx, "рассылки", 2*time.Second, func(ctx context.Context) error { return worker.ProcessBroadcasts(ctx, 20) })
		})
	}
	if worker, ok := sender.(interface {
		ProcessPanelEvents(context.Context, int) error
	}); ok {
		group.Go(func() error {
			return every(ctx, "события панели", 5*time.Second, func(ctx context.Context) error { return worker.ProcessPanelEvents(ctx, 20) })
		})
	}

	group.Go(func() error {
		return every(ctx, "выдача заказов", time.Minute, func(ctx context.Context) error {
			if err := pay.ReconcilePayments(ctx, 25); err != nil {
				return err
			}
			if err := pay.ProcessPaymentInbox(ctx, 50); err != nil {
				return err
			}
			done, err := pay.ProvisionPending(ctx, 25)
			if recurringErr := pay.ProcessRecurring(ctx, 25); recurringErr != nil {
				slog.Warn("автопродление", "err", recurringErr)
			}
			if done > 0 {
				slog.Info("выдача заказов завершена", "выдано", done)
			}
			return err
		})
	})

	group.Go(func() error {
		return every(ctx, "напоминания о сроке", 15*time.Minute, func(ctx context.Context) error {
			return remindExpiry(ctx, db, sender, cfg)
		})
	})

	group.Go(func() error {
		return every(ctx, "напоминания о счёте", 30*time.Minute, func(ctx context.Context) error {
			return remindUnpaid(ctx, db, sender, cfg)
		})
	})

	group.Go(func() error {
		return every(ctx, "лимит устройств", 10*time.Minute, func(ctx context.Context) error {
			return sweepDevices(ctx, db, pay, cfg)
		})
	})

	if assistant != nil && assistant.Enabled() {
		group.Go(func() error {
			return every(ctx, "помощник поддержки", 20*time.Second, func(ctx context.Context) error {
				done, err := assistant.Handle(ctx)
				if err != nil {
					return err
				}
				if done > 0 {
					slog.Info("помощник ответил", "обращений", done)
				}
				return nil
			})
		})
		group.Go(func() error {
			return every(ctx, "передача забытых обращений", time.Hour, func(ctx context.Context) error {
				closed, err := assistant.CloseIdle(ctx)
				if err != nil {
					return err
				}
				if closed > 0 {
					slog.Info("обращения переданы сотрудникам", "обращений", closed)
				}
				return nil
			})
		})
	}

	return group.Wait()
}

// every зовёт задачу сразу, а затем с заданным интервалом. Ошибка одной
// итерации не останавливает цикл: база и панель переживают короткие сбои.
func every(ctx context.Context, name string, interval time.Duration, task func(context.Context) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := task(ctx); err != nil && ctx.Err() == nil {
			slog.Error("фоновая задача", "задача", name, "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// remindExpiry предупреждает за сутки до конца подписки.
func remindExpiry(ctx context.Context, db *storage.DB, sender Sender, configs ...*config.Settings) error {
	now := time.Now().UTC()
	orders, err := db.ExpiringSubscriptions(ctx, now, now.Add(24*time.Hour))
	if err != nil {
		return err
	}
	for _, order := range orders {
		if order.TelegramID == nil || order.ExpireAt == nil {
			continue
		}
		settings, err := db.NotificationSettingsFor(ctx, *order.TelegramID)
		if err != nil || !settings.Expiry {
			continue
		}
		subject, err := db.SubjectByTelegramID(ctx, *order.TelegramID)
		if err != nil {
			return err
		}
		var recurring *domain.RecurringAgreement
		if agreement, err := db.LatestRecurringForSubject(ctx, subject.ID); err == nil {
			recurring = &agreement
		} else if err != storage.ErrNotFound {
			return err
		}
		claim, err := db.ClaimReminder(ctx, *order.TelegramID, "expiry", order.ExpireAt.UTC())
		if err != nil || claim == "" {
			continue
		}
		location := time.Local
		if len(configs) > 0 && configs[0] != nil && configs[0].Timezone != "" {
			if configured, err := time.LoadLocation(configs[0].Timezone); err == nil {
				location = configured
			}
		}
		text := fmt.Sprintf(
			"⏳ <b>Подписка заканчивается</b>\n\nДоступ действует до <b>%s</b>.\n"+
				"Продлите заранее, чтобы не перенастраивать подключение.",
			order.ExpireAt.In(location).Format("02.01.2006"))
		if recurring != nil && recurring.Enabled {
			text = "🔁 <b>Автопродление</b>\n\n" + html.EscapeString(recurring.StatusText())
			if recurring.Activated && recurring.LastError == "" && recurring.NextChargeAt != nil {
				text += fmt.Sprintf("\nСледующее списание: %s — %s %s каждые %d дней.", recurring.NextChargeAt.In(location).Format("02.01.2006 15:04"), recurring.Amount.String(), html.EscapeString(recurring.Currency), recurring.Days)
			}
			text += "\nУправление и отключение доступны в аккаунте. Если платёж уже отправлен, повторно платить не нужно."
		} else if recurring != nil && recurring.StopReason != "user" {
			text += "\n\n" + html.EscapeString(recurring.StatusText())
		}
		sendErr := sender.SendText(ctx, *order.TelegramID, text)
		if err := db.FinishReminder(ctx, claim, sendErr == nil); err != nil {
			return err
		}
		if err := sendErr; err != nil {
			slog.Warn("напоминание не доставлено", "telegram_id", *order.TelegramID, "err", err)
		}
	}
	return nil
}

// remindUnpaid напоминает о счёте, который так и не оплатили. Выключается
// нулевой настройкой REMIND_UNPAID_INVOICE_HOURS.
func remindUnpaid(ctx context.Context, db *storage.DB, sender Sender, cfg *config.Settings) error {
	if cfg.RemindUnpaidHours <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(cfg.RemindUnpaidHours) * time.Hour)
	orders, err := db.PendingOrdersForReminder(ctx, cutoff, 50)
	if err != nil {
		return err
	}
	for _, order := range orders {
		if order.TelegramID == nil {
			continue
		}
		settings, err := db.NotificationSettingsFor(ctx, *order.TelegramID)
		if err != nil || !settings.Activity {
			continue
		}
		claim, err := db.ClaimReminder(ctx, *order.TelegramID, "unpaid", order.CreatedAt.UTC())
		if err != nil || claim == "" {
			continue
		}
		text := "💳 <b>Счёт ждёт оплаты</b>\n\nЗаказ <code>" + html.EscapeString(order.OrderID) + "</code>."
		if order.PaymentURL != nil {
			if link, err := url.Parse(*order.PaymentURL); err == nil && (link.Scheme == "https" || link.Scheme == "http") && link.Host != "" {
				text += "\n<a href=\"" + html.EscapeString(link.String()) + "\">Продолжить оплату этого счёта</a>"
			}
		}
		text += "\nЕсли уже оплатили, проверьте этот заказ в истории покупок."
		sendErr := sender.SendText(ctx, *order.TelegramID, text)
		if err := db.FinishReminder(ctx, claim, sendErr == nil); err != nil {
			return err
		}
		if err := sendErr; err != nil {
			slog.Warn("напоминание о счёте не доставлено", "telegram_id", *order.TelegramID, "err", err)
		}
	}
	return nil
}

// sweepDevices понижает лимит устройств после истечения докупленных слотов.
// Поднимать лимит имеет право только выдача заказа; опускать — лишь здесь,
// и только если панельное значение совпадает с тем, что бот записал сам.
func sweepDevices(ctx context.Context, db *storage.DB, pay *payments.Service, cfg *config.Settings) error {
	if !cfg.DeviceLimitEnabled {
		return nil
	}
	grants, err := db.ExpiredGrants(ctx, 100)
	if err != nil {
		return err
	}
	if len(grants) == 0 {
		return nil
	}
	bySubject := map[int64][]string{}
	for _, grant := range grants {
		bySubject[grant.SubjectID] = append(bySubject[grant.SubjectID], grant.OrderID)
	}
	for subjectID, orderIDs := range bySubject {
		if _, err := pay.SyncDeviceLimitForSubject(ctx, subjectID); err != nil {
			slog.Warn("не удалось пересчитать лимит устройств",
				"subject_id", subjectID, "err", err)
			continue
		}
		if err := db.MarkGrantExpiryApplied(ctx, orderIDs); err != nil {
			return err
		}
	}
	return nil
}
