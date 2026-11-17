package config

import "sync/atomic"

// EditableSettings is also the allow-list at the database configuration boundary.
// Credentials, listeners, identity providers and ADMIN_IDS always come from env.
var EditableSettings = []string{
	"BRAND_NAME", "SUPPORT_EMAIL", "SUPPORT_USERNAME", "LEGAL_ENTITY", "TERMS_URL", "PRIVACY_POLICY_URL",
	"TRIAL_WEB_DAYS", "TRIAL_BOT_DAYS", "TRIAL_TRAFFIC_LIMIT_BYTES", "TRAFFIC_LIMIT_BYTES",
	"DEVICE_LIMIT_BASE", "REFERRAL_MODE", "REFERRAL_BALANCE_PERCENT",
	"REMIND_UNPAID_INVOICE_HOURS", "REMIND_NOT_CONNECTED_HOURS", "SUPPORT_COMPENSATION_DAILY_DAYS",
}

func Editable(key string) bool {
	for _, name := range EditableSettings {
		if name == key {
			return true
		}
	}
	return false
}

type runtimeSettings struct{ value atomic.Pointer[Settings] }

// EnableRuntime is called once during construction, before sharing Settings.
func (s *Settings) EnableRuntime() {
	if s.runtime == nil {
		s.runtime = &runtimeSettings{}
	}
}
func (s *Settings) Current() *Settings {
	if s == nil {
		return nil
	}
	if s.runtime != nil {
		if v := s.runtime.value.Load(); v != nil {
			return v
		}
	}
	return s
}

// Publish replaces an immutable snapshot; readers never observe a partial edit.
func (s *Settings) Publish(next *Settings) { next.runtime = s.runtime; s.runtime.value.Store(next) }
