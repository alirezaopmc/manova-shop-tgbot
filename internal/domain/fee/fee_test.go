package fee

import (
	"testing"
)

func TestFeeLadder(t *testing.T) {
	// Worked example from Section 5.1:
	// P = 2,000,000 IRT and C = 1,500,000 IRT. Rate is 3%. fee = 60,000, total = 2,060,000 IRT.
	f, total := CalculateFee(2_000_000, 1_500_000)
	if f != 60_000 || total != 2_060_000 {
		t.Fatalf("expected fee 60,000 and total 2,060,000, got fee %d total %d", f, total)
	}

	// C = 16,000,000 IRT (1% bucket). P = 2,000,000. fee = 20,000.
	f, total = CalculateFee(2_000_000, 16_000_000)
	if f != 20_000 || total != 2_020_000 {
		t.Fatalf("expected fee 20,000, got %d", f)
	}

	// Boundary: C = 900,000 (10% bucket). P = 2,000,000 -> fee = 200,000.
	f, total = CalculateFee(2_000_000, 900_000)
	if f != 200_000 || total != 2_200_000 {
		t.Fatalf("expected fee 200,000, got %d", f)
	}

	// Exact boundaries:
	// C = 999,999 -> 10%
	if FeeRate(999_999) != 0.10 {
		t.Fatalf("expected 10%% at 999,999")
	}
	// C = 1,000,000 -> 3%
	if FeeRate(1_000_000) != 0.03 {
		t.Fatalf("expected 3%% at 1,000,000")
	}
	// C = 9,999,999 -> 3%
	if FeeRate(9_999_999) != 0.03 {
		t.Fatalf("expected 3%% at 9,999,999")
	}
	// C = 10,000,000 -> 1%
	if FeeRate(10_000_000) != 0.01 {
		t.Fatalf("expected 1%% at 10,000,000")
	}
}

func TestFirstPurchasePromotion(t *testing.T) {
	// Eligible buying hour at 10,500 IRT -> fee 0, total 10,500
	f, total, promoApplied, err := EvaluatePurchaseWithPromotion(10_500, 0, FirstPurchaseEligible)
	if err != nil || !promoApplied || f != 0 || total != 10_500 {
		t.Fatalf("expected promo applied with 0 fee, got fee %d total %d err %v", f, total, err)
	}

	// Eligible buying over hard limit (1,500,000 IRT) -> error
	_, _, _, err = EvaluatePurchaseWithPromotion(1_500_000, 0, FirstPurchaseEligible)
	if err != ErrFirstPurchaseHardLimitExceeded {
		t.Fatalf("expected ErrFirstPurchaseHardLimitExceeded, got %v", err)
	}

	// Eligible buying exactly at hard limit (1,000,000 IRT) -> allowed
	f, total, promoApplied, err = EvaluatePurchaseWithPromotion(1_000_000, 0, FirstPurchaseEligible)
	if err != nil || !promoApplied || f != 0 || total != 1_000_000 {
		t.Fatalf("expected promo allowed at hard limit, got %v", err)
	}

	// Forfeited promotion with C = 0 -> normal 10% fee
	// 10,500 * 10% = 1,050 fee, total 11,550
	f, total, promoApplied, err = EvaluatePurchaseWithPromotion(10_500, 0, FirstPurchaseForfeited)
	if err != nil || promoApplied || f != 1_050 || total != 11_550 {
		t.Fatalf("expected normal fee 1,050 and total 11,550, got fee %d total %d", f, total)
	}
}

func TestSegmentCalculation(t *testing.T) {
	if CalculateSegment(500_000) != SegmentNew {
		t.Fatalf("expected SegmentNew")
	}
	if CalculateSegment(1_000_000) != SegmentCustomer {
		t.Fatalf("expected SegmentCustomer")
	}
	if CalculateSegment(10_000_000) != SegmentLoyal {
		t.Fatalf("expected SegmentLoyal")
	}
}
