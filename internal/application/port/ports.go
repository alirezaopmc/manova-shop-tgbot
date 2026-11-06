package port

import (
	"context"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/catalog"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/order"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/osimage"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/refund"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
)

// FxRateSource abstracts market exchange rate providers (SwapWallet, Wallex).
type FxRateSource interface {
	FetchRate(ctx context.Context) (pricing.RateObservation, error)
}

// ProviderServerStatus holds current server facts from cloud provider.
type ProviderServerStatus struct {
	ExternalID  string
	Status      string
	PowerState  billing.PowerState
	Created     time.Time
	IPv4Address string
	IPv6Address string
}

// ProviderCreateResult represents result of server creation call.
type ProviderCreateResult struct {
	ExternalID   string
	ActionID     int64
	RootPassword string // returned when no SSH key is supplied
}

// CloudProvider abstracts Hetzner Cloud infrastructure operations.
type CloudProvider interface {
	CreateServer(ctx context.Context, name string, serverType string, imageName string, location string, sshKeyIDs []string) (*ProviderCreateResult, error)
	GetServer(ctx context.Context, externalID string) (*ProviderServerStatus, error)
	DeleteServer(ctx context.Context, externalID string) error
	PowerOn(ctx context.Context, externalID string) error
	PowerOff(ctx context.Context, externalID string) error
	Shutdown(ctx context.Context, externalID string) error
	ResetPassword(ctx context.Context, externalID string) (string, error)
	EnableRescue(ctx context.Context, externalID string) (string, error)
	DisableRescue(ctx context.Context, externalID string) error
}

// ImageCatalogSource provides live OS images from cloud provider.
type ImageCatalogSource interface {
	ListSystemImages(ctx context.Context, arch osimage.Architecture) ([]osimage.Entry, error)
}

// TotpVerifier checks TOTP codes for sensitive admin operations.
type TotpVerifier interface {
	VerifyCode(ctx context.Context, adminID int64, code string) (bool, error)
}

// Notifier abstracts sending user and admin messages.
type Notifier interface {
	SendUserMessage(ctx context.Context, telegramUserID int64, text string) error
	SendAdminAlert(ctx context.Context, text string) error
}

// OrderRepository handles order persistence.
type OrderRepository interface {
	GetByID(ctx context.Context, orderID string) (*order.Order, error)
	Save(ctx context.Context, o *order.Order, event *order.OrderEvent) error
}

// WalletRepository handles wallet and ledger persistence.
type WalletRepository interface {
	GetByUserID(ctx context.Context, userID int64) (*wallet.Wallet, error)
	RecordLedgerEntry(ctx context.Context, entry *wallet.LedgerEntry) error
	UpdateBalances(ctx context.Context, w *wallet.Wallet) error
}

// ReceiptRepository handles payment receipt persistence.
type ReceiptRepository interface {
	GetByID(ctx context.Context, receiptID string) (*receipt.Receipt, error)
	FindByImageSHA256(ctx context.Context, hash string) (*receipt.Receipt, error)
	FindByTrackingCode(ctx context.Context, trackingCode string) (*receipt.Receipt, error)
	Save(ctx context.Context, r *receipt.Receipt) error
}

// RefundRepository handles refund request persistence.
type RefundRepository interface {
	GetByID(ctx context.Context, refundID string) (*refund.Request, error)
	Save(ctx context.Context, r *refund.Request) error
}

// CatalogRepository provides access to sellable items.
type CatalogRepository interface {
	GetItem(ctx context.Context, itemID string) (*catalog.Item, error)
	ListActiveItems(ctx context.Context) ([]catalog.Item, error)
}

// OSCatalogRepository handles cached OS image records.
type OSCatalogRepository interface {
	GetOffered(ctx context.Context, distro osimage.Distribution, arch osimage.Architecture) ([]osimage.Entry, error)
	ReplaceSnapshot(ctx context.Context, entries []osimage.Entry, syncedAt time.Time) error
}

// BillingRepository handles minute postings and provisioned resources.
type BillingRepository interface {
	GetResource(ctx context.Context, resourceID string) (*billing.ProvisionedResource, error)
	ListActiveResources(ctx context.Context) ([]billing.ProvisionedResource, error)
	SaveMinutePosting(ctx context.Context, p *billing.MinutePosting) error
	UpdateResourceBillingEnd(ctx context.Context, resourceID string, billingEnd time.Time, status billing.ResourceStatus) error
}
