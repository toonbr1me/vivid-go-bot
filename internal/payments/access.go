package payments

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
)

// Access собирает состояние подписки для экранов бота и сайта.
//
// Источник истины — панель: лимит устройств применяет она, срок и трафик
// тоже её. Если панель недоступна, отдаём хотя бы срок последнего выданного
// заказа и возвращаем ошибку: экран покажет данные, а обработчик решит,
// стоит ли ругаться вслух.
func (s *Service) Access(ctx context.Context, subject domain.Subject, username string) (domain.Access, error) {
	access := domain.Access{Username: fallbackUsername(subject)}
	if strings.TrimSpace(username) != "" {
		access.Username = username
	}
	if subject.RemnawaveUser != nil && *subject.RemnawaveUser != "" {
		access.Username = *subject.RemnawaveUser
	}

	var panelErr error
	if s.panelReady() {
		users, err := s.findPanelUsers(ctx, subject.TelegramID, access.Username)
		if err != nil {
			panelErr = err
		} else if user := remnawave.PickUser(users); user != nil {
			panelErr = s.fillAccess(ctx, &access, subject, *user)
			if subject.TelegramID != nil && subject.RemnawaveUser != nil && user.TelegramID == nil {
				// Repair an interrupted account link with an absolute identity update.
				_, err := s.panel.UpdateUser(ctx, remnawave.UpdateUserParams{UserID: user.ID, TelegramID: subject.TelegramID})
				panelErr = errors.Join(panelErr, err)
			}
		}
	}
	if access.Found {
		return access, panelErr
	}

	// Панель молчит или пользователя в ней ещё нет. Срок из последнего
	// выданного заказа хотя бы не даст экрану соврать про отсутствие
	// подписки у человека, который только что заплатил.
	if order, err := s.db.SubscriptionForOwner(ctx, subject.TelegramID,
		accountIDPtr(subject, Purchase{})); err == nil && order != nil && order.ExpireAt != nil {
		expire := *order.ExpireAt
		access.ExpireAt = &expire
		access.Active = expire.After(time.Now().UTC())
	}
	return access, panelErr
}

// fillAccess переносит ответ панели в доменную модель.
func (s *Service) fillAccess(ctx context.Context, access *domain.Access, subject domain.Subject, user remnawave.User) error {
	access.Found = true
	access.PanelUserID = user.ID
	access.Username = user.Username
	access.Active = user.IsActive()
	access.URL = remnawave.SubscriptionURL(s.cfg.APIBaseURL, user)
	access.TrafficUsedBytes = user.TrafficUsedBytes
	access.TrafficLimit = user.TrafficLimitBytes
	if !user.ExpireAt.IsZero() {
		expire := user.ExpireAt.Time
		access.ExpireAt = &expire
	}

	limit := panelDeviceLimit(user)
	if limit == 0 && s.cfg.DeviceLimitEnabled {
		if managed, err := s.ManagedDeviceLimit(ctx, subject.ID); err == nil && managed > limit {
			limit = managed
		}
	}
	access.DeviceLimit = limit

	// Список устройств — украшение экрана, а не его основа: сбой отдельного
	// запроса не должен прятать срок и ссылку подписки.
	if !s.panelReady() || user.ID == 0 {
		return nil
	}
	devices, err := s.panel.HWIDDevices(ctx, user.ID)
	if err != nil {
		return err
	}
	access.Devices = make([]domain.AccessDevice, 0, len(devices))
	for _, device := range devices {
		access.Devices = append(access.Devices, domain.AccessDevice{
			HWID:      device.HWID,
			Platform:  device.Platform,
			Model:     device.DeviceModel,
			CreatedAt: device.CreatedAt.Time,
		})
	}
	return nil
}

// RemoveAccessDevice отвязывает устройство от подписки.
func (s *Service) RemoveAccessDevice(ctx context.Context, userID int64, hwid string) (bool, error) {
	if !s.panelReady() || userID == 0 || strings.TrimSpace(hwid) == "" {
		return false, ErrNoSubscription
	}
	return s.panel.DeleteHWIDDevice(ctx, userID, hwid)
}
