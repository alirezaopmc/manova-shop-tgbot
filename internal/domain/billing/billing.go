package billing

import (
	"errors"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// PowerState represents the operational state reported by provider.
// Informational only — does not alter billing rates.
type PowerState string

const (
	PowerStateRunning PowerState = "running"
	PowerStateOff     PowerState = "off"
	PowerStateUnknown PowerState = "unknown"
)

// ResourceStatus tracks server lifecycle.
type ResourceStatus string

const (
	ResourceStatusActive   ResourceStatus = "active"
	ResourceStatusDeleting ResourceStatus = "deleting"
	ResourceStatusDeleted  ResourceStatus = "deleted"
)

var (
	ErrInvalidMinuteIndex = errors.New("minute index must be non-negative")
)

// MinutePosting represents the charge for a single minute of server existence.
type MinutePosting struct {
	ID                string
	ResourceID        string
	MinuteIndex       int64
	State             string // "posted" or "reversed"
	USDMicro          pricing.MicroUSD
	AmountIRT         pricing.IRT
	RemainderMicroIRT int64
	RateObsID         string
	LedgerID          string
	CreatedAt         time.Time
}

// ProvisionedResource represents a billed Hetzner Cloud server.
type ProvisionedResource struct {
	ID                  string
	OrderItemID         string
	ExternalID          string
	Status              ResourceStatus
	PowerState          PowerState
	HourlyPriceUSD      pricing.MicroUSD
	MonthlyCapUSD       pricing.MicroUSD
	BillingStartAt      time.Time
	BillingEndAt        *time.Time
	PostedThroughMinute int64
	RemainderMicroIRT   int64
	LastVerifiedAt      time.Time
	CreatedAt           time.Time
}

// CalculateMinuteCharge computes IRT charge for one minute at given hourly USD price and sell rate.
// Remainder is carried forward to ensure the sum of 60 minutes exactly equals the hourly charge.
// Hourly rate in micro-USD:
// Total hourly IRT = ceil(hourlyUSD * sellRate / 1,000,000)
// Base minute = Total hourly IRT / 60
// Remainder per hour = Total hourly IRT % 60
// In each hour (minuteIndex % 60): if minuteIndex % 60 < remainder, add 1 IRT.
func CalculateMinuteCharge(
	hourlyUSD pricing.MicroUSD,
	sellRate pricing.IRT,
	minuteIndex int64,
) (minuteIRT pricing.IRT, minuteUSD pricing.MicroUSD) {
	if hourlyUSD <= 0 || sellRate <= 0 || minuteIndex < 0 {
		return 0, 0
	}

	hourlyIRT := pricing.CalculatePriceIRT(hourlyUSD, sellRate)
	baseMinuteIRT := hourlyIRT / 60
	remIRT := hourlyIRT % 60

	minInHour := minuteIndex % 60
	if minInHour < int64(remIRT) {
		minuteIRT = baseMinuteIRT + 1
	} else {
		minuteIRT = baseMinuteIRT
	}

	minuteUSD = hourlyUSD / 60
	return minuteIRT, minuteUSD
}

// ComputeInitialBlock calculates initial prepaid block (first 60 minutes) at purchase.
func ComputeInitialBlock(hourlyUSD pricing.MicroUSD, sellRate pricing.IRT) pricing.IRT {
	return pricing.CalculatePriceIRT(hourlyUSD, sellRate)
}
