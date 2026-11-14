package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Роли сотрудников. Владелец — не строка staff, а список ADMIN_IDS.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleSupport = "support"
)

// StaffMember — сотрудник.
type StaffMember struct {
	TelegramID  int64
	Role        string
	DisplayName string
	IsActive    bool
}

// Staff читает активный состав.
func (db *DB) Staff(ctx context.Context) ([]StaffMember, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT telegram_id, role, display_name, is_active
		FROM staff WHERE is_active ORDER BY added_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StaffMember
	for rows.Next() {
		var member StaffMember
		if err := rows.Scan(&member.TelegramID, &member.Role, &member.DisplayName, &member.IsActive); err != nil {
			return nil, err
		}
		out = append(out, member)
	}
	return out, rows.Err()
}

// StaffMemberByID читает сотрудника; nil — его нет.
func (db *DB) StaffMemberByID(ctx context.Context, telegramID int64) (*StaffMember, error) {
	var member StaffMember
	err := db.Pool.QueryRow(ctx, `
		SELECT telegram_id, role, display_name, is_active
		FROM staff WHERE telegram_id = $1`, telegramID).Scan(
		&member.TelegramID, &member.Role, &member.DisplayName, &member.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &member, err
}

// AddStaff добавляет или обновляет сотрудника.
func (db *DB) AddStaff(ctx context.Context, telegramID int64, role, name string, addedBy int64) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO staff (telegram_id, role, display_name, added_by, is_active)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (telegram_id) DO UPDATE
		   SET role = EXCLUDED.role, display_name = EXCLUDED.display_name,
		       is_active = TRUE`, telegramID, role, name, addedBy)
	return err
}

// RemoveStaff деактивирует сотрудника.
func (db *DB) RemoveStaff(ctx context.Context, telegramID int64) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE staff SET is_active = FALSE WHERE telegram_id = $1`, telegramID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// LogAction пишет журнал действий staff.
func (db *DB) LogAction(ctx context.Context, actorID int64, action, target string, amount domain.Money, details string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO staff_audit (actor_id, action, target, amount, details)
		VALUES ($1, $2, $3, $4, $5)`,
		actorID, action, nullString(target), amount.String(), nullString(details))
	return err
}

// SpentToday — сколько дней компенсаций сотрудник выдал за сутки.
func (db *DB) SpentToday(ctx context.Context, actorID int64, action string) (int, error) {
	var total int
	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(sum(amount), 0)::int FROM staff_audit
		WHERE actor_id = $1 AND action = $2 AND created_at >= date_trunc('day', now())`,
		actorID, action).Scan(&total)
	return total, err
}

// AuditEntry — строка журнала.
type AuditEntry struct {
	ActorID   int64
	Action    string
	Target    *string
	Amount    domain.Money
	Details   *string
	CreatedAt string
}

// RecentAudit — последние действия.
func (db *DB) RecentAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT actor_id, action, target, round(amount * 100)::bigint, details, created_at::text
		FROM staff_audit ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var cents int64
		if err := rows.Scan(&entry.ActorID, &entry.Action, &entry.Target,
			&cents, &entry.Details, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entry.Amount = domain.Money(cents)
		out = append(out, entry)
	}
	return out, rows.Err()
}

// Permission проверяет право сотрудника. Владелец умеет всё.
func (db *DB) Permission(ctx context.Context, telegramID int64, action string, adminIDs map[int64]bool) bool {
	if adminIDs[telegramID] {
		return true
	}
	member, err := db.StaffMemberByID(ctx, telegramID)
	if err != nil || member == nil || !member.IsActive {
		return false
	}
	switch action {
	case "staff.manage", "settings.edit", "plans.edit", "orders.view", "orders.retry":
		return member.Role == RoleOwner || member.Role == RoleAdmin
	case "support.answer", "support.compensate":
		return true
	default:
		return true
	}
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
