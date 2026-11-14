package payments

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// StarsCurrency — валюта счёта Telegram Stars.
const StarsCurrency = "XTR"

// StarsProvider — счёт в звёздах. У него нет страницы шлюза и вебхука:
// оплату бот узнаёт сообщением successful_payment.
type StarsProvider struct {
	linker    InvoiceLinker
	starPrice domain.Money
}

// InvoiceLinker — часть telego.Bot, которая нужна звёздам. Отдельный
// интерфейс, чтобы пакет не тащил весь бот и его можно было подменить в тесте.
type InvoiceLinker interface {
	CreateInvoiceLink(ctx context.Context, params *telego.CreateInvoiceLinkParams) (*string, error)
}

func NewStarsProvider(linker InvoiceLinker, starPrice domain.Money) *StarsProvider {
	return &StarsProvider{linker: linker, starPrice: starPrice}
}

func (p *StarsProvider) Name() string       { return "stars" }
func (p *StarsProvider) Methods() []string  { return []string{"stars"} }
func (p *StarsProvider) TelegramOnly() bool { return true }

// StarsFor — сколько звёзд стоит сумма. Округляем вверх, минимум одна.
func (p *StarsProvider) StarsFor(amount domain.Money) int {
	if p.starPrice <= 0 || amount <= 0 {
		return 1
	}
	stars := (int64(amount) + int64(p.starPrice) - 1) / int64(p.starPrice)
	if stars < 1 {
		stars = 1
	}
	return int(stars)
}

// StarsFor — удобный вызов там, где провайдер не нужен.
func StarsFor(amount domain.Money, starPrice domain.Money) int {
	if starPrice <= 0 || amount <= 0 {
		return 1
	}
	stars := (int64(amount) + int64(starPrice) - 1) / int64(starPrice)
	if stars < 1 {
		stars = 1
	}
	return int(stars)
}

func (p *StarsProvider) CreateLink(ctx context.Context, req CreateRequest) (Link, error) {
	stars := p.StarsFor(req.Amount)
	title := "Оплата заказа"
	description := req.Description
	if runes := []rune(description); len(runes) > 255 {
		description = string(runes[:255])
	}
	url, err := p.linker.CreateInvoiceLink(ctx, &telego.CreateInvoiceLinkParams{
		Title:       title,
		Description: description,
		Payload:     StarsPayload(req.OrderID, stars),
		Currency:    StarsCurrency,
		Prices:      []telego.LabeledPrice{{Label: title, Amount: stars}},
	})
	if err != nil {
		return Link{}, fmt.Errorf("Telegram не открыл счёт: %w", err)
	}
	if url == nil || *url == "" {
		return Link{}, fmt.Errorf("Telegram не вернул ссылку на счёт")
	}
	expires := time.Now().UTC().Add(req.ExpiresIn)
	return Link{URL: *url, ID: "stars", ExpiresAt: &expires, Stars: stars}, nil
}

func (p *StarsProvider) Verify(context.Context, []byte, http.Header) (Event, error) {
	return Event{}, fmt.Errorf("%w: звёзды не используют вебхук", ErrVerification)
}

// StarsPayload — то, что Telegram вернёт в successful_payment без изменений:
// номер заказа и замороженное число звёзд. Сверка не зависит от курса,
// который мог поменяться, пока счёт ждал оплаты.
func StarsPayload(orderID string, stars int) string {
	return orderID + "|" + strconv.Itoa(stars)
}

// ParseStarsPayload разбирает payload счёта.
func ParseStarsPayload(payload string) (string, int, bool) {
	index := strings.LastIndex(payload, "|")
	if index <= 0 || index == len(payload)-1 {
		return "", 0, false
	}
	stars, err := strconv.Atoi(payload[index+1:])
	if err != nil || stars <= 0 {
		return "", 0, false
	}
	return payload[:index], stars, true
}
