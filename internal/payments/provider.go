// Package payments — приём денег и выдача оплаченного.
//
// Провайдеры знают только про свой протокол: собрать ссылку и разобрать
// уведомление. Всё остальное — тарифы, промокоды, продление доступа,
// начисления — живёт в Service и не зависит от того, кто именно прислал
// деньги.
package payments

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Статусы уведомления. Всё, что не paid, заказ не продлевает.
type Status string

const (
	StatusPaid      Status = "paid"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Уведомление о платеже в общем для всех провайдеров виде.
type Event struct {
	OrderID        string
	Status         Status
	TransactionID  string
	PaidAt         *time.Time
	Amount         *domain.Money
	Currency       string
	SavedMethodID  string
	SubscriptionID string
	NextChargeAt   *time.Time
}

// Link — созданный счёт.
type Link struct {
	URL       string
	ID        string
	ExpiresAt *time.Time
	Stars     int
}

// CreateRequest — параметры счёта, одинаковые для всех шлюзов.
type CreateRequest struct {
	OrderID       string
	Amount        domain.Money
	Currency      string
	Description   string
	ExpiresIn     time.Duration
	SuccessURL    string
	FailURL       string
	Method        string
	Recurring     bool
	PlanDays      int
	CustomerEmail string
	SavedMethodID string
}

// Provider — платёжный шлюз.
type Provider interface {
	Name() string
	// Methods — способы оплаты, которые шлюз умеет из общего списка.
	Methods() []string
	// TelegramOnly — счёт открывается только внутри Telegram.
	TelegramOnly() bool
	CreateLink(ctx context.Context, req CreateRequest) (Link, error)
	// Verify разбирает уведомление. ErrNoEvent — уведомление не про платёж:
	// на него надо ответить «ок» и ничего не менять.
	Verify(ctx context.Context, body []byte, headers http.Header) (Event, error)
}

// ErrVerification — подпись не сошлась или тело не разобрано.
var ErrVerification = errors.New("уведомление не прошло проверку")

// ErrNoEvent — уведомление валидно, но заказа не касается.
var ErrNoEvent = errors.New("уведомление не относится к платежу")

// Provider labels для витрины. Имя провайдера, которого здесь нет,
// показывается как есть — новый шлюз не должен ломать экран.
func providerLabel(name string) string {
	switch name {
	case "stars":
		return "Telegram"
	case "yookassa":
		return "ЮKassa"
	case "platega":
		return "Platega"
	case "wata":
		return "WATA"
	default:
		return name
	}
}

// methodLabel — человеческое имя способа оплаты.
func methodLabel(method string) string {
	switch method {
	case "card":
		return "Банковская карта"
	case "sbp":
		return "СБП"
	case "tpay":
		return "T-Pay"
	case "sberpay":
		return "SberPay"
	case "crypto":
		return "Криптовалюта"
	case "stars":
		return "Звёзды"
	default:
		return method
	}
}
