package pricing

import (
	"context"
	"errors"
	"time"
)

// FxRateSource abstracts market exchange rate providers (SwapWallet, Wallex).
type FxRateSource interface {
	FetchRate(ctx context.Context) (RateObservation, error)
}

// MicroUSD represents money in integer micro-dollars ($1 = 1,000,000 micro-USD).
type MicroUSD int64

// IRT represents money in Iranian Toman (integer).
type IRT int64

// IRR represents money in Iranian Rial (1 IRT = 10 IRR).
type IRR int64

var (
	ErrInvalidRialMultiple = errors.New("rial amount must be an exact multiple of 10")
	ErrNegativeAmount      = errors.New("amount must not be negative")
)

// IRRToIRT converts rial to toman, rejecting amounts not multiple of 10.
func IRRToIRT(irr IRR) (IRT, error) {
	if irr < 0 {
		return 0, ErrNegativeAmount
	}
	if irr%10 != 0 {
		return 0, ErrInvalidRialMultiple
	}
	return IRT(irr / 10), nil
}

// IRTToIRR converts toman to rial.
func IRTToIRR(irt IRT) IRR {
	return IRR(irt * 10)
}

// RateObservation holds a market rate observation.
type RateObservation struct {
	ID              string
	MarketIRTPerUSD IRT
	ObservedAt      time.Time
	SourceRef       string
}

// CalculateSellRate computes sell rate adding spread to market rate: S = M + spread.
func CalculateSellRate(marketRate IRT, spread IRT) IRT {
	return marketRate + spread
}

// CalculatePriceIRT computes IRT price for a given USD micro amount and sell rate:
// price_irt = ceil(usdMicro * sellRate / 1,000,000).
func CalculatePriceIRT(usd MicroUSD, sellRate IRT) IRT {
	if usd <= 0 || sellRate <= 0 {
		return 0
	}
	// Use math.Ceil with large precision integer division
	// Numerator = usd * sellRate
	// To avoid float inaccuracy while doing ceiling integer division:
	// ceil(a / b) for positive ints = (a + b - 1) / b
	num := int64(usd) * int64(sellRate)
	den := int64(1_000_000)
	price := (num + den - 1) / den
	return IRT(price)
}

// Quote stores locked price parameters for an order.
type Quote struct {
	ID            string
	USDBaseMicro  MicroUSD
	MarketRate    IRT
	SellRate      IRT
	PriceIRT      IRT
	PriceIRR      IRR
	ObservationID string
	CreatedAt     time.Time
}

// NewQuote creates a new Quote locking pricing calculations.
func NewQuote(id string, usd MicroUSD, marketRate IRT, spread IRT, obsID string, now time.Time) Quote {
	sellRate := CalculateSellRate(marketRate, spread)
	priceIRT := CalculatePriceIRT(usd, sellRate)
	priceIRR := IRTToIRR(priceIRT)

	return Quote{
		ID:            id,
		USDBaseMicro:  usd,
		MarketRate:    marketRate,
		SellRate:      sellRate,
		PriceIRT:      priceIRT,
		PriceIRR:      priceIRR,
		ObservationID: obsID,
		CreatedAt:     now,
	}
}

// RequiresRequote returns true if market rate moved more than threshold from the quoted rate.
func (q Quote) RequiresRequote(currentMarketRate IRT, threshold IRT) bool {
	diff := int64(currentMarketRate - q.MarketRate)
	if diff < 0 {
		diff = -diff
	}
	return diff > int64(threshold)
}

// HourlyToMonthlyCap estimates monthly cap at gross rate (720 hours cap basis or Hetzner cap).
func HourlyToMonthlyCap(hourly MicroUSD, monthlyCap MicroUSD, sellRate IRT) IRT {
	// If Hetzner has a dedicated monthly cap, calculate price from cap
	if monthlyCap > 0 {
		return CalculatePriceIRT(monthlyCap, sellRate)
	}
	// Default 720 hours
	return CalculatePriceIRT(hourly*720, sellRate)
}
