package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/osimage"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

func TestStoreInterfaceSatisfaction(t *testing.T) {
	// Verify that NewStore with a nil pool produces non-nil sub-repositories
	// and that each sub-repository satisfies its respective port interface.
	s := NewStore(nil)
	if s == nil {
		t.Fatal("expected non-nil Store from NewStore(nil)")
	}

	var _ port.Store = s

	if s.Users() == nil {
		t.Error("Users() returned nil")
	}
	var _ port.UserRepository = s.Users()

	if s.Invites() == nil {
		t.Error("Invites() returned nil")
	}
	var _ port.InviteRepository = s.Invites()

	if s.Wallets() == nil {
		t.Error("Wallets() returned nil")
	}
	var _ port.WalletRepository = s.Wallets()

	if s.Orders() == nil {
		t.Error("Orders() returned nil")
	}
	var _ port.OrderRepository = s.Orders()

	if s.Receipts() == nil {
		t.Error("Receipts() returned nil")
	}
	var _ port.ReceiptRepository = s.Receipts()

	if s.Refunds() == nil {
		t.Error("Refunds() returned nil")
	}
	var _ port.RefundRepository = s.Refunds()

	if s.Catalog() == nil {
		t.Error("Catalog() returned nil")
	}
	var _ port.CatalogRepository = s.Catalog()

	if s.OSCatalog() == nil {
		t.Error("OSCatalog() returned nil")
	}
	var _ port.OSCatalogRepository = s.OSCatalog()

	if s.Billing() == nil {
		t.Error("Billing() returned nil")
	}
	var _ port.BillingRepository = s.Billing()

	if s.FxRates() == nil {
		t.Error("FxRates() returned nil")
	}
	var _ port.FxRateSource = s.FxRates()
}

func TestStoreConnectionAndPool(t *testing.T) {
	s := openTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil Store from openTestStore")
	}

	pool := s.Pool()
	if pool == nil {
		t.Fatal("expected non-nil pgxpool.Pool from s.Pool()")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping test database through pool: %v", err)
	}

	// Verify Open helper connects cleanly
	dsn := getTestDatabaseURL(t)
	s2, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s2.Close()

	if err := s2.Pool().Ping(ctx); err != nil {
		t.Fatalf("failed to ping through s2 pool: %v", err)
	}
}

func TestStoreMarketRateAndFxRates(t *testing.T) {
	s := NewStore(nil)

	if s.MarketRate() != 270_000 {
		t.Fatalf("expected initial market rate 270_000, got %d", s.MarketRate())
	}

	var newRate pricing.IRT = 285_000
	s.SetMarketRate(newRate)
	if s.MarketRate() != newRate {
		t.Fatalf("expected updated market rate %d, got %d", newRate, s.MarketRate())
	}

	ctx := context.Background()
	obs, err := s.FxRates().FetchRate(ctx)
	if err != nil {
		t.Fatalf("FxRates().FetchRate failed: %v", err)
	}
	if obs.MarketIRTPerUSD != newRate {
		t.Fatalf("expected rate observation %d, got %d", newRate, obs.MarketIRTPerUSD)
	}
}

func TestSubRepositoryStubsReturnNotImplemented(t *testing.T) {
	s := NewStore(nil)
	ctx := context.Background()

	// Orders
	if _, err := s.Orders().GetByID(ctx, "ord"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Orders().GetByID, got %v", err)
	}
	if err := s.Orders().Save(ctx, nil, nil); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Orders().Save, got %v", err)
	}

	// Receipts
	if _, err := s.Receipts().GetByID(ctx, "rcp"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Receipts().GetByID, got %v", err)
	}
	if _, err := s.Receipts().FindByImageSHA256(ctx, "hash"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Receipts().FindByImageSHA256, got %v", err)
	}
	if _, err := s.Receipts().FindByTrackingCode(ctx, "track"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Receipts().FindByTrackingCode, got %v", err)
	}
	if err := s.Receipts().Save(ctx, nil); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Receipts().Save, got %v", err)
	}

	// Refunds
	if _, err := s.Refunds().GetByID(ctx, "ref"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Refunds().GetByID, got %v", err)
	}
	if err := s.Refunds().Save(ctx, nil); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Refunds().Save, got %v", err)
	}

	// Catalog
	if _, err := s.Catalog().GetItem(ctx, "item"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Catalog().GetItem, got %v", err)
	}
	if _, err := s.Catalog().ListActiveItems(ctx); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Catalog().ListActiveItems, got %v", err)
	}

	// OSCatalog
	if _, err := s.OSCatalog().GetOffered(ctx, osimage.DistroUbuntu, osimage.ArchX86); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from OSCatalog().GetOffered, got %v", err)
	}
	if err := s.OSCatalog().ReplaceSnapshot(ctx, nil, time.Now()); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from OSCatalog().ReplaceSnapshot, got %v", err)
	}

	// Billing
	if _, err := s.Billing().GetResource(ctx, "res"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Billing().GetResource, got %v", err)
	}
	if _, err := s.Billing().ListActiveResources(ctx); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Billing().ListActiveResources, got %v", err)
	}
	if _, err := s.Billing().ListUserResources(ctx, 1); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Billing().ListUserResources, got %v", err)
	}
	if err := s.Billing().SaveResource(ctx, nil); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Billing().SaveResource, got %v", err)
	}
	if err := s.Billing().SaveMinutePosting(ctx, nil); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Billing().SaveMinutePosting, got %v", err)
	}
	if err := s.Billing().UpdateResourceBillingEnd(ctx, "res", time.Now(), billing.ResourceStatusDeleted); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented from Billing().UpdateResourceBillingEnd, got %v", err)
	}
}
