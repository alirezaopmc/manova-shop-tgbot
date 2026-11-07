package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
)

// Store aggregates persistence repositories needed for billing daemon.
type Store interface {
	Users() user.Repository
	Wallets() wallet.Repository
	Billing() Repository
	FxRates() pricing.FxRateSource
}

// CloudProvider abstracts cloud infrastructure operations required for billing.
type CloudProvider interface {
	Shutdown(ctx context.Context, externalID string) error
	PowerOff(ctx context.Context, externalID string) error
}

// Notifier abstracts sending user and admin messages.
type Notifier interface {
	SendUserMessage(ctx context.Context, telegramUserID int64, text string) error
	SendAdminAlert(ctx context.Context, text string) error
}

// WorkerConfig holds configuration options for the recurring billing worker.
type WorkerConfig struct {
	TickInterval time.Duration
	SpreadIRT    pricing.IRT
	GracePeriod  time.Duration
	Logger       *slog.Logger
}

// Worker processes per-minute billing deductions and enforces runway shutdowns.
type Worker struct {
	store    Store
	cloud    CloudProvider
	notifier Notifier
	cfg      WorkerConfig
	mu       sync.Mutex
	breaches map[string]time.Time
}

// BillingWorker is an alias for Worker.
type BillingWorker = Worker

// NewWorker initializes a new billing daemon worker.
func NewWorker(store Store, cloud CloudProvider, notifier Notifier, cfg WorkerConfig) *Worker {
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = 60 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Worker{
		store:    store,
		cloud:    cloud,
		notifier: notifier,
		cfg:      cfg,
		breaches: make(map[string]time.Time),
	}
}

// Run executes the recurring billing loop until ctx is canceled.
func (w *Worker) Run(ctx context.Context) error {
	interval := w.cfg.TickInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.cfg.Logger.Info("billing daemon worker started", "tick_interval", interval)

	for {
		select {
		case <-ctx.Done():
			w.cfg.Logger.Info("billing daemon worker stopping", "reason", ctx.Err())
			return ctx.Err()
		case now := <-ticker.C:
			if err := w.ProcessMinuteTick(ctx, now); err != nil {
				w.cfg.Logger.Error("error processing billing tick", "error", err)
			}
		}
	}
}

