package jobs

import (
	"context"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"html"
	"log/slog"
	"time"
)

// connectedState returns unknown when the panel omitted the field entirely.
func connectedState(users []remnawave.User) (known, connected bool) {
	if len(users) == 0 {
		return false, false
	}
	allKnown := true
	for _, u := range users {
		source := u.Raw
		if nested, ok := u.Raw["userTraffic"].(map[string]any); ok {
			source = nested
		}
		value, ok := source["firstConnectedAt"]
		if !ok {
			allKnown = false
			continue
		}
		if value != nil && value != "" {
			return true, true
		}
	}
	return allKnown, false
}

func remindNotConnected(ctx context.Context, db *storage.DB, pay *payments.Service, sender Sender, cfg *config.Settings) error {
	cfg = cfg.Current()
	hours := cfg.RemindNotConnectedHours
	if hours <= 0 {
		hours = 6
	}
	candidates, err := db.NotConnectedCandidates(ctx, hours)
	if err != nil {
		return err
	}
	for _, subject := range candidates {
		var users []remnawave.User
		if pay.Panel() == nil {
			continue
		}
		if subject.RemnawaveUser != nil {
			u, e := pay.Panel().UserByUsername(ctx, *subject.RemnawaveUser)
			if e != nil {
				return e
			}
			if u != nil {
				users = append(users, *u)
			}
		} else if subject.TelegramID != nil {
			users, err = pay.Panel().UsersByTelegramID(ctx, *subject.TelegramID)
			if err != nil {
				return err
			}
		}
		known, connected := connectedState(users)
		if err = db.RecordConnectionCheck(ctx, subject.ID, known, connected); err != nil {
			return err
		}
		if !known || connected || cfg.RemindNotConnectedHours <= 0 || subject.TelegramID == nil {
			continue
		}
		active := false
		for _, u := range users {
			if u.IsActive() {
				active = true
			}
		}
		if !active {
			continue
		}
		prefs, err := db.NotificationSettingsFor(ctx, *subject.TelegramID)
		if err != nil {
			return err
		}
		if !prefs.Activity {
			continue
		}
		if err = sendEngagement(ctx, db, sender, *subject.TelegramID, "not_connected", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), "📱 <b>Поможем подключиться</b>\n\nДоступ уже готов. Откройте /subscription для подключения или /support, если нужна помощь."); err != nil {
			return err
		}
	}
	return nil
}
func sendEngagement(ctx context.Context, db *storage.DB, sender Sender, id int64, kind string, target time.Time, text string) error {
	claim, err := db.ClaimReminder(ctx, id, kind, target)
	if err != nil || claim == "" {
		return err
	}
	sendErr := sender.SendText(ctx, id, text)
	if err = db.FinishReminder(ctx, claim, sendErr == nil); err != nil {
		return err
	}
	if sendErr != nil {
		slog.Warn("напоминание не доставлено", "kind", kind)
	}
	return nil
}

func retentionReminders(ctx context.Context, db *storage.DB, pay *payments.Service, sender Sender, cfg *config.Settings) error {
	cfg = cfg.Current()
	now := time.Now().UTC()
	windows := []struct {
		kind     string
		from, to time.Time
		text     string
	}{
		{"expiry_soon", now.Add(24 * time.Hour), now.Add(72 * time.Hour), "⏳ <b>Подписка скоро заканчивается</b>\nПродлите заранее через /buy."},
		{"expired_winback", now.Add(-30 * 24 * time.Hour), now.Add(-3 * 24 * time.Hour), "Вернуться можно в пару нажатий: /buy. Ваши настройки подключения сохранятся."},
	}
	for _, window := range windows {
		orders, err := db.ExpiringSubscriptions(ctx, window.from, window.to)
		if err != nil {
			return err
		}
		for _, order := range orders {
			if order.TelegramID == nil || order.ExpireAt == nil {
				continue
			}
			prefs, e := db.NotificationSettingsFor(ctx, *order.TelegramID)
			if e != nil {
				return e
			}
			if !prefs.Expiry || window.kind == "expired_winback" && !prefs.Activity {
				continue
			}
			subject, e := db.SubjectByTelegramID(ctx, *order.TelegramID)
			if e != nil {
				return e
			}
			if a, e := db.LatestRecurringForSubject(ctx, subject.ID); e == nil && a.Enabled {
				continue
			} else if e != nil && e != storage.ErrNotFound {
				return e
			}
			access, e := pay.SupportAccess(ctx, subject)
			if e != nil {
				continue
			}
			if access.ExpireAt != nil && access.ExpireAt.After(*order.ExpireAt) {
				continue
			}
			if e = sendEngagement(ctx, db, sender, *order.TelegramID, window.kind, *order.ExpireAt, window.text+"\nСрок подписки: "+order.ExpireAt.Format("02.01.2006")); e != nil {
				return e
			}
		}
	}
	orders, err := db.ExpiringDeviceNotifications(ctx)
	if err != nil {
		return err
	}
	for _, o := range orders {
		prefs, e := db.NotificationSettingsFor(ctx, *o.TelegramID)
		if e != nil {
			return e
		}
		if !prefs.Expiry {
			continue
		}
		kind := "devices_expiry_soon"
		if o.DeviceExpiresAt.Sub(now) < 24*time.Hour {
			kind = "devices_expiry_today"
		}
		text := fmt.Sprintf("⏳ Пакет устройств по заказу <code>%s</code> действует до %s.\nУправление и продление: /devices.", html.EscapeString(o.OrderID), o.DeviceExpiresAt.Format("02.01.2006"))
		if e = sendEngagement(ctx, db, sender, *o.TelegramID, kind+":"+o.OrderID, *o.DeviceExpiresAt, text); e != nil {
			return e
		}
	}
	_, err = db.ReleaseExpiredLocks(ctx)
	return err
}

func observedEvery(ctx context.Context, db *storage.DB, sender Sender, cfg *config.Settings, name string, interval time.Duration, task func(context.Context) error) error {
	return every(ctx, name, interval, func(ctx context.Context) error {
		if err := db.RecordJobStart(ctx, name, interval); err != nil {
			return err
		}
		runCtx, cancel := context.WithTimeout(ctx, max(time.Minute, interval))
		defer cancel()
		err := task(runCtx)
		if ctx.Err() != nil {
			return err
		}
		if recordErr := db.RecordJobResult(ctx, name, err == nil); recordErr != nil {
			return recordErr
		}
		if err != nil {
			if alert, e := db.ClaimJobAlert(ctx, name); e == nil && alert {
				for id := range cfg.Current().AdminIDs {
					_ = sender.SendText(ctx, id, "⚠️ Фоновая задача «"+html.EscapeString(name)+"» не завершилась успешно три раза подряд. Проверьте /admin/operations на сайте и журнал сервиса.")
				}
			}
		}
		return err
	})
}
