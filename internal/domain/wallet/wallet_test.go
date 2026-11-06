package wallet

import (
	"testing"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

func TestCombinedValuation(t *testing.T) {
	// Wallet with 10,000 IRT and 1,000,000 MicroUSD ($1.00)
	// Sell rate = 100,000 IRT / USD
	// Valuation = 10,000 + floor(1,000,000 * 100,000 / 1,000,000) = 110,000 IRT
	w := &Wallet{
		BalanceIRT:      10_000,
		BalanceUSDMicro: 1_000_000,
	}

	val := w.CombinedValuation(100_000)
	if val != 110_000 {
		t.Fatalf("expected 110,000 IRT, got %d", val)
	}

	// Floor rounding test: 1 MicroUSD ($0.000001) at 105,000 IRT/USD
	// 1 * 105,000 / 1,000,000 = 0.105 -> floor is 0 IRT
	w2 := &Wallet{
		BalanceIRT:      0,
		BalanceUSDMicro: 1,
	}
	val2 := w2.CombinedValuation(105_000)
	if val2 != 0 {
		t.Fatalf("expected floor rounding 0, got %d", val2)
	}
}

func TestEffectiveFloor(t *testing.T) {
	w := &Wallet{}

	if w.EffectiveFloor(fee.SegmentNew) != 0 {
		t.Fatalf("expected 0 for NEW")
	}
	if w.EffectiveFloor(fee.SegmentCustomer) != -100 {
		t.Fatalf("expected -100 for CUSTOMER")
	}
	if w.EffectiveFloor(fee.SegmentLoyal) != -500 {
		t.Fatalf("expected -500 for LOYAL")
	}

	// Custom override
	custom := pricing.IRT(-50_000)
	w.FloorOverrideIRT = &custom
	if w.EffectiveFloor(fee.SegmentNew) != -50_000 {
		t.Fatalf("expected custom -50,000, got %d", w.EffectiveFloor(fee.SegmentNew))
	}

	// Custom override lower bound check (-1,000,000 Toman)
	tooLow := pricing.IRT(-2_000_000)
	w.FloorOverrideIRT = &tooLow
	if w.EffectiveFloor(fee.SegmentNew) != -1_000_000 {
		t.Fatalf("expected clamped -1,000,000, got %d", w.EffectiveFloor(fee.SegmentNew))
	}
}

func TestDebitSplit(t *testing.T) {
	sellRate := pricing.IRT(100_000)

	// Case 1: IRT balance covers full amount (total = 5,000, balance = 10,000)
	debitIRT, debitUSD := DebitSplit(5_000, 10_000, sellRate)
	if debitIRT != 5_000 || debitUSD != 0 {
		t.Fatalf("expected 5,000 IRT and 0 USD, got %d IRT, %d USD", debitIRT, debitUSD)
	}

	// Case 2: IRT balance covers part, USD covers rest
	// total = 15,000 IRT, balance = 10,000 IRT, remaining = 5,000 IRT
	// USD at 100,000 IRT/USD: ceil(5,000 * 1,000,000 / 100,000) = 50,000 MicroUSD ($0.05)
	debitIRT, debitUSD = DebitSplit(15_000, 10_000, sellRate)
	if debitIRT != 10_000 || debitUSD != 50_000 {
		t.Fatalf("expected 10,000 IRT and 50,000 USD, got %d IRT, %d USD", debitIRT, debitUSD)
	}

	// Case 3: Zero IRT balance, fractional rounding up in MicroUSD
	// total = 10 IRT, remaining = 10 IRT, sellRate = 105,000
	// 10 * 1,000,000 / 105,000 = 95.238... MicroUSD -> ceil is 96 MicroUSD
	debitIRT, debitUSD = DebitSplit(10, 0, 105_000)
	if debitIRT != 0 || debitUSD != 96 {
		t.Fatalf("expected 0 IRT and 96 USD, got %d IRT, %d USD", debitIRT, debitUSD)
	}
}

func TestCanPurchase(t *testing.T) {
	w := &Wallet{
		BalanceIRT: 10_000,
	}

	// NEW user floor is 0
	if !w.CanPurchase(10_000, fee.SegmentNew, 100_000) {
		t.Fatalf("purchase bringing balance to 0 should be allowed")
	}
	if w.CanPurchase(10_001, fee.SegmentNew, 100_000) {
		t.Fatalf("purchase taking balance below 0 should be refused")
	}

	// CUSTOMER user floor is -100
	if !w.CanPurchase(10_100, fee.SegmentCustomer, 100_000) {
		t.Fatalf("purchase bringing balance to -100 should be allowed for CUSTOMER")
	}
	if w.CanPurchase(10_101, fee.SegmentCustomer, 100_000) {
		t.Fatalf("purchase taking balance below -100 should be refused for CUSTOMER")
	}
}

func TestRunwayHours(t *testing.T) {
	w := &Wallet{
		BalanceIRT: 100_000,
	}
	// hourly cost = 10,000 IRT -> 10 hours runway for NEW
	runway := w.RunwayHours(10_000, fee.SegmentNew, 100_000)
	if runway != 10 {
		t.Fatalf("expected 10 hours runway, got %d", runway)
	}
}
