package fxrate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

type mockRateSetter struct {
	mu   sync.Mutex
	rate pricing.IRT
}

func (m *mockRateSetter) SetMarketRate(rate pricing.IRT) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rate = rate
}

func (m *mockRateSetter) GetRate() pricing.IRT {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rate
}

func TestSwapWalletSuccessfulFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"OK","result":{"USDT/IRT":"269500"}}`)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		SwapWalletURL: server.URL,
		Timeout:       2 * time.Second,
	}, nil)

	obs, err := client.FetchRate(context.Background())
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}

	if obs.MarketIRTPerUSD != 269500 {
		t.Fatalf("expected rate 269500, got %d", obs.MarketIRTPerUSD)
	}

	if obs.SourceRef != "swapwallet:USDT/IRT" {
		t.Fatalf("expected swapwallet source ref, got %s", obs.SourceRef)
	}
}

func TestWallexFallbackWhenSwapWalletFails(t *testing.T) {
	swapServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer swapServer.Close()

	wallexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"result": {
				"symbols": {
					"USDTTMN": {
						"stats": {
							"bidPrice": "268000.00",
							"askPrice": "269000.00",
							"lastPrice": "268500.00"
						}
					}
				}
			}
		}`)
	}))
	defer wallexServer.Close()

	client := NewClient(ClientConfig{
		SwapWalletURL: swapServer.URL,
		WallexURL:     wallexServer.URL,
		Timeout:       2 * time.Second,
	}, nil)

	obs, err := client.FetchRate(context.Background())
	if err != nil {
		t.Fatalf("unexpected fallback error: %v", err)
	}

	// Midpoint of 268000 and 269000 is 268500
	if obs.MarketIRTPerUSD != 268500 {
		t.Fatalf("expected 268500, got %d", obs.MarketIRTPerUSD)
	}

	if obs.SourceRef != "wallex:USDTTMN" {
		t.Fatalf("expected wallex source ref, got %s", obs.SourceRef)
	}
}

func TestRangeValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"OK","result":{"USDT/IRT":"50000"}}`)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		SwapWalletURL: server.URL,
		WallexURL:     server.URL,
		MinRateIRT:    100_000,
		MaxRateIRT:    2_000_000,
	}, nil)

	_, err := client.FetchRate(context.Background())
	if err == nil {
		t.Fatalf("expected error for rate 50000 below minimum, got nil")
	}
}

func TestJumpGuardQuarantine(t *testing.T) {
	var currentPrice string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"OK","result":{"USDT/IRT":"`+currentPrice+`"}}`)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		SwapWalletURL: server.URL,
		MaxJumpPct:    0.03, // 3%
	}, nil)

	// 1. Initial poll establishes baseline at 200,000
	currentPrice = "200000"
	obs1, err := client.FetchRate(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on initial poll: %v", err)
	}
	if obs1.MarketIRTPerUSD != 200000 {
		t.Fatalf("expected baseline 200000, got %d", obs1.MarketIRTPerUSD)
	}

	// 2. Sudden 10% jump to 220,000 -> should be quarantined
	currentPrice = "220000"
	_, err = client.FetchRate(context.Background())
	if err == nil {
		t.Fatalf("expected quarantine error for 10%% jump, got nil")
	}

	// 3. Consecutive poll confirms 220,000 -> jump confirmed
	obs3, err := client.FetchRate(context.Background())
	if err != nil {
		t.Fatalf("expected jump to be confirmed on consecutive poll: %v", err)
	}
	if obs3.MarketIRTPerUSD != 220000 {
		t.Fatalf("expected confirmed rate 220000, got %d", obs3.MarketIRTPerUSD)
	}
}

func TestPollerImmediateAndPeriodic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"OK","result":{"USDT/IRT":"271000"}}`)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		SwapWalletURL: server.URL,
	}, nil)

	setter := &mockRateSetter{}
	poller := NewPoller(setter, client, 50*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go poller.Start(ctx)

	// Poll should happen immediately
	time.Sleep(20 * time.Millisecond)
	if rate := setter.GetRate(); rate != 271000 {
		t.Fatalf("expected poller to set rate 271000 immediately, got %d", rate)
	}
}
