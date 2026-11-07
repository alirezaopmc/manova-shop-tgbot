package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/catalog"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/order"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/osimage"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/refund"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
)

var (
	ErrNotFound = errors.New("record not found")
)

// Store provides a thread-safe in-memory persistence implementation.
type Store struct {
	mu sync.RWMutex

	users      map[int64]*user.User
	phoneIndex map[string]int64

	invites map[string]*invite.Invite

	wallets map[int64]*wallet.Wallet
	ledger  []*wallet.LedgerEntry

	orders      map[string]*order.Order
	orderEvents []*order.OrderEvent

	receipts       map[string]*receipt.Receipt
	receiptByHash  map[string]string
	receiptByTrack map[string]string

	refunds map[string]*refund.Request

	catalogItems map[string]*catalog.Item

	osCatalog []osimage.Entry

	resources map[string]*billing.ProvisionedResource
	postings  []*billing.MinutePosting

	marketRate pricing.IRT
}

func NewStore() *Store {
	s := &Store{
		users:          make(map[int64]*user.User),
		phoneIndex:     make(map[string]int64),
		invites:        make(map[string]*invite.Invite),
		wallets:        make(map[int64]*wallet.Wallet),
		orders:         make(map[string]*order.Order),
		receipts:       make(map[string]*receipt.Receipt),
		receiptByHash:  make(map[string]string),
		receiptByTrack: make(map[string]string),
		refunds:        make(map[string]*refund.Request),
		catalogItems:   make(map[string]*catalog.Item),
		resources:      make(map[string]*billing.ProvisionedResource),
		marketRate:     270_000, // Seed baseline market rate 270,000 IRT / USD (updated dynamically by fxrate.Poller)
	}

	// Seed default Hetzner CX catalog items
	s.catalogItems["cx23"] = &catalog.Item{
		ID:             "cx23",
		Name:           "CX23",
		Family:         catalog.FamilyCX,
		VCPU:           2,
		RAMGB:          4,
		DiskGB:         40,
		HetznerType:    "cx23",
		HourlyUSDMicro: 7000,    // ~$0.0070/hr
		MonthlyCapUSD:  4490000, // $4.49/mo
		Active:         true,
	}
	s.catalogItems["cx33"] = &catalog.Item{
		ID:             "cx33",
		Name:           "CX33",
		Family:         catalog.FamilyCX,
		VCPU:           4,
		RAMGB:          8,
		DiskGB:         80,
		HetznerType:    "cx33",
		HourlyUSDMicro: 13000,   // ~$0.0130/hr
		MonthlyCapUSD:  8490000, // $8.49/mo
		Active:         true,
	}
	s.catalogItems["cx43"] = &catalog.Item{
		ID:             "cx43",
		Name:           "CX43",
		Family:         catalog.FamilyCX,
		VCPU:           8,
		RAMGB:          16,
		DiskGB:         160,
		HetznerType:    "cx43",
		HourlyUSDMicro: 25000,    // ~$0.0250/hr
		MonthlyCapUSD:  15990000, // $15.99/mo
		Active:         true,
	}

	// Seed OS catalog
	now := time.Now().UTC()
	s.osCatalog = []osimage.Entry{
		{
			ID:            "ubuntu-2404",
			Distribution:  osimage.DistroUbuntu,
			Version:       "24.04",
			Architecture:  osimage.ArchX86,
			ImageName:     "ubuntu-24.04",
			HetznerStatus: osimage.StatusAvailable,
			DisplayNameFA: "اوبونتو ۲۴.۰۴ (Ubuntu 24.04 LTS)",
			DisplayNameEN: "Ubuntu 24.04 LTS",
			SortOrder:     1,
			SyncedAt:      now,
		},
		{
			ID:            "ubuntu-2204",
			Distribution:  osimage.DistroUbuntu,
			Version:       "22.04",
			Architecture:  osimage.ArchX86,
			ImageName:     "ubuntu-22.04",
			HetznerStatus: osimage.StatusAvailable,
			DisplayNameFA: "اوبونتو ۲۲.۰۴ (Ubuntu 22.04 LTS)",
			DisplayNameEN: "Ubuntu 22.04 LTS",
			SortOrder:     2,
			SyncedAt:      now,
		},
		{
			ID:            "debian-12",
			Distribution:  osimage.DistroDebian,
			Version:       "12",
			Architecture:  osimage.ArchX86,
			ImageName:     "debian-12",
			HetznerStatus: osimage.StatusAvailable,
			DisplayNameFA: "دبیان ۱۲ (Debian 12 Bookworm)",
			DisplayNameEN: "Debian 12 Bookworm",
			SortOrder:     3,
			SyncedAt:      now,
		},
	}

	return s
}

// Sub-repository accessors
var _ port.Store = (*Store)(nil)

