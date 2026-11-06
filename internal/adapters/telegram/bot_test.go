package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
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
	if locationName("nbg1") != "🇩🇪 نورنبرگ، آلمان (nbg1)" {
		t.Errorf("unexpected nbg1 label: %s", locationName("nbg1"))
	}
	if locationName("fsn1") != "🇩🇪 فالکن‌اشتاین، آلمان (fsn1)" {
		t.Errorf("unexpected fsn1 label: %s", locationName("fsn1"))
	}
	if locationName("hel1") != "🇫🇮 هلسینکی، فنلاند (hel1)" {
		t.Errorf("unexpected hel1 label: %s", locationName("hel1"))
	}

	if osImageLabel("ubuntu-24.04") != "Ubuntu 24.04 LTS" {
		t.Errorf("unexpected ubuntu label: %s", osImageLabel("ubuntu-24.04"))
	}
	if osImageLabel("debian-12") != "Debian 12 (Bookworm)" {
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

	svc := NewService(nil, cfg, store)

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
