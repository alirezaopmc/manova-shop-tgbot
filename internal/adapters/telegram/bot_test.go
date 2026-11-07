package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/hetzner"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
)

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{100, "100"},
		{1000, "1,000"},
		{10500, "10,500"},
		{10500000, "10,500,000"},
	}

	for _, c := range cases {
		out := formatNumber(c.input)
		if out != c.expected {
			t.Fatalf("for %d expected %q, got %q", c.input, c.expected, out)
		}
	}
}

func TestPhoneStatusLabel(t *testing.T) {
	if phoneStatusLabel(true) != "🟢 تایید شده" {
		t.Fatalf("unexpected true label")
	}
	if phoneStatusLabel(false) != "🔴 تایید نشده (نیاز به احراز)" {
		t.Fatalf("unexpected false label")
	}
}

func TestParseAmountIRT(t *testing.T) {
	cases := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"100000", 100_000, false},
		{"500,000", 500_000, false},
		{"۵۰۰,۰۰۰", 500_000, false},
		{"۵۰۰۰۰۰ تومان", 500_000, false},
		{"۲۵۰ هزار", 250_000, false},
		{"100k", 100_000, false},
		{"2m", 2_000_000, false},
		{"۲ میلیون", 2_000_000, false},
		{"  ۷۵۰,۰۰۰ ت  ", 750_000, false},
		{"", 0, true},
		{"   ", 0, true},
		{"hello world", 0, true},
	}

	for _, c := range cases {
		out, err := parseAmountIRT(c.input)
		if c.wantErr {
			if err == nil {
				t.Fatalf("expected error for input %q, got %d", c.input, out)
			}
		} else {
			if err != nil || out != c.expected {
				t.Fatalf("for input %q expected %d, got %d (err: %v)", c.input, c.expected, out, err)
			}
		}
	}
}

func TestAmtLabel(t *testing.T) {
	if amtLabel(0) != "اعلام نشده" {
		t.Fatalf("expected 'اعلام نشده' for 0")
	}
	if amtLabel(500000) != "500,000 تومان" {
		t.Fatalf("expected '500,000 تومان' for 500000, got %q", amtLabel(500000))
	}
}

func TestPendingTopupSession(t *testing.T) {
	s := NewService(nil, nil, nil)
	userID := int64(987654)

	// Initially zero
	if amt := s.getPendingTopup(userID); amt != 0 {
		t.Fatalf("expected 0, got %d", amt)
	}

	// Set amount
	s.setPendingTopup(userID, 750000)
	if amt := s.getPendingTopup(userID); amt != 750000 {
		t.Fatalf("expected 750000, got %d", amt)
	}

	// Clear
	s.clearPendingTopup(userID)
	if amt := s.getPendingTopup(userID); amt != 0 {
		t.Fatalf("expected 0 after clear, got %d", amt)
	}
}

func TestValidateSSHPublicKey(t *testing.T) {
	// Sample ed25519 public key
	validKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@manova.space"
	cleaned, err := validateSSHPublicKey("  " + validKey + "\n")
	if err != nil {
		t.Fatalf("expected valid key, got error: %v", err)
	}
	if cleaned != validKey {
		t.Fatalf("expected %q, got %q", validKey, cleaned)
	}

	// Invalid key
	if _, err := validateSSHPublicKey("invalid-ssh-key-data"); err == nil {
		t.Fatalf("expected error for invalid key")
	}

	// Empty key
	if _, err := validateSSHPublicKey("   "); err == nil {
		t.Fatalf("expected error for empty key")
	}
}