func (s *Store) Users() port.UserRepository          { return &userRepo{s} }
func (s *Store) Invites() port.InviteRepository      { return &inviteRepo{s} }
func (s *Store) Wallets() port.WalletRepository      { return &walletRepo{s} }
func (s *Store) Orders() port.OrderRepository        { return &orderRepo{s} }
func (s *Store) Receipts() port.ReceiptRepository    { return &receiptRepo{s} }
func (s *Store) Refunds() port.RefundRepository      { return &refundRepo{s} }
func (s *Store) Catalog() port.CatalogRepository     { return &catalogRepo{s} }
func (s *Store) OSCatalog() port.OSCatalogRepository { return &osCatalogRepo{s} }
func (s *Store) Billing() port.BillingRepository     { return &billingRepo{s} }
func (s *Store) FxRates() port.FxRateSource          { return &fxRateRepo{s} }

// --- FxRateRepo ---

type fxRateRepo struct{ s *Store }

func (r *fxRateRepo) FetchRate(_ context.Context) (pricing.RateObservation, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return pricing.RateObservation{
		ID:              "obs-mem",
		MarketIRTPerUSD: r.s.marketRate,
		ObservedAt:      time.Now().UTC(),
		SourceRef:       "memory-seed",
	}, nil
}

func (s *Store) SetMarketRate(rate pricing.IRT) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marketRate = rate
}

// --- UserRepo ---

type userRepo struct{ s *Store }

func (r *userRepo) GetByID(_ context.Context, userID int64) (*user.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	u, ok := r.s.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *userRepo) FindByPhoneHash(_ context.Context, phoneHash string) (*user.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	uid, ok := r.s.phoneIndex[phoneHash]
	if !ok {
		return nil, ErrNotFound
	}
	u := r.s.users[uid]
	cp := *u
	return &cp, nil
}

func (r *userRepo) Save(_ context.Context, u *user.User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *u
	r.s.users[u.TelegramUserID] = &cp
	if u.PhoneHash != nil {
		r.s.phoneIndex[*u.PhoneHash] = u.TelegramUserID
	} else {
		for h, uid := range r.s.phoneIndex {
			if uid == u.TelegramUserID {
				delete(r.s.phoneIndex, h)
			}
		}
	}
	return nil
}

func (r *userRepo) Count(_ context.Context) (int64, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return int64(len(r.s.users)), nil
}

func (r *userRepo) ListUsers(_ context.Context, limit int) ([]user.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []user.User
	for _, u := range r.s.users {
		out = append(out, *u)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// --- InviteRepo ---

type inviteRepo struct{ s *Store }

func (r *inviteRepo) Create(_ context.Context, inv *invite.Invite) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *inv
	r.s.invites[inv.Code] = &cp
	return nil
}

func (r *inviteRepo) GetByCode(_ context.Context, code string) (*invite.Invite, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	inv, ok := r.s.invites[code]
	if !ok {
		return nil, invite.ErrInviteNotFound
	}
	cp := *inv
	return &cp, nil
}

func (r *inviteRepo) Claim(_ context.Context, code string, userID int64, now time.Time) (*invite.Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	inv, ok := r.s.invites[code]
	if !ok {
		return nil, invite.ErrInviteNotFound
	}
	if err := inv.Claim(userID, now); err != nil {
		return nil, err
	}
	cp := *inv
	return &cp, nil
}

func (r *inviteRepo) ListByCreator(_ context.Context, adminID int64) ([]invite.Invite, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []invite.Invite
	for _, inv := range r.s.invites {
		if inv.CreatedBy == adminID {
			out = append(out, *inv)
		}
	}
	return out, nil
}

// --- WalletRepo ---

type walletRepo struct{ s *Store }

func (r *walletRepo) GetByUserID(_ context.Context, userID int64) (*wallet.Wallet, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	w, ok := r.s.wallets[userID]
	if !ok {
		w = &wallet.Wallet{
			ID:              userID,
			UserID:          userID,
			BalanceIRT:      0,
			BalanceUSDMicro: 0,
			Version:         1,
		}
		r.s.wallets[userID] = w
	}
	cp := *w
	return &cp, nil
}

func (r *walletRepo) RecordLedgerEntry(_ context.Context, entry *wallet.LedgerEntry) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *entry
	r.s.ledger = append(r.s.ledger, &cp)
	return nil
}

// LedgerEntries returns a snapshot of all recorded ledger entries.
func (s *Store) LedgerEntries() []wallet.LedgerEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]wallet.LedgerEntry, len(s.ledger))
	for i, e := range s.ledger {
		out[i] = *e
	}
	return out
}

func (r *walletRepo) UpdateBalances(_ context.Context, w *wallet.Wallet) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *w
	cp.Version++
	r.s.wallets[w.UserID] = &cp
	return nil
}

// --- OrderRepo ---

