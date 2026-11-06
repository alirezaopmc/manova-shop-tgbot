package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/secrets"
)

// Config represents all shop-tgbot settings.
type Config struct {
	// Secrets
	TelegramBotToken      secrets.Secret[string]
	HCloudToken           secrets.Secret[string]
	DBPassword            secrets.Secret[string]
	CredentialKEK         secrets.Secret[string]
	CloudflareTunnelToken secrets.Secret[string]

	// Admin and Support
	AdminTelegramID int64
	SupportHandle   string
	TotpIssuer      string
	CardHolderName  string

	// Database
	DBHost    string
	DBPort    int
	DBUser    string
	DBName    string
	DBSSLMode string

	// Pricing & FX
	RateSpreadIRTPerUSD       int64
	RequoteThresholdIRTPerUSD int64
	RateMaxAgeHours           int
	SwapWalletAPIURL          string

	// Limits & Receipts
	ReceiptMinIRR      int64
	ReceiptMaxIRR      int64
	MaxRAMGB           int
	OrderExpiryMinutes int
	MinBillableMinutes int

	// Floors & Segments (in IRT)
	NewMaxServers              int
	NewFloorIRT                int64
	CustomerFloorIRT           int64
	LoyalFloorIRT              int64
	OverrideFloorLowerBoundIRT int64

	// Scheduling & Billing
	ReconcileIntervalMinutes int
	WarnRunwayHours          []int
	DeletionGraceMinutes     int

	// Hetzner Rate Windows
	HetznerGlobalPerMinute     int
	HetznerUserPerMinute       int
	HetznerUserConcurrency     int
	HetznerSystemReservePerMin int

	// OS Catalog
	OSDistributions         []string
	OSCatalogRefreshMinutes int
	OSReselectWindowHours   int

	// Web Tunnels
	RevealBaseURL string
	AdminBaseURL  string
}

// DatabaseDSN generates standard PostgreSQL connection string.
func (c *Config) DatabaseDSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword.Expose(), c.DBName, c.DBSSLMode)
}