func TestBuyDraftSession(t *testing.T) {
	s := NewService(nil, nil, nil)
	userID := int64(112233)

	if d := s.getBuyDraft(userID); d != nil {
		t.Fatalf("expected nil draft initially")
	}

	draft := &buyDraft{
		PlanID:     "cx23",
		Location:   "nbg1",
		Distro:     "ubuntu",
		OSImage:    "ubuntu-24.04",
		AccessMode: "ssh",
		SSHKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl",
		Awaiting:   "ssh_key",
	}
	s.setBuyDraft(userID, draft)

	got := s.getBuyDraft(userID)
	if got == nil || got.PlanID != "cx23" || got.Location != "nbg1" || got.Awaiting != "ssh_key" {
		t.Fatalf("unexpected draft: %+v", got)
	}

	s.clearBuyDraft(userID)
	if d := s.getBuyDraft(userID); d != nil {
		t.Fatalf("expected nil after clear")
	}
}

func TestLocationAndOSLabels(t *testing.T) {
	if locationName("nbg1") != "nbg1 (Nuremberg, DE)" {
		t.Errorf("unexpected nbg1 label: %s", locationName("nbg1"))
	}
	if locationName("fsn1") != "fsn1 (Falkenstein, DE)" {
		t.Errorf("unexpected fsn1 label: %s", locationName("fsn1"))
	}
	if locationName("hel1") != "hel1 (Helsinki, FI)" {
		t.Errorf("unexpected hel1 label: %s", locationName("hel1"))
	}

	if osImageLabel("ubuntu-24.04") != "Ubuntu 24.04 LTS" {
		t.Errorf("unexpected ubuntu label: %s", osImageLabel("ubuntu-24.04"))
	}
	if osImageLabel("debian-12") != "Debian 12" {
		t.Errorf("unexpected debian label: %s", osImageLabel("debian-12"))
	}
}

