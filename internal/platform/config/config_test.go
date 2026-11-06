package config

import (
	"os"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.AdminTelegramID != 6246642516 {
		t.Fatalf("expected admin ID 6246642516, got %d", cfg.AdminTelegramID)
	}

	if cfg.RateSpreadIRTPerUSD != 5000 {
		t.Fatalf("expected rate spread 5000, got %d", cfg.RateSpreadIRTPerUSD)
	}

	if cfg.NewFloorIRT != 0 || cfg.CustomerFloorIRT != -100 || cfg.LoyalFloorIRT != -500 {
		t.Fatalf("unexpected floor defaults")
	}

	if len(cfg.WarnRunwayHours) != 3 || cfg.WarnRunwayHours[0] != 24 {
		t.Fatalf("unexpected runway warnings: %v", cfg.WarnRunwayHours)
	}

	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected validation error when secrets are missing in production")
	}
}

func TestConfigEnvOverrides(t *testing.T) {
	os.Setenv("ADMIN_TELEGRAM_ID", "999888777")
	os.Setenv("WARN_RUNWAY_HOURS", "48, 12, 2")
	os.Setenv("TELEGRAM_BOT_TOKEN", "mock-token")
	os.Setenv("HCLOUD_TOKEN", "mock-hcloud")
	os.Setenv("DB_PASSWORD", "mock-pwd")
	os.Setenv("CREDENTIAL_KEK", "mock-kek")
	defer func() {
		os.Unsetenv("ADMIN_TELEGRAM_ID")
		os.Unsetenv("WARN_RUNWAY_HOURS")
		os.Unsetenv("TELEGRAM_BOT_TOKEN")
		os.Unsetenv("HCLOUD_TOKEN")
		os.Unsetenv("DB_PASSWORD")
		os.Unsetenv("CREDENTIAL_KEK")
	}()

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.AdminTelegramID != 999888777 {
		t.Fatalf("expected 999888777, got %d", cfg.AdminTelegramID)
	}

	if len(cfg.WarnRunwayHours) != 3 || cfg.WarnRunwayHours[0] != 48 {
		t.Fatalf("expected overridden runway hours, got %v", cfg.WarnRunwayHours)
	}

	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("expected validation to pass when secrets are set: %v", err)
	}
}