type orderRepo struct{ s *Store }

func (r *orderRepo) GetByID(_ context.Context, orderID string) (*order.Order, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	o, ok := r.s.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *o
	return &cp, nil
}

func (r *orderRepo) Save(_ context.Context, o *order.Order, event *order.OrderEvent) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *o
	r.s.orders[o.ID] = &cp
	if event != nil {
		e := *event
		r.s.orderEvents = append(r.s.orderEvents, &e)
	}
	return nil
}

// --- ReceiptRepo ---

type receiptRepo struct{ s *Store }

func (r *receiptRepo) GetByID(_ context.Context, receiptID string) (*receipt.Receipt, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	rcp, ok := r.s.receipts[receiptID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *rcp
	return &cp, nil
}

func (r *receiptRepo) FindByImageSHA256(_ context.Context, hash string) (*receipt.Receipt, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	id, ok := r.s.receiptByHash[hash]
	if !ok {
		return nil, ErrNotFound
	}
	return r.s.receipts[id], nil
}

func (r *receiptRepo) FindByTrackingCode(_ context.Context, trackingCode string) (*receipt.Receipt, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	id, ok := r.s.receiptByTrack[trackingCode]
	if !ok {
		return nil, ErrNotFound
	}
	return r.s.receipts[id], nil
}

func (r *receiptRepo) Save(_ context.Context, rcp *receipt.Receipt) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *rcp
	r.s.receipts[rcp.ID] = &cp
	r.s.receiptByHash[rcp.ImageSHA256] = rcp.ID
	r.s.receiptByTrack[rcp.TrackingCodeNorm] = rcp.ID
	return nil
}

// --- RefundRepo ---

type refundRepo struct{ s *Store }

func (r *refundRepo) GetByID(_ context.Context, refundID string) (*refund.Request, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	ref, ok := r.s.refunds[refundID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *ref
	return &cp, nil
}

func (r *refundRepo) Save(_ context.Context, ref *refund.Request) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *ref
	r.s.refunds[ref.ID] = &cp
	return nil
}

// --- CatalogRepo ---

type catalogRepo struct{ s *Store }

func (r *catalogRepo) GetItem(_ context.Context, itemID string) (*catalog.Item, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	it, ok := r.s.catalogItems[itemID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *it
	return &cp, nil
}

func (r *catalogRepo) ListActiveItems(_ context.Context) ([]catalog.Item, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []catalog.Item
	for _, it := range r.s.catalogItems {
		if it.Active {
			out = append(out, *it)
		}
	}
	return out, nil
}

// --- OSCatalogRepo ---

type osCatalogRepo struct{ s *Store }

func (r *osCatalogRepo) GetOffered(_ context.Context, distro osimage.Distribution, arch osimage.Architecture) ([]osimage.Entry, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []osimage.Entry
	now := time.Now().UTC()
	for _, e := range r.s.osCatalog {
		if (distro == "" || e.Distribution == distro) && (arch == "" || e.Architecture == arch) {
			if e.IsOffered(now) {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

func (r *osCatalogRepo) ReplaceSnapshot(_ context.Context, entries []osimage.Entry, _ time.Time) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.osCatalog = append([]osimage.Entry(nil), entries...)
	return nil
}

// --- BillingRepo ---

type billingRepo struct{ s *Store }

func (r *billingRepo) GetResource(_ context.Context, resourceID string) (*billing.ProvisionedResource, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	res, ok := r.s.resources[resourceID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *res
	return &cp, nil
}

func (r *billingRepo) ListActiveResources(_ context.Context) ([]billing.ProvisionedResource, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []billing.ProvisionedResource
	for _, res := range r.s.resources {
		if res.Status == billing.ResourceStatusActive {
			out = append(out, *res)
		}
	}
	return out, nil
}

func (r *billingRepo) ListUserResources(_ context.Context, userID int64) ([]billing.ProvisionedResource, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []billing.ProvisionedResource
	for _, res := range r.s.resources {
		if res.UserID == userID && res.Status == billing.ResourceStatusActive {
			out = append(out, *res)
		}
	}
	return out, nil
}

func (r *billingRepo) SaveResource(_ context.Context, res *billing.ProvisionedResource) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *res
	r.s.resources[res.ID] = &cp
	return nil
}

func (r *billingRepo) SaveMinutePosting(_ context.Context, p *billing.MinutePosting) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *p
	r.s.postings = append(r.s.postings, &cp)
	return nil
}

func (r *billingRepo) UpdateResourceBillingEnd(_ context.Context, resourceID string, billingEnd time.Time, status billing.ResourceStatus) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	res, ok := r.s.resources[resourceID]
	if !ok {
		return ErrNotFound
	}
	res.BillingEndAt = &billingEnd
	res.Status = status
	return nil
}

// Verify interface implementations
var (
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
