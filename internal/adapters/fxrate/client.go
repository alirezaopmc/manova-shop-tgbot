package fxrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

var (
	ErrInvalidResponse = errors.New("invalid fx rate provider response")
	ErrRateOutOfRange  = errors.New("fx rate is out of safe acceptable range")
	ErrRateQuarantined = errors.New("fx rate jump quarantined awaiting confirmation")
	ErrAllSourcesDown  = errors.New("all fx rate sources failed")
)

const (
	DefaultMinRateIRT  pricing.IRT = 100_000
	DefaultMaxRateIRT  pricing.IRT = 2_000_000
	DefaultMaxJumpPct  float64     = 0.03 // 3%
	DefaultHTTPTimeout             = 5 * time.Second
	UserAgentHeader                = "manova-shop-tgbot/1.0 (+https://internal.manova.space)"
)

// ClientConfig holds configuration for fx rate fetching.
type ClientConfig struct {
	SwapWalletURL string
	WallexURL     string
	Timeout       time.Duration
	MinRateIRT    pricing.IRT
	MaxRateIRT    pricing.IRT
	MaxJumpPct    float64
	HTTPClient    *http.Client
}

// Client implements port.FxRateSource by querying SwapWallet with Wallex fallback.
type Client struct {
	cfg        ClientConfig
	httpClient *http.Client

	mu               sync.Mutex
	swapFailures     int
	swapSuccesses    int
	activeSource     string // "swapwallet" or "wallex"
	lastAcceptedRate pricing.IRT
	quarantinedRate  pricing.IRT
	logger           *slog.Logger
}

// NewClient creates a new rate client.
func NewClient(cfg ClientConfig, logger *slog.Logger) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultHTTPTimeout
	}
	if cfg.MinRateIRT <= 0 {
		cfg.MinRateIRT = DefaultMinRateIRT
	}
	if cfg.MaxRateIRT <= 0 {
		cfg.MaxRateIRT = DefaultMaxRateIRT
	}
	if cfg.MaxJumpPct <= 0 {
		cfg.MaxJumpPct = DefaultMaxJumpPct
	}
	if cfg.SwapWalletURL == "" {
		cfg.SwapWalletURL = "https://swapwallet.app/api/v1/market/prices?pair=USDT/IRT"
	}
	if cfg.WallexURL == "" {
		cfg.WallexURL = "https://api.wallex.ir/v1/markets"
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.Timeout,
		}
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		cfg:          cfg,
		httpClient:   httpClient,
		activeSource: "swapwallet",
		logger:       logger,
	}
}

// FetchRate fetches the current market USD/IRT rate per project specification.
func (c *Client) FetchRate(ctx context.Context) (pricing.RateObservation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var obs pricing.RateObservation
	var err error

	if c.activeSource == "swapwallet" {
		obs, err = c.fetchSwapWallet(ctx)
		if err != nil {
			c.swapFailures++
			c.swapSuccesses = 0
			c.logger.Warn("swapwallet rate poll failed", "consecutive_failures", c.swapFailures, "error", err)

			if c.swapFailures >= 3 {
				c.logger.Warn("switching primary fx rate source to wallex fallback")
				c.activeSource = "wallex"
			}

			// Attempt immediate fallback to Wallex
			wallexObs, wErr := c.fetchWallex(ctx)
			if wErr != nil {
				return pricing.RateObservation{}, fmt.Errorf("%w: swapwallet: %v, wallex: %v", ErrAllSourcesDown, err, wErr)
			}
			obs = wallexObs
		} else {
			c.swapFailures = 0
			c.swapSuccesses++
		}
	} else {
		// Active source is Wallex
		obs, err = c.fetchWallex(ctx)
		if err != nil {
			c.logger.Warn("wallex rate poll failed", "error", err)
			// Attempt SwapWallet probe to see if it recovered
			swapObs, sErr := c.fetchSwapWallet(ctx)
			if sErr != nil {
				return pricing.RateObservation{}, fmt.Errorf("%w: wallex: %v, swapwallet: %v", ErrAllSourcesDown, err, sErr)
			}
			obs = swapObs
			c.swapSuccesses = 1
			c.swapFailures = 0
			c.activeSource = "swapwallet"
		} else {
			// Check if SwapWallet has recovered in background
			go c.probeSwapWalletRecovery()
		}
	}

	// Apply jump guard on the resulting rate
	checkedObs, jErr := c.applyJumpGuard(obs)
	if jErr != nil {
		return pricing.RateObservation{}, jErr
	}

	return checkedObs, nil
}

