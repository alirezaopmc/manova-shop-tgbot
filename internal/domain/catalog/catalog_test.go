package catalog

import (
	"testing"
)

func TestCatalogItemValidation(t *testing.T) {
	valid := &Item{
		Name:           "CX23",
		Family:         FamilyCX,
		VCPU:           2,
		RAMGB:          4,
		DiskGB:         40,
		HetznerType:    "cx23",
		HourlyUSDMicro: 100_000,
	}

	if err := valid.Validate(16); err != nil {
		t.Fatalf("expected valid item, got %v", err)
	}

	tooBig := &Item{
		Name:           "BigServer",
		RAMGB:          32,
		HourlyUSDMicro: 100_000,
	}
	if err := tooBig.Validate(16); err != ErrRAMExceedsCap {
		t.Fatalf("expected ErrRAMExceedsCap, got %v", err)
	}

	noPrice := &Item{
		Name:           "FreeServer",
		RAMGB:          4,
		HourlyUSDMicro: 0,
	}
	if err := noPrice.Validate(16); err != ErrInvalidHourlyPrice {
		t.Fatalf("expected ErrInvalidHourlyPrice, got %v", err)
	}
}