// LoadFromEnv loads configuration from environment variables with sensible defaults.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		TelegramBotToken:      secrets.New(os.Getenv("TELEGRAM_BOT_TOKEN")),
		HCloudToken:           secrets.New(os.Getenv("HCLOUD_TOKEN")),
		DBPassword:            secrets.New(os.Getenv("DB_PASSWORD")),
		CredentialKEK:         secrets.New(os.Getenv("CREDENTIAL_KEK")),
		CloudflareTunnelToken: secrets.New(os.Getenv("CLOUDFLARE_TUNNEL_TOKEN")),

		AdminTelegramID: getEnvInt64("ADMIN_TELEGRAM_ID", 6246642516),
		SupportHandle:   getEnvString("SUPPORT_HANDLE", "@manova_support"),
		TotpIssuer:      getEnvString("TOTP_ISSUER", "shop_manovabot"),
		CardHolderName:  getEnvString("CARD_HOLDER_NAME", "Alireza Jafartash"),

		DBHost:    getEnvString("DATABASE_HOST", "localhost"),
		DBPort:    getEnvInt("DATABASE_PORT", 5432),
		DBUser:    getEnvString("DATABASE_USER", "shopbot"),
		DBName:    getEnvString("DATABASE_NAME", "shopbot"),
		DBSSLMode: getEnvString("DATABASE_SSLMODE", "disable"),

		RateSpreadIRTPerUSD:       getEnvInt64("RATE_SPREAD_IRT_PER_USD", 5000),
		RequoteThresholdIRTPerUSD: getEnvInt64("REQUOTE_THRESHOLD_IRT_PER_USD", 5000),
		RateMaxAgeHours:           getEnvInt("RATE_MAX_AGE_HOURS", 6),
		SwapWalletAPIURL:          getEnvString("SWAPWALLET_API_URL", "https://api.swapwallet.io"),

		ReceiptMinIRR:      getEnvInt64("RECEIPT_MIN_IRR", 1000000),
		ReceiptMaxIRR:      getEnvInt64("RECEIPT_MAX_IRR", 500000000),
		MaxRAMGB:           getEnvInt("MAX_RAM_GB", 16),
		OrderExpiryMinutes: getEnvInt("ORDER_EXPIRY_MINUTES", 60),
		MinBillableMinutes: getEnvInt("MIN_BILLABLE_MINUTES", 60),

		NewMaxServers:              getEnvInt("NEW_MAX_SERVERS", 1),
		NewFloorIRT:                getEnvInt64("NEW_FLOOR_IRT", 0),
		CustomerFloorIRT:           getEnvInt64("CUSTOMER_FLOOR_IRT", -100),
		LoyalFloorIRT:              getEnvInt64("LOYAL_FLOOR_IRT", -500),
		OverrideFloorLowerBoundIRT: getEnvInt64("OVERRIDE_FLOOR_LOWER_BOUND_IRT", -1000000),

		ReconcileIntervalMinutes: getEnvInt("RECONCILE_INTERVAL_MINUTES", 5),
		WarnRunwayHours:          getEnvIntSlice("WARN_RUNWAY_HOURS", []int{24, 6, 1}),
		DeletionGraceMinutes:     getEnvInt("DELETION_GRACE_MINUTES", 60),

		HetznerGlobalPerMinute:     getEnvInt("HETZNER_GLOBAL_PER_MINUTE", 60),
		HetznerUserPerMinute:       getEnvInt("HETZNER_USER_PER_MINUTE", 30),
		HetznerUserConcurrency:     getEnvInt("HETZNER_USER_CONCURRENCY", 2),
		HetznerSystemReservePerMin: getEnvInt("HETZNER_SYSTEM_RESERVE_PER_MINUTE", 10),

		OSDistributions:         getEnvStringSlice("OS_DISTRIBUTIONS", []string{"ubuntu", "debian"}),
		OSCatalogRefreshMinutes: getEnvInt("OS_CATALOG_REFRESH_MINUTES", 15),
		OSReselectWindowHours:   getEnvInt("OS_RESELECT_WINDOW_HOURS", 72),

		RevealBaseURL: getEnvString("REVEAL_BASE_URL", "https://reveal.internal.manova.space"),
		AdminBaseURL:  getEnvString("ADMIN_BASE_URL", "https://admin.internal.manova.space"),
	}

	return cfg, nil
}

// ValidateProduction checks that critical secrets are set when running in production mode.
func (c *Config) ValidateProduction() error {
	var missing []string
	if c.TelegramBotToken.IsZero() {
		missing = append(missing, "TELEGRAM_BOT_TOKEN")
	}
	if c.HCloudToken.IsZero() {
		missing = append(missing, "HCLOUD_TOKEN")
	}
	if c.DBPassword.IsZero() {
		missing = append(missing, "DB_PASSWORD")
	}
	if c.CredentialKEK.IsZero() {
		missing = append(missing, "CREDENTIAL_KEK")
	}
	if len(missing) > 0 {
		return errors.New("missing required secrets in production: " + strings.Join(missing, ", "))
	}
	return nil
}

// Helpers
func getEnvString(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return defaultVal
}

func getEnvInt64(key string, defaultVal int64) int64 {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			return n
		}
	}
	return defaultVal
}

func getEnvStringSlice(key string, defaultVal []string) []string {
	if val := os.Getenv(key); val != "" {
		parts := strings.Split(val, ",")
		var out []string
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return defaultVal
}

func getEnvIntSlice(key string, defaultVal []int) []int {
	if val := os.Getenv(key); val != "" {
		parts := strings.Split(val, ",")
		var out []int
		for _, p := range parts {
			if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return defaultVal
}

// Duration helpers
func (c *Config) OrderExpiryDuration() time.Duration {
	return time.Duration(c.OrderExpiryMinutes) * time.Minute
}

func (c *Config) DeletionGraceDuration() time.Duration {
	return time.Duration(c.DeletionGraceMinutes) * time.Minute
}

func (c *Config) ReconcileIntervalDuration() time.Duration {
	return time.Duration(c.ReconcileIntervalMinutes) * time.Minute
}

func (c *Config) RateMaxAgeDuration() time.Duration {
	return time.Duration(c.RateMaxAgeHours) * time.Hour
}