func TestBuyExecutionAndServerManagement(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()

	cfg := &config.Config{
		RateSpreadIRTPerUSD: 5000,
		SupportHandle:       "@manova_support",
	}

	simProvider := hetzner.NewClient(hetzner.Config{SimulationMode: true})
	svc := NewService(nil, cfg, store, simProvider)

	userID := int64(887766)
	usr := user.New(userID, "tester", "Test", "User", user.RoleUser, time.Now().UTC())
	if err := store.Users().Save(ctx, usr); err != nil {
		t.Fatalf("save user failed: %v", err)
	}

	w, _ := store.Wallets().GetByUserID(ctx, userID)

	// 1. Attempt buy with 0 balance -> must fail gracefully without provisioning
	w.BalanceIRT = 0
	_ = store.Wallets().UpdateBalances(ctx, w)

	err := svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx23", "nbg1", "ubuntu-24.04", "pwd")
	if err != nil {
		t.Fatalf("expected nil return on insufficient balance notice, got %v", err)
	}
	resList, _ := store.Billing().ListUserResources(ctx, userID)
	if len(resList) != 0 {
		t.Fatalf("expected 0 resources with empty balance, got %d", len(resList))
	}

	// 2. Fund wallet with 500,000 Toman and buy CX23 with Root Password mode
	w.BalanceIRT = 500_000
	_ = store.Wallets().UpdateBalances(ctx, w)

	err = svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx23", "nbg1", "ubuntu-24.04", "pwd")
	if err != nil {
		t.Fatalf("handleBuyExecute failed: %v", err)
	}

	wAfter, _ := store.Wallets().GetByUserID(ctx, userID)
	if wAfter.BalanceIRT >= 500_000 {
		t.Fatalf("expected wallet balance deducted, got %d", wAfter.BalanceIRT)
	}

	resList, err = store.Billing().ListUserResources(ctx, userID)
	if err != nil || len(resList) != 1 {
		t.Fatalf("expected 1 active resource, got %d (err: %v)", len(resList), err)
	}

	res1 := resList[0]
	if res1.UserID != userID || res1.Location != "nbg1" || res1.ImageName != "ubuntu-24.04" {
		t.Fatalf("unexpected resource details: %+v", res1)
	}
	if res1.RootPassword == "" {
		t.Fatalf("expected generated root password")
	}
	if res1.IPv4Address == "" {
		t.Fatalf("expected allocated IPv4 address")
	}
	if res1.PowerState != billing.PowerStateRunning {
		t.Fatalf("expected running power state, got %s", res1.PowerState)
	}

	// 3. Buy second server (CX33) with SSH key mode
	sshKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@manova.space"
	draft := &buyDraft{
		PlanID:     "cx33",
		Location:   "fsn1",
		OSImage:    "debian-12",
		AccessMode: "ssh",
		SSHKey:     sshKey,
	}
	svc.setBuyDraft(userID, draft)

	err = svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx33", "fsn1", "debian-12", "ssh")
	if err != nil {
		t.Fatalf("second buy failed: %v", err)
	}

	resList, _ = store.Billing().ListUserResources(ctx, userID)
	if len(resList) != 2 {
		t.Fatalf("expected 2 active resources, got %d", len(resList))
	}

	var res2 *billing.ProvisionedResource
	for _, r := range resList {
		if r.ID != res1.ID {
			res2 = &r
			break
		}
	}
	if res2 == nil {
		t.Fatalf("expected second resource to exist")
	}
	if res2.SSHKey != sshKey {
		t.Fatalf("expected saved SSH key %q, got %q", sshKey, res2.SSHKey)
	}
	if res2.RootPassword != "" {
		t.Fatalf("expected empty root password when SSH key provided, got %q", res2.RootPassword)
	}

	// 4. Test Server Management Actions on res1
	cbMsg := &telego.Message{Chat: telego.Chat{ID: userID}}
	cb := &telego.CallbackQuery{ID: "cb_test", Message: cbMsg}

	// Power off
	if err := svc.handleServerPowerOff(ctx, cb, res1.ID, userID); err != nil {
		t.Fatalf("handleServerPowerOff failed: %v", err)
	}
	res1Updated, _ := store.Billing().GetResource(ctx, res1.ID)
	if res1Updated.PowerState != billing.PowerStateOff {
		t.Fatalf("expected PowerStateOff, got %s", res1Updated.PowerState)
	}

	// Power on
	if err := svc.handleServerPowerOn(ctx, cb, res1.ID, userID); err != nil {
		t.Fatalf("handleServerPowerOn failed: %v", err)
	}
	res1Updated, _ = store.Billing().GetResource(ctx, res1.ID)
	if res1Updated.PowerState != billing.PowerStateRunning {
		t.Fatalf("expected PowerStateRunning, got %s", res1Updated.PowerState)
	}

	// Reboot
	if err := svc.handleServerReboot(ctx, cb, res1.ID, userID); err != nil {
		t.Fatalf("handleServerReboot failed: %v", err)
	}

	// Reset Password
	oldPwd := res1.RootPassword
	if err := svc.handleServerResetPassword(ctx, cb, res1.ID, userID); err != nil {
		t.Fatalf("handleServerResetPassword failed: %v", err)
	}
	res1Updated, _ = store.Billing().GetResource(ctx, res1.ID)
	if res1Updated.RootPassword == "" || res1Updated.RootPassword == oldPwd {
		t.Fatalf("expected new reset root password, got %s", res1Updated.RootPassword)
	}

	// Enable Rescue
	if err := svc.handleServerRescue(ctx, cb, res1.ID, userID); err != nil {
		t.Fatalf("handleServerRescue failed: %v", err)
	}

	// Delete Confirmation
	if err := svc.handleServerDeleteConfirm(ctx, cb, res1.ID, userID); err != nil {
		t.Fatalf("handleServerDeleteConfirm failed: %v", err)
	}
	res1Updated, _ = store.Billing().GetResource(ctx, res1.ID)
	if res1Updated.Status != billing.ResourceStatusDeleted {
		t.Fatalf("expected ResourceStatusDeleted, got %s", res1Updated.Status)
	}

	// Verify res1 is no longer in active user resources list
	resListAfterDelete, _ := store.Billing().ListUserResources(ctx, userID)
	if len(resListAfterDelete) != 1 {
		t.Fatalf("expected 1 active resource remaining, got %d", len(resListAfterDelete))
	}
	if resListAfterDelete[0].ID != res2.ID {
		t.Fatalf("expected remaining resource to be res2")
	}
}

