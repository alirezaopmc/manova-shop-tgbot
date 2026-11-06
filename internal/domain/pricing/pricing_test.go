package pricing

import (
	"testing"
	"time"
)

func TestIRRToIRT(t *testing.T) {
	// Valid
	irt, err := IRRToIRT(105000)
	if err != nil || irt != 10500 {
		t.Fatalf("expected 10500 IRT, got %d, err %v", irt, err)
	}

	// Invalid: not multiple of 10
	_, err = IRRToIRT(105005)
	if err != ErrInvalidRialMultiple {
		t.Fatalf("expected ErrInvalidRialMultiple, got %v", err)
	}

	// Invalid: negative
	_, err = IRRToIRT(-10)
	if err != ErrNegativeAmount {
		t.Fatalf("expected ErrNegativeAmount, got %v", err)
	}
}

func TestPricingCalculations(t *testing.T) {
	// Example from Section 4.2:
	// item at $0.10 per hour is 100,000 micro-USD.
	// market rate 100,000, spread 5,000 -> sell rate 105,000.
	// price = 10,500 IRT, rial = 105,000 IRR.
	usd := MicroUSD(100_000)
	market := IRT(100_000)
	spread := IRT(5_000)

	now := time.Now()
	quote := NewQuote("q-1", usd, market, spread, "obs-1", now)

	if quote.SellRate != 105_000 {
		t.Fatalf("expected sell rate 105,000, got %d", quote.SellRate)
	}
	if quote.PriceIRT != 10_500 {
		t.Fatalf("expected price IRT 10,500, got %d", quote.PriceIRT)
	}
	if quote.PriceIRR != 105_000 {
		t.Fatalf("expected price IRR 105,000, got %d", quote.PriceIRR)
	}

	// Rounding up test:
	// 100 micro-USD ($0.000100) * 105,000 sell rate = 10.5 IRT -> ceil is 11 IRT
	ceilPrice := CalculatePriceIRT(100, 105_000)
	if ceilPrice != 11 {
		t.Fatalf("expected ceil price 11, got %d", ceilPrice)
	}
}

func TestRequoteCheck(t *testing.T) {
	quote := Quote{MarketRate: 100_000}
	threshold := IRT(5_000)

	// Difference exactly 5,000 -> false (not strictly greater)
	if quote.RequiresRequote(105_000, threshold) {
		t.Fatalf("expected false for exact threshold difference")
	}
	if quote.RequiresRequote(95_000, threshold) {
		t.Fatalf("expected false for exact negative threshold difference")
	}

	// Difference 5,001 -> true
	if !quote.RequiresRequote(105_001, threshold) {
		t.Fatalf("expected true for difference 5,001")
	}
	if !quote.RequiresRequote(94_999, threshold) {
		t.Fatalf("expected true for difference -5,001")
	}
}
