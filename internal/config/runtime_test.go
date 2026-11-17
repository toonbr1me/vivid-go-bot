package config

import (
	"sync"
	"testing"
)

func TestDatabaseSettingsCannotOverrideCredentialsOrIdentity(t *testing.T) {
	baseEnv(t)
	t.Setenv("ADMIN_IDS", "100")
	t.Setenv("API_TOKEN", "env-panel-token")
	t.Setenv("WATA_ACCESS_TOKEN", "env-payment-token")
	cfg, err := Load(map[string]string{"BOT_TOKEN": "evil", "DATABASE_URL": "evil", "ADMIN_IDS": "999", "API_TOKEN": "evil", "WATA_ACCESS_TOKEN": "evil", "BRAND_NAME": "Changed", "TRIAL_BOT_DAYS": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramToken == "evil" || cfg.APIToken != "env-panel-token" || cfg.WataToken != "env-payment-token" || cfg.AdminIDs[999] || !cfg.AdminIDs[100] {
		t.Fatal("database changed an env-only setting")
	}
	if cfg.BrandName != "Changed" || cfg.TrialBotDays != 3 {
		t.Fatal("allowed settings were ignored")
	}
}

func TestRuntimeSnapshotsArePublishedWhole(t *testing.T) {
	cfg := &Settings{BrandName: "old", TrialBotDays: 1}
	cfg.EnableRuntime()
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Go(func() {
			for j := 0; j < 1000; j++ {
				v := cfg.Current()
				if v.BrandName == "new" && v.TrialBotDays != 7 {
					t.Error("partial configuration")
				}
			}
		})
	}
	for i := 0; i < 100; i++ {
		cfg.Publish(&Settings{BrandName: "new", TrialBotDays: 7})
	}
	group.Wait()
}
