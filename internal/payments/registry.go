package payments

import (
	"fmt"
	"time"

	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// Registry — включённые шлюзы в порядке приоритета.
type Registry struct {
	providers map[string]Provider
	order     []string
}

// BuildRegistry собирает провайдеров из настроек. Ошибка конфигурации
// ловится на старте, а не на первом платеже.
func BuildRegistry(cfg *config.Settings, linker InvoiceLinker) (*Registry, error) {
	timeout := time.Duration(cfg.RequestTimeoutSecs * float64(time.Second))
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	registry := &Registry{providers: map[string]Provider{}}
	for _, name := range cfg.PaymentProviders {
		switch name {
		case "wata":
			registry.add(NewWataProvider(cfg.WataBaseURL, cfg.WataToken, cfg.WataPublicKeyURL,
				cfg.PaymentMethods, cfg.WataMethodTokens, timeout))
		case "platega":
			registry.add(NewPlategaProvider(cfg.PlategaBaseURL, cfg.PlategaMerchant, cfg.PlategaSecret,
				cfg.PaymentMethods, timeout))
		case "yookassa":
			registry.add(NewYooKassaProvider(cfg.YooKassaBaseURL, cfg.YooKassaShopID, cfg.YooKassaSecret,
				cfg.PaymentMethods, timeout))
		case "stars":
			if linker == nil {
				return nil, fmt.Errorf("звёзды включены, но бот не передан платёжной службе")
			}
			registry.add(NewStarsProvider(linker, cfg.StarsPrice))
		default:
			return nil, fmt.Errorf("неизвестный платёжный провайдер %q", name)
		}
	}
	return registry, nil
}

func (r *Registry) add(provider Provider) {
	name := provider.Name()
	if _, exists := r.providers[name]; exists {
		return
	}
	r.providers[name] = provider
	r.order = append(r.order, name)
}

// Get возвращает шлюз по имени.
func (r *Registry) Get(name string) (Provider, bool) {
	provider, ok := r.providers[name]
	return provider, ok
}

// First возвращает приоритетный шлюз для оплаты.
func (r *Registry) First(web bool) (Provider, bool) {
	for _, name := range r.order {
		provider := r.providers[name]
		if web && provider.TelegramOnly() {
			continue
		}
		return provider, true
	}
	return nil, false
}

// Enabled — есть ли хоть один шлюз.
func (r *Registry) Enabled() bool { return len(r.providers) > 0 }

// WebEnabled — можно ли платить с сайта (звёзды только в Telegram).
func (r *Registry) WebEnabled() bool {
	_, ok := r.First(true)
	return ok
}

// Options — все комбинации «шлюз × способ оплаты» для витрины.
func (r *Registry) Options(web bool) []domain.PaymentOption {
	var options []domain.PaymentOption
	for _, name := range r.order {
		provider := r.providers[name]
		if web && provider.TelegramOnly() {
			continue
		}
		for _, method := range provider.Methods() {
			options = append(options, domain.PaymentOption{
				Provider:      name,
				Method:        method,
				ProviderLabel: providerLabel(name),
				MethodLabel:   methodLabel(method),
				Recommended:   len(options) == 0,
			})
		}
	}
	return options
}

// ResolveOption разбирает токен формы, не позволяя выбрать чужую пару.
// Старые формы присылали только способ — они продолжают работать через
// приоритетный шлюз.
func (r *Registry) ResolveOption(token string, web bool) (string, string, error) {
	if token == "" {
		return "", "", nil
	}
	for _, option := range r.Options(web) {
		if option.Token() == token {
			return option.Provider, option.Method, nil
		}
	}
	for _, option := range r.Options(web) {
		if option.Method == token {
			return "", token, nil
		}
	}
	return "", "", fmt.Errorf("неизвестный способ оплаты %q", token)
}

// StarsAmount — цена в звёздах для подписи кнопки; ноль — звёзды выключены.
func (r *Registry) StarsAmount(amount domain.Money) int {
	provider, ok := r.providers["stars"]
	if !ok {
		return 0
	}
	stars, ok := provider.(*StarsProvider)
	if !ok {
		return 0
	}
	return stars.StarsFor(amount)
}