type failingProvider struct {
	port.CloudProvider
	failErr error
}

func (f *failingProvider) HasToken() bool { return true }
func (f *failingProvider) CreateServer(ctx context.Context, name string, serverType string, imageName string, location string, sshKeyIDs []string) (*port.ProviderCreateResult, error) {
	return nil, f.failErr
}

func TestBuyExecutionAccountingRollbackOnFailure(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		RateSpreadIRTPerUSD: 5000,
		SupportHandle:       "@manova_support",
	}

	failErr := errors.New("simulated hetzner provisioning timeout")
	failProv := &failingProvider{failErr: failErr}
	svc := NewService(nil, cfg, store, failProv)

	userID := int64(778899)
	usr := user.New(userID, "tester", "Test", "User", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	initialBalance := pricing.IRT(1_000_000)
	w, _ := store.Wallets().GetByUserID(ctx, userID)
	w.BalanceIRT = initialBalance
	_ = store.Wallets().UpdateBalances(ctx, w)

	err := svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx23", "nbg1", "ubuntu-24.04", "pwd")
	if err != nil {
		t.Fatalf("expected nil error (graceful failure response), got %v", err)
	}

	// 1. Verify wallet balance was refunded back to initial balance
	wAfter, _ := store.Wallets().GetByUserID(ctx, userID)
	if wAfter.BalanceIRT != initialBalance {
		t.Fatalf("expected balance refunded to %d, got %d", initialBalance, wAfter.BalanceIRT)
	}

	// 2. Verify ledger contains compensating KindUsageReversal entry
	entries := store.LedgerEntries()
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries (purchase debit + rollback credit), got %d", len(entries))
	}

	debitEntry := entries[0]
	if debitEntry.Kind != wallet.KindPurchase || debitEntry.Amount >= 0 {
		t.Fatalf("expected negative purchase entry, got kind=%s amount=%d", debitEntry.Kind, debitEntry.Amount)
	}

	reversalEntry := entries[1]
	if reversalEntry.Kind != wallet.KindUsageReversal {
		t.Fatalf("expected KindUsageReversal entry, got %s", reversalEntry.Kind)
	}
	if reversalEntry.Amount != -debitEntry.Amount {
		t.Fatalf("expected reversal amount %d to equal debit amount %d", reversalEntry.Amount, -debitEntry.Amount)
	}
	if reversalEntry.BalanceAfter != int64(initialBalance) {
		t.Fatalf("expected BalanceAfter %d, got %d", initialBalance, reversalEntry.BalanceAfter)
	}
	if reversalEntry.RefType != "order" || reversalEntry.RefID == "" {
		t.Fatalf("expected RefType=order and non-empty RefID, got RefType=%s RefID=%s", reversalEntry.RefType, reversalEntry.RefID)
	}

	// Invariant: sum of ledger amounts must equal 0 (no net change)
	sum := debitEntry.Amount + reversalEntry.Amount
	if sum != 0 {
		t.Fatalf("ledger invariant broken: sum of ledger amounts is %d (expected 0)", sum)
	}

	// 3. Verify no provisioned resources exist
	resList, _ := store.Billing().ListUserResources(ctx, userID)
	if len(resList) != 0 {
		t.Fatalf("expected 0 resources created, got %d", len(resList))
	}
}

type slowProvider struct {
	port.CloudProvider
	callCount int32
	delay     time.Duration
}

func (s *slowProvider) HasToken() bool { return true }
func (s *slowProvider) CreateServer(ctx context.Context, name string, serverType string, imageName string, location string, sshKeyIDs []string) (*port.ProviderCreateResult, error) {
	atomic.AddInt32(&s.callCount, 1)
	time.Sleep(s.delay)
	return &port.ProviderCreateResult{
		ExternalID:  "srv-12345",
		IPv4Address: "192.168.1.100",
		Status:      "running",
	}, nil
}

