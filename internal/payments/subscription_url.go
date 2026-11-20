package payments

import (
	"net/url"
	"strings"

	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
)

// serviceSubscriptionURL keeps subscription links on the service's public
// domain while preserving the path and query returned by Remnawave.
func serviceSubscriptionURL(apiBaseURL, subscriptionBaseURL string, user remnawave.User) string {
	link := remnawave.SubscriptionURL(apiBaseURL, user)
	if link == "" || strings.TrimSpace(subscriptionBaseURL) == "" {
		return link
	}

	base, err := url.Parse(strings.TrimSpace(subscriptionBaseURL))
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return link
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return link
	}
	parsed.Scheme = base.Scheme
	parsed.Host = base.Host
	return parsed.String()
}
