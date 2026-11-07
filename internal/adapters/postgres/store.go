package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/refund"
)

var (
	// ErrNotImplemented is returned by scaffolding stubs not yet implemented.
	ErrNotImplemented = errors.New("postgres: method not implemented")

	// ErrNotFound is returned when a requested record does not exist in the database.
	// Aliased to memory.ErrNotFound for cross-adapter compatibility.
	ErrNotFound = memory.ErrNotFound
)

// Compile-time assertions verifying all sub-repositories and the Store implement port interfaces.
var (
	_ port.Store               = (*Store)(nil)
	_ port.UserRepository      = (*userRepo)(nil)
	_ port.InviteRepository    = (*inviteRepo)(nil)
	_ port.WalletRepository    = (*walletRepo)(nil)
	_ port.OrderRepository     = (*orderRepo)(nil)
	_ port.ReceiptRepository   = (*receiptRepo)(nil)
	_ port.RefundRepository    = (*refundRepo)(nil)
	_ port.CatalogRepository   = (*catalogRepo)(nil)
	_ port.OSCatalogRepository = (*osCatalogRepo)(nil)
	_ port.BillingRepository   = (*billingRepo)(nil)
	_ port.FxRateSource        = (*fxRateRepo)(nil)
)

// Store provides a PostgreSQL-backed implementation of port.Store.
type Store struct {
	pool *pgxpool.Pool

	users         *userRepo
	invites       *inviteRepo
	wallets       *walletRepo
	orders        *orderRepo
	receipts      *receiptRepo
	refunds       *refundRepo
	catalog       *catalogRepo
	osCatalog     *osCatalogRepo
	billing       *billingRepo
	fxRates       *fxRateRepo
	conversations *ConversationStore

	mu         sync.RWMutex
	marketRate pricing.IRT
}

// NewStore constructs a Store backed by the provided pgx connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	s := &Store{
		pool:       pool,
		marketRate: 270_000, // Seed baseline fallback rate: 270,000 IRT / USD
	}
	s.users = &userRepo{s: s}
	s.invites = &inviteRepo{s: s}
	s.wallets = &walletRepo{s: s}
	s.orders = &orderRepo{s: s}
	s.receipts = &receiptRepo{s: s}
	s.refunds = &refundRepo{s: s}
	s.catalog = &catalogRepo{s: s}
	s.osCatalog = &osCatalogRepo{s: s}
	s.billing = &billingRepo{s: s}
	s.fxRates = &fxRateRepo{s: s}
	s.conversations = &ConversationStore{s: s}
	return s
}

// NewPool parses databaseURL and initializes a new pgxpool.Pool with verification ping.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse pool config: %w", err)
	}
	cfg.MaxConns = 15
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: init pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping pool: %w", err)
	}
	return pool, nil
}

// Open creates a new connection pool to databaseURL and returns an initialized Store.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := NewPool(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	return NewStore(pool), nil
}

// Close closes the underlying connection pool.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Pool returns the underlying pgx connection pool.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// SetMarketRate updates the dynamic market exchange rate.
func (s *Store) SetMarketRate(rate pricing.IRT) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marketRate = rate
}

// MarketRate returns the current dynamic market exchange rate.
func (s *Store) MarketRate() pricing.IRT {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.marketRate
}

// Sub-repository accessor methods

func (s *Store) Users() port.UserRepository          { return s.users }
func (s *Store) Invites() port.InviteRepository      { return s.invites }
func (s *Store) Wallets() port.WalletRepository      { return s.wallets }
func (s *Store) Orders() port.OrderRepository        { return s.orders }
func (s *Store) Receipts() port.ReceiptRepository    { return s.receipts }
func (s *Store) Refunds() port.RefundRepository      { return s.refunds }
func (s *Store) Catalog() port.CatalogRepository     { return s.catalog }
func (s *Store) OSCatalog() port.OSCatalogRepository { return s.osCatalog }
func (s *Store) Billing() port.BillingRepository     { return s.billing }
func (s *Store) FxRates() port.FxRateSource          { return s.fxRates }
func (s *Store) Conversations() *ConversationStore   { return s.conversations }

// --- Sub-repository scaffolding stubs ---

type refundRepo struct{ s *Store }

func (r *refundRepo) GetByID(_ context.Context, _ string) (*refund.Request, error) {
	return nil, ErrNotImplemented
}
func (r *refundRepo) Save(_ context.Context, _ *refund.Request) error {
	return ErrNotImplemented
}

type fxRateRepo struct{ s *Store }

func (r *fxRateRepo) FetchRate(_ context.Context) (pricing.RateObservation, error) {
	return pricing.RateObservation{
		ID:              "obs-postgres",
		MarketIRTPerUSD: r.s.MarketRate(),
		ObservedAt:      time.Now().UTC(),
		SourceRef:       "postgres-store",
	}, nil
}