// ProcessMinuteTick iterates over all active resources and accrues per-minute charges.
func (w *Worker) ProcessMinuteTick(ctx context.Context, now time.Time) error {
	resources, err := w.store.Billing().ListActiveResources(ctx)
	if err != nil {
		return fmt.Errorf("list active resources: %w", err)
	}

	for i := range resources {
		res := &resources[i]
		if res.Status != ResourceStatusActive {
			continue
		}

		if now.Before(res.BillingStartAt) {
			continue
		}

		elapsedMinutes := int64(now.Sub(res.BillingStartAt) / time.Minute)
		if elapsedMinutes <= res.PostedThroughMinute {
			continue
		}

		obs, err := w.store.FxRates().FetchRate(ctx)
		if err != nil {
			w.cfg.Logger.Error("failed to fetch fx rate for billing", "error", err, "resource_id", res.ID)
			continue
		}

		sellRate := pricing.CalculateSellRate(obs.MarketIRTPerUSD, w.cfg.SpreadIRT)
		if sellRate <= 0 {
			w.cfg.Logger.Error("invalid sell rate for billing", "market_rate", obs.MarketIRTPerUSD, "spread", w.cfg.SpreadIRT, "resource_id", res.ID)
			continue
		}

		var userSegment fee.Segment = fee.SegmentNew
		if w.store.Users() != nil {
			usr, err := w.store.Users().GetByID(ctx, res.UserID)
			if err != nil {
				w.cfg.Logger.Warn("could not fetch user for billing, using default segment", "user_id", res.UserID, "error", err)
			} else if usr != nil {
				userSegment = usr.Segment
			}
		}

		for m := res.PostedThroughMinute; m < elapsedMinutes; m++ {
			minuteIRT, minuteUSD := CalculateMinuteCharge(res.HourlyPriceUSD, sellRate, m)
			refID := fmt.Sprintf("min-%s-%d", res.ID, m)

			var debitErr error
			if minuteIRT > 0 {
				_, debitErr = w.store.Wallets().DebitAtomic(
					ctx,
					res.UserID,
					minuteIRT,
					userSegment,
					sellRate,
					wallet.KindUsage,
					"minute",
					refID,
				)
			}

			if debitErr != nil {
				if errors.Is(debitErr, wallet.ErrBelowFloor) ||
					strings.Contains(debitErr.Error(), "below floor") ||
					strings.Contains(debitErr.Error(), "insufficient") {
					w.handleDepletedBalance(ctx, res, now)
				} else {
					w.cfg.Logger.Error("failed to debit wallet for minute billing", "error", debitErr, "resource_id", res.ID, "user_id", res.UserID, "minute", m)
				}
				break
			}

			// Clear breach on successful debit
			w.mu.Lock()
			delete(w.breaches, res.ID)
			w.mu.Unlock()

			mp := &MinutePosting{
				ID:                fmt.Sprintf("mp-%s-%d", res.ID, m),
				ResourceID:        res.ID,
				MinuteIndex:       m,
				State:             "posted",
				USDMicro:          minuteUSD,
				AmountIRT:         minuteIRT,
				RemainderMicroIRT: 0,
				RateObsID:         obs.ID,
				LedgerID:          refID,
				CreatedAt:         now,
			}

			if err := w.store.Billing().SaveMinutePosting(ctx, mp); err != nil {
				w.cfg.Logger.Error("failed to save minute posting", "error", err, "resource_id", res.ID, "minute", m)
				break
			}

			res.PostedThroughMinute = m + 1
			res.LastVerifiedAt = now
			if err := w.store.Billing().SaveResource(ctx, res); err != nil {
				w.cfg.Logger.Error("failed to save updated resource after posting minute", "error", err, "resource_id", res.ID, "minute", m)
				break
			}
		}
	}

	return nil
}

func (w *Worker) handleDepletedBalance(ctx context.Context, res *ProvisionedResource, now time.Time) {
	w.mu.Lock()
	breachAt, exists := w.breaches[res.ID]
	if !exists {
		w.breaches[res.ID] = now
		breachAt = now
		if w.cfg.GracePeriod > 0 && w.notifier != nil {
			_ = w.notifier.SendUserMessage(ctx, res.UserID, fmt.Sprintf("⚠️ Wallet balance depleted for server %s. Please top up within %v to avoid shutdown.", res.ServerName, w.cfg.GracePeriod))
		}
	}
	w.mu.Unlock()

	graceExceeded := w.cfg.GracePeriod <= 0 || !now.Before(breachAt.Add(w.cfg.GracePeriod))
	if graceExceeded {
		if res.PowerState != PowerStateOff {
			if w.cloud != nil && res.ExternalID != "" {
				if err := w.cloud.Shutdown(ctx, res.ExternalID); err != nil {
					w.cfg.Logger.Warn("shutdown failed, attempting power off", "error", err, "external_id", res.ExternalID)
					if pErr := w.cloud.PowerOff(ctx, res.ExternalID); pErr != nil {
						w.cfg.Logger.Error("power off also failed", "error", pErr, "external_id", res.ExternalID)
					}
				}
			}
			res.PowerState = PowerStateOff
			res.LastVerifiedAt = now
			if err := w.store.Billing().SaveResource(ctx, res); err != nil {
				w.cfg.Logger.Error("failed to save resource power state off", "error", err, "resource_id", res.ID)
			}
			if w.notifier != nil {
				_ = w.notifier.SendUserMessage(ctx, res.UserID, fmt.Sprintf("⛔ Server %s has been shut down due to depleted wallet balance.", res.ServerName))
			}
		}
	}
}
