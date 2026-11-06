package billing

import (
	"testing"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

func TestMinuteChargeExactSumProperty(t *testing.T) {
	// Test case 1: Exact division (10,500 IRT per hour)
	// $0.10/hr at 105,000 sell rate -> 10,500 IRT
	hourlyUSD := pricing.MicroUSD(100_000)
	sellRate := pricing.IRT(105_000)
	expectedHourly := pricing.CalculatePriceIRT(hourlyUSD, sellRate) // 10,500

	var sumIRT pricing.IRT
	for m := int64(0); m < 60; m++ {
		minIRT, _ := CalculateMinuteCharge(hourlyUSD, sellRate, m)
		if minIRT != 175 {
			t.Fatalf("expected minute charge 175, got %d at min %d", minIRT, m)
		}
		sumIRT += minIRT
	}
	if sumIRT != expectedHourly {
		t.Fatalf("expected sum of 60 minutes to equal %d, got %d", expectedHourly, sumIRT)
	}

	// Test case 2: Non-zero remainder
	// Choose rate producing non-divisible hourly price
	// $0.095432/hr at 105,000 sell rate
	hourlyUSD2 := pricing.MicroUSD(95_432)
	expectedHourly2 := pricing.CalculatePriceIRT(hourlyUSD2, sellRate)

	var sumIRT2 pricing.IRT
	for m := int64(0); m < 60; m++ {
		minIRT, _ := CalculateMinuteCharge(hourlyUSD2, sellRate, m)
		sumIRT2 += minIRT
	}
	if sumIRT2 != expectedHourly2 {
		t.Fatalf("sum of minutes %d must exactly match expected hourly %d", sumIRT2, expectedHourly2)
	}
}

func TestInitialBlock(t *testing.T) {
	hourlyUSD := pricing.MicroUSD(100_000)
	sellRate := pricing.IRT(105_000)
	block := ComputeInitialBlock(hourlyUSD, sellRate)
	if block != 10_500 {
		t.Fatalf("expected initial block 10,500, got %d", block)
	}
}
