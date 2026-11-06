package catalog

import (
	"errors"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// ServerFamily classifies server tiers.
type ServerFamily string

const (
	FamilyCX  ServerFamily = "CX"  // Cost-Optimized
	FamilyCPX ServerFamily = "CPX" // Regular Performance
)

var (
	ErrRAMExceedsCap      = errors.New("RAM cannot exceed 16 GB")
	ErrInvalidHourlyPrice = errors.New("hourly price must be positive")
	ErrEmptyName          = errors.New("item name cannot be empty")
)

// Item represents a sellable Hetzner server configuration.
type Item struct {
	ID             string
	Name           string // e.g. "CX23"
	Family         ServerFamily
	VCPU           int
	RAMGB          int
	DiskGB         int
	HetznerType    string // API type name e.g. "cx23"
	HourlyUSDMicro pricing.MicroUSD
	MonthlyCapUSD  pricing.MicroUSD
	Active         bool
}

// Validate ensures catalog item invariants are satisfied.
func (i *Item) Validate(maxRAMGB int) error {
	if i.Name == "" {
		return ErrEmptyName
	}
	if i.HourlyUSDMicro <= 0 {
		return ErrInvalidHourlyPrice
	}
	if maxRAMGB > 0 && i.RAMGB > maxRAMGB {
		return ErrRAMExceedsCap
	}
	if i.RAMGB > 16 {
		return ErrRAMExceedsCap
	}
	return nil
}