func (c *Client) probeSwapWalletRecovery() {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
	defer cancel()

	obs, err := c.fetchSwapWallet(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && obs.MarketIRTPerUSD > 0 {
		c.swapSuccesses++
		if c.swapSuccesses >= 2 {
			c.logger.Info("swapwallet recovered; switching back as primary fx source")
			c.activeSource = "swapwallet"
			c.swapFailures = 0
		}
	} else {
		c.swapSuccesses = 0
	}
}

func (c *Client) applyJumpGuard(obs pricing.RateObservation) (pricing.RateObservation, error) {
	rate := obs.MarketIRTPerUSD

	// First accepted rate initializes the baseline
	if c.lastAcceptedRate == 0 {
		c.lastAcceptedRate = rate
		c.quarantinedRate = 0
		return obs, nil
	}

	diff := math.Abs(float64(rate-c.lastAcceptedRate)) / float64(c.lastAcceptedRate)
	if diff > c.cfg.MaxJumpPct {
		if c.quarantinedRate > 0 {
			qdiff := math.Abs(float64(rate-c.quarantinedRate)) / float64(c.quarantinedRate)
			if qdiff <= 0.005 { // Within 0.5% of quarantined rate -> confirm jump
				c.logger.Info("quarantined fx rate confirmed by consecutive poll", "new_rate", rate, "previous", c.lastAcceptedRate)
				c.lastAcceptedRate = rate
				c.quarantinedRate = 0
				return obs, nil
			}
		}

		c.quarantinedRate = rate
		c.logger.Warn("rate jump quarantined", "diff_pct", diff*100, "rate", rate, "last_accepted", c.lastAcceptedRate)
		return pricing.RateObservation{}, fmt.Errorf("%w: jump %.2f%% from %d to %d", ErrRateQuarantined, diff*100, c.lastAcceptedRate, rate)
	}

	c.lastAcceptedRate = rate
	c.quarantinedRate = 0
	return obs, nil
}

type swapWalletResponse struct {
	Status string            `json:"status"`
	Result map[string]string `json:"result"`
}

func (c *Client) fetchSwapWallet(ctx context.Context) (pricing.RateObservation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.SwapWalletURL, nil)
	if err != nil {
		return pricing.RateObservation{}, err
	}
	req.Header.Set("User-Agent", UserAgentHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return pricing.RateObservation{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return pricing.RateObservation{}, fmt.Errorf("%w: status %d", ErrInvalidResponse, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return pricing.RateObservation{}, err
	}

	var data swapWalletResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return pricing.RateObservation{}, fmt.Errorf("%w: failed to parse json: %v", ErrInvalidResponse, err)
	}

	if data.Status != "OK" {
		return pricing.RateObservation{}, fmt.Errorf("%w: status is %q", ErrInvalidResponse, data.Status)
	}

	rawRate, ok := data.Result["USDT/IRT"]
	if !ok || rawRate == "" {
		return pricing.RateObservation{}, fmt.Errorf("%w: USDT/IRT pair missing in response", ErrInvalidResponse)
	}

	parsedFloat, err := strconv.ParseFloat(strings.TrimSpace(rawRate), 64)
	if err != nil || parsedFloat <= 0 {
		return pricing.RateObservation{}, fmt.Errorf("%w: invalid numeric value %q: %v", ErrInvalidResponse, rawRate, err)
	}

	rateIRT := pricing.IRT(math.Round(parsedFloat))
	if rateIRT < c.cfg.MinRateIRT || rateIRT > c.cfg.MaxRateIRT {
		return pricing.RateObservation{}, fmt.Errorf("%w: %d IRT not in [%d, %d]", ErrRateOutOfRange, rateIRT, c.cfg.MinRateIRT, c.cfg.MaxRateIRT)
	}

	return pricing.RateObservation{
		ID:              fmt.Sprintf("obs-swap-%d", time.Now().UnixNano()),
		MarketIRTPerUSD: rateIRT,
		ObservedAt:      time.Now().UTC(),
		SourceRef:       "swapwallet:USDT/IRT",
	}, nil
}

type wallexResponse struct {
	Result struct {
		Symbols map[string]struct {
			Stats struct {
				BidPrice  string `json:"bidPrice"`
				AskPrice  string `json:"askPrice"`
				LastPrice string `json:"lastPrice"`
			} `json:"stats"`
		} `json:"symbols"`
	} `json:"result"`
}

func (c *Client) fetchWallex(ctx context.Context) (pricing.RateObservation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.WallexURL, nil)
	if err != nil {
		return pricing.RateObservation{}, err
	}
	req.Header.Set("User-Agent", UserAgentHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return pricing.RateObservation{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return pricing.RateObservation{}, fmt.Errorf("%w: wallex status %d", ErrInvalidResponse, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return pricing.RateObservation{}, err
	}

	var data wallexResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return pricing.RateObservation{}, fmt.Errorf("%w: failed to parse wallex json: %v", ErrInvalidResponse, err)
	}

	sym, ok := data.Result.Symbols["USDTTMN"]
	if !ok {
		return pricing.RateObservation{}, fmt.Errorf("%w: wallex USDTTMN symbol missing", ErrInvalidResponse)
	}

	var parsedRate float64
	bid, bErr := strconv.ParseFloat(strings.TrimSpace(sym.Stats.BidPrice), 64)
	ask, aErr := strconv.ParseFloat(strings.TrimSpace(sym.Stats.AskPrice), 64)

	if bErr == nil && aErr == nil && bid > 0 && ask > 0 {
		parsedRate = (bid + ask) / 2.0
	} else if last, lErr := strconv.ParseFloat(strings.TrimSpace(sym.Stats.LastPrice), 64); lErr == nil && last > 0 {
		parsedRate = last
	} else {
		return pricing.RateObservation{}, fmt.Errorf("%w: wallex prices could not be parsed", ErrInvalidResponse)
	}

	rateIRT := pricing.IRT(math.Round(parsedRate))
	if rateIRT < c.cfg.MinRateIRT || rateIRT > c.cfg.MaxRateIRT {
		return pricing.RateObservation{}, fmt.Errorf("%w: wallex %d IRT not in [%d, %d]", ErrRateOutOfRange, rateIRT, c.cfg.MinRateIRT, c.cfg.MaxRateIRT)
	}

	return pricing.RateObservation{
		ID:              fmt.Sprintf("obs-wallex-%d", time.Now().UnixNano()),
		MarketIRTPerUSD: rateIRT,
		ObservedAt:      time.Now().UTC(),
		SourceRef:       "wallex:USDTTMN",
	}, nil
}

var _ port.FxRateSource = (*Client)(nil)
