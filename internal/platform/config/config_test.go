package config

import (
	"os"
	"testing"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/secrets"
)

func TestConfigDefaults(t *testing.T) {
	os.Setenv("SHOPBOT_NO_DOTENV", "1")
	defer os.Unsetenv("SHOPBOT_NO_DOTENV")

	varsToClear := []string{
		"ADMIN_TELEGRAM_ID",
		"CARD_HOLDER_NAME",
		"TELEGRAM_BOT_TOKEN",
		"HCLOUD_TOKEN",
		"DB_PASSWORD",
		"CREDENTIAL_KEK",
	}
	for _, v := range varsToClear {
		if old, ok := os.LookupEnv(v); ok {
			defer os.Setenv(v, old)
			os.Unsetenv(v)
		}
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.AdminTelegramID != 0 {
		t.Fatalf("expected admin ID 0, got %d", cfg.AdminTelegramID)
	}

	if cfg.CardHolderName != "" {
		t.Fatalf("expected empty card holder name, got %q", cfg.CardHolderName)
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
	os.Setenv("CARD_HOLDER_NAME", "Jane Doe")
	os.Setenv("WARN_RUNWAY_HOURS", "48, 12, 2")
	os.Setenv("TELEGRAM_BOT_TOKEN", "mock-token")
	os.Setenv("HCLOUD_TOKEN", "mock-hcloud")
	os.Setenv("DB_PASSWORD", "mock-pwd")
	os.Setenv("CREDENTIAL_KEK", "mock-kek")
	defer func() {
		os.Unsetenv("ADMIN_TELEGRAM_ID")
		os.Unsetenv("CARD_HOLDER_NAME")
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

	if cfg.CardHolderName != "Jane Doe" {
		t.Fatalf("expected Jane Doe, got %s", cfg.CardHolderName)
	}

	if len(cfg.WarnRunwayHours) != 3 || cfg.WarnRunwayHours[0] != 48 {
		t.Fatalf("expected overridden runway hours, got %v", cfg.WarnRunwayHours)
	}

	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("expected validation to pass when secrets are set: %v", err)
	}
}

func TestValidateProduction(t *testing.T) {
	validCfg := func() *Config {
		return &Config{
			TelegramBotToken: secrets.New("tg-tok"),
			HCloudToken:      secrets.New("hc-tok"),
			DBPassword:       secrets.New("db-pass"),
			CredentialKEK:    secrets.New("kek-val"),
			AdminTelegramID:  123456789,
			CardHolderName:   "Admin User",
		}
	}

	// Valid config passes
	if err := validCfg().ValidateProduction(); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}

	// AdminTelegramID zero or negative fails
	cfg := validCfg()
	cfg.AdminTelegramID = 0
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected error when AdminTelegramID is 0")
	}

	cfg = validCfg()
	cfg.AdminTelegramID = -10
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected error when AdminTelegramID is negative")
	}

	// CardHolderName empty fails
	cfg = validCfg()
	cfg.CardHolderName = ""
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected error when CardHolderName is empty")
	}

	// Missing secrets fail
	cfg = validCfg()
	cfg.TelegramBotToken = secrets.New("")
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected error when TelegramBotToken is empty")
	}

	cfg = validCfg()
	cfg.HCloudToken = secrets.New("")
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected error when HCloudToken is empty")
	}

	cfg = validCfg()
	cfg.DBPassword = secrets.New("")
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected error when DBPassword is empty")
	}

	cfg = validCfg()
	cfg.CredentialKEK = secrets.New("")
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatalf("expected error when CredentialKEK is empty")
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
	_ = os.Unsetenv("DATABASE_URL")

	// URL encoding with reserved URI characters in password & username
	cfgSpecial := &Config{
		DBHost:     "127.0.0.1",
		DBPort:     5432,
		DBUser:     "shop:user",
		DBName:     "shop_db",
		DBSSLMode:  "disable",
		DBPassword: secrets.New("p@ss:w/o?r#d%1"),
	}
	expectedSpecial := "postgres://shop%3Auser:p%40ss%3Aw%2Fo%3Fr%23d%251@127.0.0.1:5432/shop_db?sslmode=disable"
	if got := cfgSpecial.DatabaseDSN(); got != expectedSpecial {
		t.Fatalf("expected encoded DSN %q, got %q", expectedSpecial, got)
	}
}
