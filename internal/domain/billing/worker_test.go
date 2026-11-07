package billing_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
)

type mockCloud struct {
	mu           sync.Mutex
	shutdownIDs  []string
	powerOffIDs  []string
	shutdownFail bool
}

func (m *mockCloud) Shutdown(_ context.Context, externalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdownIDs = append(m.shutdownIDs, externalID)
	if m.shutdownFail {
		return errors.New("simulated shutdown failure")
	}
	return nil
}

func (m *mockCloud) PowerOff(_ context.Context, externalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.powerOffIDs = append(m.powerOffIDs, externalID)
	return nil
}

type mockNotifier struct {
	mu           sync.Mutex
	userMessages []string
	adminAlerts  []string
}

func (n *mockNotifier) SendUserMessage(_ context.Context, userID int64, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.userMessages = append(n.userMessages, fmt.Sprintf("%d: %s", userID, text))
	return nil
}

func (n *mockNotifier) SendAdminAlert(_ context.Context, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.adminAlerts = append(n.adminAlerts, text)
	return nil
}

func TestWorker_MinuteBillingCalculation(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cloud := &mockCloud{}
	notifier := &mockNotifier{}

	// Setup user
	now := time.Now().UTC()
	u := user.New(12345, "testuser", "Test", "User", user.RoleUser, now)
	if err := store.Users().Save(ctx, u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	// Fund wallet with 1,000,000 IRT
	w, err := store.Wallets().GetByUserID(ctx, 12345)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	w.BalanceIRT = 1_000_000
	if err := store.Wallets().UpdateBalances(ctx, w); err != nil {
		t.Fatalf("update wallet: %v", err)
	}

	// Setup FX rate: 100,000 IRT market rate + 5,000 spread = 105,000 IRT sell rate
	store.SetMarketRate(100_000)

	// Setup provisioned resource: $0.10/hr = 100,000 micro-USD
	// Started 65 minutes ago, posted through minute 60 -> 5 minutes to bill (60, 61, 62, 63, 64)
	billingStart := now.Add(-65 * time.Minute)
	res := &billing.ProvisionedResource{
		ID:                  "res-101",
		UserID:              12345,
		ServerName:          "web-server-1",
		ExternalID:          "srv-ext-1",
		Status:              billing.ResourceStatusActive,
		PowerState:          billing.PowerStateRunning,
		HourlyPriceUSD:      100_000,
		BillingStartAt:      billingStart,
		PostedThroughMinute: 60,
		LastVerifiedAt:      billingStart,
		CreatedAt:           billingStart,
	}
	if err := store.Billing().SaveResource(ctx, res); err != nil {
		t.Fatalf("save resource: %v", err)
	}

	worker := billing.NewWorker(store, cloud, notifier, billing.WorkerConfig{
		TickInterval: 60 * time.Second,
		SpreadIRT:    5_000,
		GracePeriod:  0,
	})

	// Execute single minute tick
	if err := worker.ProcessMinuteTick(ctx, now); err != nil {
		t.Fatalf("ProcessMinuteTick failed: %v", err)
	}

	// Verify resource updated
	updatedRes, err := store.Billing().GetResource(ctx, "res-101")
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if updatedRes.PostedThroughMinute != 65 {
		t.Errorf("expected PostedThroughMinute 65, got %d", updatedRes.PostedThroughMinute)
	}
	if updatedRes.PowerState != billing.PowerStateRunning {
		t.Errorf("expected PowerState running, got %s", updatedRes.PowerState)
	}

	// Calculate expected charge:
	// At $0.10/hr and 105,000 sell rate -> 10,500 IRT/hr -> 175 IRT/min
	// 5 minutes * 175 = 875 IRT
	updatedW, err := store.Wallets().GetByUserID(ctx, 12345)
	if err != nil {
		t.Fatalf("get updated wallet: %v", err)
	}
	expectedBalance := pricing.IRT(1_000_000 - 875)
	if updatedW.BalanceIRT != expectedBalance {
		t.Errorf("expected wallet balance %d, got %d", expectedBalance, updatedW.BalanceIRT)
	}
}

func TestWorker_DepletedBalance_RunwayShutdown(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cloud := &mockCloud{}
	notifier := &mockNotifier{}

	now := time.Now().UTC()
	u := user.New(22222, "pooruser", "Poor", "User", user.RoleUser, now)
	if err := store.Users().Save(ctx, u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	// Empty wallet balance (0 IRT), segment New (floor 0)
	w, err := store.Wallets().GetByUserID(ctx, 22222)
	if err != nil {
		t.Fatalf("get wallet: %v", err)
	}
	w.BalanceIRT = 0
	if err := store.Wallets().UpdateBalances(ctx, w); err != nil {
		t.Fatalf("update wallet: %v", err)
	}

	store.SetMarketRate(100_000)

	billingStart := now.Add(-65 * time.Minute)
	res := &billing.ProvisionedResource{
		ID:                  "res-202",
		UserID:              22222,
		ServerName:          "depleted-server",
		ExternalID:          "srv-ext-2",
		Status:              billing.ResourceStatusActive,
		PowerState:          billing.PowerStateRunning,
		HourlyPriceUSD:      100_000,
		BillingStartAt:      billingStart,
		PostedThroughMinute: 60,
		LastVerifiedAt:      billingStart,
		CreatedAt:           billingStart,
	}
	if err := store.Billing().SaveResource(ctx, res); err != nil {
		t.Fatalf("save resource: %v", err)
	}

	worker := billing.NewWorker(store, cloud, notifier, billing.WorkerConfig{
		TickInterval: 60 * time.Second,
		SpreadIRT:    5_000,
		GracePeriod:  0, // immediate shutdown
	})

	if err := worker.ProcessMinuteTick(ctx, now); err != nil {
		t.Fatalf("ProcessMinuteTick failed: %v", err)
	}

	// Verify cloud.Shutdown was called
	cloud.mu.Lock()
	if len(cloud.shutdownIDs) != 1 || cloud.shutdownIDs[0] != "srv-ext-2" {
		t.Errorf("expected cloud.Shutdown called for srv-ext-2, got: %v", cloud.shutdownIDs)
	}
	cloud.mu.Unlock()

	// Verify server state updated to off
	updatedRes, err := store.Billing().GetResource(ctx, "res-202")
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if updatedRes.PowerState != billing.PowerStateOff {
		t.Errorf("expected PowerState off, got %s", updatedRes.PowerState)
	}

	// Verify user received notification
	notifier.mu.Lock()
	if len(notifier.userMessages) == 0 {
		t.Errorf("expected user notification for server shutdown")
	}
	notifier.mu.Unlock()
}

func TestWorker_GracePeriodHandling(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cloud := &mockCloud{}
	notifier := &mockNotifier{}

	t0 := time.Now().UTC()
	u := user.New(33333, "graceuser", "Grace", "User", user.RoleUser, t0)
	_ = store.Users().Save(ctx, u)

	store.SetMarketRate(100_000)

	billingStart := t0.Add(-65 * time.Minute)
	res := &billing.ProvisionedResource{
		ID:                  "res-303",
		UserID:              33333,
		ServerName:          "grace-server",
		ExternalID:          "srv-ext-3",
		Status:              billing.ResourceStatusActive,
		PowerState:          billing.PowerStateRunning,
		HourlyPriceUSD:      100_000,
		BillingStartAt:      billingStart,
		PostedThroughMinute: 60,
		LastVerifiedAt:      billingStart,
		CreatedAt:           billingStart,
	}
	_ = store.Billing().SaveResource(ctx, res)

	worker := billing.NewWorker(store, cloud, notifier, billing.WorkerConfig{
		TickInterval: 60 * time.Second,
		SpreadIRT:    5_000,
		GracePeriod:  10 * time.Minute,
	})

	// Tick 1 at t0: grace period starts, server should NOT be shut down yet
	if err := worker.ProcessMinuteTick(ctx, t0); err != nil {
		t.Fatalf("tick 1 failed: %v", err)
	}

	cloud.mu.Lock()
	if len(cloud.shutdownIDs) > 0 {
		t.Errorf("expected no shutdown during grace period, got %v", cloud.shutdownIDs)
	}
	cloud.mu.Unlock()

	// Tick 2 at t0 + 11m: grace period expired, shutdown MUST be triggered
	tExpired := t0.Add(11 * time.Minute)
	if err := worker.ProcessMinuteTick(ctx, tExpired); err != nil {
		t.Fatalf("tick 2 failed: %v", err)
	}

	cloud.mu.Lock()
	if len(cloud.shutdownIDs) != 1 || cloud.shutdownIDs[0] != "srv-ext-3" {
		t.Errorf("expected shutdown after grace period expired, got %v", cloud.shutdownIDs)
	}
	cloud.mu.Unlock()
}

func TestWorker_ShutdownFallbackToPowerOff(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cloud := &mockCloud{shutdownFail: true} // shutdown returns error
	notifier := &mockNotifier{}

	now := time.Now().UTC()
	u := user.New(44444, "poweroffuser", "Power", "Off", user.RoleUser, now)
	_ = store.Users().Save(ctx, u)

	store.SetMarketRate(100_000)

	billingStart := now.Add(-65 * time.Minute)
	res := &billing.ProvisionedResource{
		ID:                  "res-404",
		UserID:              44444,
		ServerName:          "poweroff-server",
		ExternalID:          "srv-ext-4",
		Status:              billing.ResourceStatusActive,
		PowerState:          billing.PowerStateRunning,
		HourlyPriceUSD:      100_000,
		BillingStartAt:      billingStart,
		PostedThroughMinute: 60,
		LastVerifiedAt:      billingStart,
		CreatedAt:           billingStart,
	}
	_ = store.Billing().SaveResource(ctx, res)

	worker := billing.NewWorker(store, cloud, notifier, billing.WorkerConfig{
		TickInterval: 60 * time.Second,
		SpreadIRT:    5_000,
		GracePeriod:  0,
	})

	if err := worker.ProcessMinuteTick(ctx, now); err != nil {
		t.Fatalf("ProcessMinuteTick failed: %v", err)
	}

	// Verify Shutdown was tried, and PowerOff was called as fallback
	cloud.mu.Lock()
	if len(cloud.shutdownIDs) != 1 {
		t.Errorf("expected 1 shutdown attempt, got %d", len(cloud.shutdownIDs))
	}
	if len(cloud.powerOffIDs) != 1 || cloud.powerOffIDs[0] != "srv-ext-4" {
		t.Errorf("expected PowerOff called as fallback, got %v", cloud.powerOffIDs)
	}
	cloud.mu.Unlock()
}

func TestWorker_Run_ContextCancel(t *testing.T) {
	store := memory.NewStore()
	cloud := &mockCloud{}
	notifier := &mockNotifier{}

	worker := billing.NewWorker(store, cloud, notifier, billing.WorkerConfig{
		TickInterval: 10 * time.Millisecond,
		SpreadIRT:    5_000,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(ctx)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker did not terminate upon context cancellation")
	}
}
