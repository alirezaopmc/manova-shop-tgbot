package config

import (
	"os"
	"testing"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/secrets"
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

func TestDatabaseDSN(t *testing.T) {
	cfg := &Config{
		DBHost:     "127.0.0.1",
		DBPort:     10000,
		DBUser:     "shop",
		DBName:     "shop",
		DBSSLMode:  "disable",
		DBPassword: secrets.New("secret123"),
	}

	expected := "postgres://shop:secret123@127.0.0.1:10000/shop?sslmode=disable"
	if got := cfg.DatabaseDSN(); got != expected {
		t.Fatalf("expected DSN %q, got %q", expected, got)
	}

	// DATABASE_URL takes precedence
	cfg.DatabaseURL = "postgres://custom:pass@remote:5432/mydb?sslmode=require"
	if got := cfg.DatabaseDSN(); got != cfg.DatabaseURL {
		t.Fatalf("expected DSN %q, got %q", cfg.DatabaseURL, got)
	}

	// os.Getenv("DATABASE_URL") takes precedence when cfg.DatabaseURL is empty
	cfg.DatabaseURL = ""
	os.Setenv("DATABASE_URL", "postgres://envuser:envpass@envhost:5432/envdb?sslmode=verify-full")
	defer os.Unsetenv("DATABASE_URL")

	if got := cfg.DatabaseDSN(); got != "postgres://envuser:envpass@envhost:5432/envdb?sslmode=verify-full" {
		t.Fatalf("expected env DSN, got %q", got)
	}
}
