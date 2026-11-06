package fee

import (
	"errors"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// Segment defines user classification based on 30-day rolling purchases.
type Segment string

const (
	SegmentNew      Segment = "NEW"
	SegmentCustomer Segment = "CUSTOMER"
	SegmentLoyal    Segment = "LOYAL"
)

// FirstPurchaseState tracks promotion status.
type FirstPurchaseState string

const (
	FirstPurchaseEligible  FirstPurchaseState = "eligible"
	FirstPurchaseConsumed  FirstPurchaseState = "consumed"
	FirstPurchaseForfeited FirstPurchaseState = "forfeited"
)

const (
	FirstPurchaseHardLimit = pricing.IRT(1_000_000)
	CustomerThreshold      = pricing.IRT(1_000_000)
	LoyalThreshold         = pricing.IRT(10_000_000)
)

var (
	ErrFirstPurchaseHardLimitExceeded = errors.New("first purchase exceeds 1,000,000 Toman hard limit")
)

// CalculateSegment determines segment from rolling 30-day purchase total C.
func CalculateSegment(rolling30DaySpend pricing.IRT) Segment {
	if rolling30DaySpend < CustomerThreshold {
		return SegmentNew
	}
	if rolling30DaySpend < LoyalThreshold {
		return SegmentCustomer
	}
	return SegmentLoyal
}

// FeeRate returns percentage rate for spend C: 10%, 3%, or 1%.
func FeeRate(rolling30DaySpend pricing.IRT) float64 {
	if rolling30DaySpend < CustomerThreshold {
		return 0.10
	}
	if rolling30DaySpend < LoyalThreshold {
		return 0.03
	}
	return 0.01
}

// CalculateFee computes fee and total for a purchase P with rolling spend C.
// fee = ceil(P * f(C))
func CalculateFee(purchaseAmount pricing.IRT, rolling30DaySpend pricing.IRT) (fee pricing.IRT, total pricing.IRT) {
	if purchaseAmount <= 0 {
		return 0, 0
	}
	rate := FeeRate(rolling30DaySpend)
	// integer ceil arithmetic:
	// rate 0.10: (P * 10 + 99) / 100
	// rate 0.03: (P * 3 + 99) / 100
	// rate 0.01: (P * 1 + 99) / 100
	var percent int64
	switch rate {
	case 0.10:
		percent = 10
	case 0.03:
		percent = 3
	default:
		percent = 1
	}

	num := int64(purchaseAmount) * percent
	den := int64(100)
	feeVal := (num + den - 1) / den
	fee = pricing.IRT(feeVal)
	total = purchaseAmount + fee
	return fee, total
}

// EvaluatePurchaseWithPromotion checks promotion eligibility and calculates final fee and total.
func EvaluatePurchaseWithPromotion(
	purchaseAmount pricing.IRT,
	rolling30DaySpend pricing.IRT,
	promoState FirstPurchaseState,
) (fee pricing.IRT, total pricing.IRT, promoApplied bool, err error) {
	if promoState == FirstPurchaseEligible {
		if purchaseAmount > FirstPurchaseHardLimit {
			return 0, 0, false, ErrFirstPurchaseHardLimitExceeded
		}
		// First purchase promotion: fee is 0
		return 0, purchaseAmount, true, nil
	}

	// Normal fee ladder
	fee, total = CalculateFee(purchaseAmount, rolling30DaySpend)
	return fee, total, false, nil
}
