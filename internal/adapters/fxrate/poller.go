package fxrate

import (
	"context"
	"log/slog"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// RateSetter represents any entity that can store the updated market exchange rate.
type RateSetter interface {
	SetMarketRate(rate pricing.IRT)
}

// Poller periodically fetches USD/IRT exchange rate and updates the store.
type Poller struct {
	setter   RateSetter
	source   port.FxRateSource
	interval time.Duration
	logger   *slog.Logger
}

// NewPoller creates a new periodic rate poller.
func NewPoller(setter RateSetter, source port.FxRateSource, interval time.Duration, logger *slog.Logger) *Poller {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		setter:   setter,
		source:   source,
		interval: interval,
		logger:   logger,
	}
}

// Start runs the periodic poller until the context is canceled.
// It executes an initial fetch immediately upon starting.
func (p *Poller) Start(ctx context.Context) {
	p.logger.Info("starting fx rate poller", "interval", p.interval.String())

	// Immediate initial fetch
	p.poll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("fx rate poller stopping")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	obs, err := p.source.FetchRate(fetchCtx)
	if err != nil {
		p.logger.Warn("fx rate poll failed; retaining previous rate", "error", err)
		return
	}

	p.setter.SetMarketRate(obs.MarketIRTPerUSD)
	p.logger.Info("fx rate updated successfully",
		"rate_irt", obs.MarketIRTPerUSD,
		"source", obs.SourceRef,
		"observed_at", obs.ObservedAt.Format(time.RFC3339),
	)
}