func TestBuyExecutionConcurrentDoublePurchasePrevention(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		RateSpreadIRTPerUSD: 5000,
		SupportHandle:       "@manova_support",
	}

	simProvider := hetzner.NewClient(hetzner.Config{SimulationMode: true})
	slowProv := &slowProvider{
		CloudProvider: simProvider,
		delay:         50 * time.Millisecond,
	}
	svc := NewService(nil, cfg, store, slowProv)

	userID := int64(556677)
	usr := user.New(userID, "tester", "Test", "User", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	// Fund wallet with enough for 2 servers
	w, _ := store.Wallets().GetByUserID(ctx, userID)
	w.BalanceIRT = 2_000_000
	_ = store.Wallets().UpdateBalances(ctx, w)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx23", "nbg1", "ubuntu-24.04", "pwd")
	}()
	go func() {
		defer wg.Done()
		_ = svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx23", "nbg1", "ubuntu-24.04", "pwd")
	}()
	wg.Wait()

	// Exactly ONE server must be created
	if slowProv.callCount != 1 {
		t.Fatalf("expected CreateServer to be called exactly once, called %d times", slowProv.callCount)
	}

	resList, _ := store.Billing().ListUserResources(ctx, userID)
	if len(resList) != 1 {
		t.Fatalf("expected 1 active resource, got %d", len(resList))
	}
}

func TestConcurrentReceiptApproval(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		RateSpreadIRTPerUSD: 5000,
		SupportHandle:       "@manova_support",
	}
	svc := NewService(nil, cfg, store)

	userID := int64(12345)
	usr := user.New(userID, "tester", "Test", "User", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	// User wallet starts at 0
	w, _ := store.Wallets().GetByUserID(ctx, userID)
	w.BalanceIRT = 0
	_ = store.Wallets().UpdateBalances(ctx, w)

	rcptID := "rcpt-test-concurrency"
	rcp := &receipt.Receipt{
		ID:        rcptID,
		UserID:    userID,
		Purpose:   receipt.PurposeWalletTopup,
		AmountIRT: 500_000,
		Status:    receipt.StatusPendingReview,
		CreatedAt: time.Now().UTC(),
	}
	_ = store.Receipts().Save(ctx, rcp)

	// Simulate 10 concurrent approval clicks
	var wg sync.WaitGroup
	concurrency := 10
	cbMsg := &telego.Message{Chat: telego.Chat{ID: userID}}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cb := &telego.CallbackQuery{
				ID:      fmt.Sprintf("cb_%d", idx),
				From:    telego.User{ID: 999}, // Admin ID
				Message: cbMsg,
			}
			_ = svc.handleReceiptApproval(ctx, cb, rcptID, 500_000)
		}(i)
	}

	wg.Wait()

	// Verify wallet was credited EXACTLY once (500,000 IRT), NOT 10 times!
	wFinal, err := store.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("get wallet failed: %v", err)
	}
	if wFinal.BalanceIRT != 500_000 {
		t.Fatalf("expected wallet balance 500,000 IRT, got %d IRT", wFinal.BalanceIRT)
	}

	// Verify receipt status is Approved
	rcpFinal, err := store.Receipts().GetByID(ctx, rcptID)
	if err != nil {
		t.Fatalf("get receipt failed: %v", err)
	}
	if rcpFinal.Status != receipt.StatusApproved {
		t.Fatalf("expected receipt status Approved, got %s", rcpFinal.Status)
	}

	// Verify exactly ONE ledger entry was recorded
	entries := store.LedgerEntries()
	var topupEntries []wallet.LedgerEntry
	for _, e := range entries {
		if e.Kind == wallet.KindTopupReceipt && e.RefID == rcptID {
			topupEntries = append(topupEntries, e)
		}
	}
	if len(topupEntries) != 1 {
		t.Fatalf("expected exactly 1 ledger entry for receipt %s, got %d", rcptID, len(topupEntries))
	}
	if topupEntries[0].Amount != 500_000 {
		t.Fatalf("expected ledger entry amount 500,000, got %d", topupEntries[0].Amount)
	}
}

type mockConversationStore struct {
	mu     sync.Mutex
	states map[string]mockConvEntry
}

