package wallet

import (
	"context"
	"errors"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// Repository handles wallet and ledger persistence.
type Repository interface {
	GetByUserID(ctx context.Context, userID int64) (*Wallet, error)
	RecordLedgerEntry(ctx context.Context, entry *LedgerEntry) error
	UpdateBalances(ctx context.Context, w *Wallet) error
	DebitAtomic(ctx context.Context, userID int64, totalIRT pricing.IRT, segment fee.Segment, sellRate pricing.IRT, kind LedgerKind, refType, refID string) (*Wallet, error)
}

// Currency represents currency code in ledger entries.
type Currency string

const (
	CurrencyIRT Currency = "IRT"
	CurrencyUSD Currency = "USD"
)

// LedgerKind classifies entries in wallet_ledger.
type LedgerKind string

const (
	KindTopupReceipt     LedgerKind = "topup_receipt"
	KindTopupUSDCrypto   LedgerKind = "topup_usd_crypto"
	KindCheckoutCredit   LedgerKind = "checkout_credit"
	KindPurchase         LedgerKind = "purchase"
	KindFee              LedgerKind = "fee"
	KindUsage            LedgerKind = "usage"
	KindUsageReversal    LedgerKind = "usage_reversal"
	KindRefundCredit     LedgerKind = "refund_credit"
	KindRefundPayout     LedgerKind = "refund_payout"
	KindRefundCommission LedgerKind = "refund_commission"
	KindAdminAdjustment  LedgerKind = "admin_adjustment"
)

var (
	ErrBelowFloor                 = errors.New("purchase would breach wallet floor")
	ErrCustomFloorBelowLowerBound = errors.New("custom floor cannot be lower than -1,000,000 Toman")
	ErrInvalidDebitAmount         = errors.New("debit amount must be positive")
)

const MinCustomFloor = pricing.IRT(-1_000_000)

// LedgerEntry represents an append-only row in the ledger.
type LedgerEntry struct {
	ID           string
	WalletID     int64
	Currency     Currency
	Amount       int64 // signed: IRT for IRT currency, MicroUSD for USD currency
	Kind         LedgerKind
	BalanceAfter int64
	RefType      string
	RefID        string
	CreatedAt    time.Time
}

// Wallet stores current cached balances and configuration.
type Wallet struct {
	ID               int64
	UserID           int64
	BalanceIRT       pricing.IRT
	BalanceUSDMicro  pricing.MicroUSD
	FloorOverrideIRT *pricing.IRT
	Version          int64
}

// CombinedValuation returns the total wallet value in IRT:
// balance_irt + floor(balance_usd_micro * sellRate / 1,000,000).
func (w *Wallet) CombinedValuation(sellRate pricing.IRT) pricing.IRT {
	if sellRate <= 0 {
		return w.BalanceIRT
	}
	usdInIRT := int64(w.BalanceUSDMicro) * int64(sellRate) / int64(1_000_000)
	return w.BalanceIRT + pricing.IRT(usdInIRT)
}

// EffectiveFloor determines floor for user's segment or custom override.
func (w *Wallet) EffectiveFloor(segment fee.Segment) pricing.IRT {
	if w.FloorOverrideIRT != nil {
		floor := *w.FloorOverrideIRT
		if floor < MinCustomFloor {
			return MinCustomFloor
		}
		return floor
	}

	switch segment {
	case fee.SegmentNew:
		return 0
	case fee.SegmentCustomer:
		return -100
	case fee.SegmentLoyal:
		return -500
	default:
		return 0
	}
}

// DebitSplit calculates amounts to debit from IRT and USD balances for a given total in IRT:
// Rule: Toman first, remaining from USD micro-units at sell rate (rounded up).
func DebitSplit(totalIRT pricing.IRT, balanceIRT pricing.IRT, sellRate pricing.IRT) (debitIRT pricing.IRT, debitUSD pricing.MicroUSD) {
	if totalIRT <= 0 {
		return 0, 0
	}

	// 1. Take from IRT balance first if positive
	if balanceIRT > 0 {
		if balanceIRT >= totalIRT {
			return totalIRT, 0
		}
		debitIRT = balanceIRT
	} else {
		debitIRT = 0
	}

	// 2. Remaining must be converted to MicroUSD at sellRate, rounded up
	remainingIRT := totalIRT - debitIRT
	if remainingIRT > 0 && sellRate > 0 {
		// ceil(remainingIRT * 1_000_000 / sellRate)
		num := int64(remainingIRT) * int64(1_000_000)
		den := int64(sellRate)
		usdCeil := (num + den - 1) / den
		debitUSD = pricing.MicroUSD(usdCeil)
	}

	return debitIRT, debitUSD
}

// CanPurchase checks whether a purchase debit of totalIRT is permitted given segment and sellRate.
// Purchases are refused if combined valuation would drop below effective floor.
func (w *Wallet) CanPurchase(totalIRT pricing.IRT, segment fee.Segment, sellRate pricing.IRT) bool {
	currentValuation := w.CombinedValuation(sellRate)
	postValuation := currentValuation - totalIRT
	floor := w.EffectiveFloor(segment)
	return postValuation >= floor
}

// RunwayHours calculates estimated hours of runway remaining given hourly cost in IRT.
func (w *Wallet) RunwayHours(hourlyCostIRT pricing.IRT, segment fee.Segment, sellRate pricing.IRT) int {
	if hourlyCostIRT <= 0 {
		return 999999
	}
	currentValuation := w.CombinedValuation(sellRate)
	floor := w.EffectiveFloor(segment)
	available := currentValuation - floor
	if available <= 0 {
		return 0
	}
	return int(available / hourlyCostIRT)
}