type mockConvEntry struct {
	step      string
	data      []byte
	expiresAt time.Time
}

func newMockConversationStore() *mockConversationStore {
	return &mockConversationStore{
		states: make(map[string]mockConvEntry),
	}
}

func (m *mockConversationStore) SaveConversationState(_ context.Context, userID int64, flow string, step string, data any, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, _ := json.Marshal(data)
	key := fmt.Sprintf("%d:%s", userID, flow)
	m.states[key] = mockConvEntry{step: step, data: b, expiresAt: expiresAt}
	return nil
}

func (m *mockConversationStore) GetConversationState(_ context.Context, userID int64, flow string, dest any) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%d:%s", userID, flow)
	e, ok := m.states[key]
	if !ok {
		return "", false, nil
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		delete(m.states, key)
		return "", false, nil
	}
	if dest != nil && len(e.data) > 0 {
		_ = json.Unmarshal(e.data, dest)
	}
	return e.step, true, nil
}

func (m *mockConversationStore) DeleteConversationState(_ context.Context, userID int64, flow string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%d:%s", userID, flow)
	delete(m.states, key)
	return nil
}

type storeWithConversations struct {
	*memory.Store
	conv conversationStore
}

func (s *storeWithConversations) Conversations() conversationStore {
	return s.conv
}

func TestConversationStorePersistenceAndRecovery(t *testing.T) {
	memStore := memory.NewStore()
	mockConv := newMockConversationStore()
	swc := &storeWithConversations{Store: memStore, conv: mockConv}

	cfg := &config.Config{
		RateSpreadIRTPerUSD: 5000,
		SupportHandle:       "@manova_support",
	}

	simProvider := hetzner.NewClient(hetzner.Config{SimulationMode: true})
	svc := NewService(nil, cfg, swc, simProvider)

	userID := int64(998877)

	// 1. Set buy draft and pending topup on service
	draft := &buyDraft{
		PlanID:     "cx23",
		Location:   "nbg1",
		Distro:     "ubuntu",
		OSImage:    "ubuntu-24.04",
		AccessMode: "ssh",
		SSHKey:     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl",
		Awaiting:   "ssh_key",
		CreatedAt:  time.Now().UTC(),
	}
	svc.setBuyDraft(userID, draft)
	svc.setPendingTopup(userID, 750_000)

	// 2. Simulate bot crash / restart:
	// New service instance with completely empty in-memory maps
	restartedSvc := NewService(nil, cfg, swc, simProvider)

	// Verify in-memory maps are empty initially
	restartedSvc.mu.RLock()
	if len(restartedSvc.buyDrafts) != 0 || len(restartedSvc.pendingTopups) != 0 {
		t.Fatalf("expected empty memory maps in restarted service")
	}
	restartedSvc.mu.RUnlock()

	// 3. Recover buy draft across restart
	recoveredDraft := restartedSvc.getBuyDraft(userID)
	if recoveredDraft == nil {
		t.Fatalf("expected buy draft to be recovered from conversation store")
	}
	if recoveredDraft.PlanID != "cx23" || recoveredDraft.Location != "nbg1" || recoveredDraft.Awaiting != "ssh_key" {
		t.Fatalf("recovered draft mismatch: %+v", recoveredDraft)
	}

	// 4. Recover pending topup across restart
	recoveredTopup := restartedSvc.getPendingTopup(userID)
	if recoveredTopup != 750_000 {
		t.Fatalf("expected recovered topup 750,000, got %d", recoveredTopup)
	}

	// 5. Test cleanups propagate to store
	restartedSvc.clearBuyDraft(userID)
	restartedSvc.clearPendingTopup(userID)

	if d := restartedSvc.getBuyDraft(userID); d != nil {
		t.Fatalf("expected nil buy draft after clear, got %+v", d)
	}
	if amt := restartedSvc.getPendingTopup(userID); amt != 0 {
		t.Fatalf("expected 0 topup after clear, got %d", amt)
	}
}
