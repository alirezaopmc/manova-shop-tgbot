package telegram

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
)

type spyCloudProvider struct {
	mu sync.Mutex

	calls         []string
	powerOnCalls  int
	powerOffCalls int
	rebootCalls   int
	resetPwdCalls int
	rescueCalls   int
	deleteCalls   int
}

func (s *spyCloudProvider) HasToken() bool { return true }

func (s *spyCloudProvider) CreateServer(ctx context.Context, name string, serverType string, imageName string, location string, sshKeyIDs []string) (*port.ProviderCreateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "CreateServer")
	return &port.ProviderCreateResult{
		ExternalID:   "ext_srv_1",
		IPv4Address:  "192.0.2.1",
		IPv6Address:  "2001:db8::1",
		RootPassword: "root-password",
	}, nil
}

func (s *spyCloudProvider) GetServer(ctx context.Context, externalID string) (*port.ProviderServerStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "GetServer")
	return &port.ProviderServerStatus{
		ExternalID: externalID,
		Status:     "running",
	}, nil
}

func (s *spyCloudProvider) DeleteServer(ctx context.Context, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "DeleteServer")
	s.deleteCalls++
	return nil
}

func (s *spyCloudProvider) PowerOn(ctx context.Context, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "PowerOn")
	s.powerOnCalls++
	return nil
}

func (s *spyCloudProvider) PowerOff(ctx context.Context, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "PowerOff")
	s.powerOffCalls++
	return nil
}

func (s *spyCloudProvider) Shutdown(ctx context.Context, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "Shutdown")
	return nil
}

func (s *spyCloudProvider) Reboot(ctx context.Context, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "Reboot")
	s.rebootCalls++
	return nil
}

func (s *spyCloudProvider) ResetPassword(ctx context.Context, externalID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "ResetPassword")
	s.resetPwdCalls++
	return "new-reset-pwd-12345", nil
}

func (s *spyCloudProvider) EnableRescue(ctx context.Context, externalID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "EnableRescue")
	s.rescueCalls++
	return "new-rescue-pwd-67890", nil
}

func (s *spyCloudProvider) DisableRescue(ctx context.Context, externalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "DisableRescue")
	return nil
}

func (s *spyCloudProvider) ListServerTypes(ctx context.Context) ([]port.ProviderServerType, error) {
	return nil, nil
}

func (s *spyCloudProvider) ListLocations(ctx context.Context) ([]port.ProviderLocation, error) {
	return nil, nil
}

func (s *spyCloudProvider) ListImages(ctx context.Context) ([]port.ProviderImage, error) {
	return nil, nil
}

func (s *spyCloudProvider) TotalCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *spyCloudProvider) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
	s.powerOnCalls = 0
	s.powerOffCalls = 0
	s.rebootCalls = 0
	s.resetPwdCalls = 0
	s.rescueCalls = 0
	s.deleteCalls = 0
}

func TestCheckServerOwnership_Direct(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		AdminTelegramID: 999999,
		SupportHandle:   "@manova_support",
	}
	svc := NewService(nil, cfg, store)

	victimID := int64(1001)
	adminID := int64(8888)
	attackerID := int64(2002)

	_ = store.Users().Save(ctx, user.New(victimID, "victim", "Victim", "", user.RoleUser, time.Now().UTC()))
	_ = store.Users().Save(ctx, user.New(adminID, "admin", "Admin", "", user.RoleAdmin, time.Now().UTC()))
	_ = store.Users().Save(ctx, user.New(attackerID, "attacker", "Attacker", "", user.RoleUser, time.Now().UTC()))

	res := &billing.ProvisionedResource{
		ID:         "res_srv_1",
		UserID:     victimID,
		ExternalID: "ext_123",
		Status:     billing.ResourceStatusActive,
	}

	// 1. Legitimate owner
	ok, err := svc.checkServerOwnership(ctx, victimID, victimID, res)
	if err != nil || !ok {
		t.Fatalf("expected owner to be authorized, got ok=%v, err=%v", ok, err)
	}

	// 2. Admin by RoleAdmin
	ok, err = svc.checkServerOwnership(ctx, adminID, adminID, res)
	if err != nil || !ok {
		t.Fatalf("expected RoleAdmin to be authorized, got ok=%v, err=%v", ok, err)
	}

	// 3. Admin by AdminTelegramID config
	ok, err = svc.checkServerOwnership(ctx, 999999, 999999, res)
	if err != nil || !ok {
		t.Fatalf("expected AdminTelegramID to be authorized, got ok=%v, err=%v", ok, err)
	}

	// 4. Attacker user
	ok, err = svc.checkServerOwnership(ctx, attackerID, attackerID, res)
	if err != nil || ok {
		t.Fatalf("expected attacker to be rejected, got ok=%v, err=%v", ok, err)
	}

	// 5. Unknown user
	ok, err = svc.checkServerOwnership(ctx, 55555, 55555, res)
	if err != nil || ok {
		t.Fatalf("expected unknown user to be rejected, got ok=%v, err=%v", ok, err)
	}

	// 6. Nil resource
	ok, err = svc.checkServerOwnership(ctx, victimID, victimID, nil)
	if err != nil || ok {
		t.Fatalf("expected nil resource to be rejected, got ok=%v, err=%v", ok, err)
	}
}

func TestServerManagement_BOLA_Protection_AttackerRejected(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		SupportHandle: "@manova_support",
	}
	spy := &spyCloudProvider{}
	svc := NewService(nil, cfg, store, spy)

	victimID := int64(1001)
	attackerID := int64(2002)

	_ = store.Users().Save(ctx, user.New(victimID, "victim", "Victim", "", user.RoleUser, time.Now().UTC()))
	_ = store.Users().Save(ctx, user.New(attackerID, "attacker", "Attacker", "", user.RoleUser, time.Now().UTC()))

	res := &billing.ProvisionedResource{
		ID:             "res_victim_srv1",
		UserID:         victimID,
		ExternalID:     "hetzner_ext_123",
		ServerName:     "victim-server",
		OrderItemID:    "cx23",
		Location:       "nbg1",
		ImageName:      "ubuntu-24.04",
		IPv4Address:    "192.0.2.10",
		IPv6Address:    "2001:db8::1",
		RootPassword:   "initial-victim-pwd",
		PowerState:     billing.PowerStateRunning,
		Status:         billing.ResourceStatusActive,
		BillingStartAt: time.Now().UTC(),
	}
	if err := store.Billing().SaveResource(ctx, res); err != nil {
		t.Fatalf("save resource failed: %v", err)
	}

	attackerCbMsg := &telego.Message{Chat: telego.Chat{ID: attackerID}}
	attackerCb := &telego.CallbackQuery{ID: "cb_attack", Message: attackerCbMsg}

	// 1. Attacker attempts to view victim's server management view
	_ = svc.sendServerManagementView(ctx, tu.ID(attackerID), res.ID, attackerID)
	if spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on view attempt, got %d", spy.TotalCalls())
	}

	// 2. Attacker attempts Power Off
	if err := svc.handleServerPowerOff(ctx, attackerCb, res.ID, attackerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.powerOffCalls != 0 || spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on unauthorized PowerOff, got %d", spy.TotalCalls())
	}
	resCurrent, _ := store.Billing().GetResource(ctx, res.ID)
	if resCurrent.PowerState != billing.PowerStateRunning {
		t.Fatalf("server power state changed unexpectedly: %s", resCurrent.PowerState)
	}

	// 3. Attacker attempts Power On (server set to PowerStateOff first)
	res.PowerState = billing.PowerStateOff
	_ = store.Billing().SaveResource(ctx, res)

	if err := svc.handleServerPowerOn(ctx, attackerCb, res.ID, attackerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.powerOnCalls != 0 || spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on unauthorized PowerOn, got %d", spy.TotalCalls())
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.PowerState != billing.PowerStateOff {
		t.Fatalf("server power state changed unexpectedly: %s", resCurrent.PowerState)
	}

	// 4. Attacker attempts Reboot
	if err := svc.handleServerReboot(ctx, attackerCb, res.ID, attackerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.rebootCalls != 0 || spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on unauthorized Reboot, got %d", spy.TotalCalls())
	}

	// 5. Attacker attempts Reset Password
	if err := svc.handleServerResetPassword(ctx, attackerCb, res.ID, attackerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.resetPwdCalls != 0 || spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on unauthorized ResetPassword, got %d", spy.TotalCalls())
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.RootPassword != "initial-victim-pwd" {
		t.Fatalf("root password changed on unauthorized reset: %s", resCurrent.RootPassword)
	}

	// 6. Attacker attempts Enable Rescue Mode
	if err := svc.handleServerRescue(ctx, attackerCb, res.ID, attackerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.rescueCalls != 0 || spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on unauthorized EnableRescue, got %d", spy.TotalCalls())
	}

	// 7. Attacker attempts Delete Prompt
	if err := svc.handleServerDeletePrompt(ctx, tu.ID(attackerID), res.ID, attackerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on unauthorized DeletePrompt, got %d", spy.TotalCalls())
	}

	// 8. Attacker attempts Delete Confirm
	if err := svc.handleServerDeleteConfirm(ctx, attackerCb, res.ID, attackerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.deleteCalls != 0 || spy.TotalCalls() != 0 {
		t.Fatalf("expected 0 provider calls on unauthorized DeleteConfirm, got %d", spy.TotalCalls())
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.Status != billing.ResourceStatusActive {
		t.Fatalf("resource status modified on unauthorized delete: %s", resCurrent.Status)
	}

	// Final verification: exactly 0 calls to Hetzner provider throughout the entire attacker onslaught
	if spy.TotalCalls() != 0 {
		t.Fatalf("total provider calls must be 0, got %d", spy.TotalCalls())
	}
}

func TestServerManagement_LegitimateOwner_Allowed(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		SupportHandle: "@manova_support",
	}
	spy := &spyCloudProvider{}
	svc := NewService(nil, cfg, store, spy)

	ownerID := int64(1001)
	_ = store.Users().Save(ctx, user.New(ownerID, "owner", "Owner", "", user.RoleUser, time.Now().UTC()))

	res := &billing.ProvisionedResource{
		ID:             "res_owner_srv1",
		UserID:         ownerID,
		ExternalID:     "hetzner_ext_456",
		ServerName:     "owner-server",
		OrderItemID:    "cx23",
		Location:       "nbg1",
		ImageName:      "ubuntu-24.04",
		IPv4Address:    "192.0.2.20",
		IPv6Address:    "2001:db8::2",
		RootPassword:   "owner-initial-pwd",
		PowerState:     billing.PowerStateRunning,
		Status:         billing.ResourceStatusActive,
		BillingStartAt: time.Now().UTC(),
	}
	if err := store.Billing().SaveResource(ctx, res); err != nil {
		t.Fatalf("save resource failed: %v", err)
	}

	ownerCbMsg := &telego.Message{Chat: telego.Chat{ID: ownerID}}
	ownerCb := &telego.CallbackQuery{ID: "cb_owner", Message: ownerCbMsg}

	// 1. Owner Power Off
	if err := svc.handleServerPowerOff(ctx, ownerCb, res.ID, ownerID); err != nil {
		t.Fatalf("owner PowerOff failed: %v", err)
	}
	if spy.powerOffCalls != 1 {
		t.Fatalf("expected 1 PowerOff call, got %d", spy.powerOffCalls)
	}
	resCurrent, _ := store.Billing().GetResource(ctx, res.ID)
	if resCurrent.PowerState != billing.PowerStateOff {
		t.Fatalf("expected power state off, got %s", resCurrent.PowerState)
	}

	// 2. Owner Power On
	if err := svc.handleServerPowerOn(ctx, ownerCb, res.ID, ownerID); err != nil {
		t.Fatalf("owner PowerOn failed: %v", err)
	}
	if spy.powerOnCalls != 1 {
		t.Fatalf("expected 1 PowerOn call, got %d", spy.powerOnCalls)
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.PowerState != billing.PowerStateRunning {
		t.Fatalf("expected power state running, got %s", resCurrent.PowerState)
	}

	// 3. Owner Reboot
	if err := svc.handleServerReboot(ctx, ownerCb, res.ID, ownerID); err != nil {
		t.Fatalf("owner Reboot failed: %v", err)
	}
	if spy.rebootCalls != 1 {
		t.Fatalf("expected 1 Reboot call, got %d", spy.rebootCalls)
	}

	// 4. Owner Reset Password
	if err := svc.handleServerResetPassword(ctx, ownerCb, res.ID, ownerID); err != nil {
		t.Fatalf("owner ResetPassword failed: %v", err)
	}
	if spy.resetPwdCalls != 1 {
		t.Fatalf("expected 1 ResetPassword call, got %d", spy.resetPwdCalls)
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.RootPassword != "new-reset-pwd-12345" {
		t.Fatalf("expected updated password, got %s", resCurrent.RootPassword)
	}

	// 5. Owner Enable Rescue Mode
	if err := svc.handleServerRescue(ctx, ownerCb, res.ID, ownerID); err != nil {
		t.Fatalf("owner EnableRescue failed: %v", err)
	}
	if spy.rescueCalls != 1 {
		t.Fatalf("expected 1 EnableRescue call, got %d", spy.rescueCalls)
	}

	// 6. Owner Delete Prompt
	if err := svc.handleServerDeletePrompt(ctx, tu.ID(ownerID), res.ID, ownerID); err != nil {
		t.Fatalf("owner DeletePrompt failed: %v", err)
	}

	// 7. Owner Delete Confirm
	if err := svc.handleServerDeleteConfirm(ctx, ownerCb, res.ID, ownerID); err != nil {
		t.Fatalf("owner DeleteConfirm failed: %v", err)
	}
	if spy.deleteCalls != 1 {
		t.Fatalf("expected 1 DeleteServer call, got %d", spy.deleteCalls)
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.Status != billing.ResourceStatusDeleted {
		t.Fatalf("expected status deleted, got %s", resCurrent.Status)
	}
}

func TestServerManagement_Admin_Allowed(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		SupportHandle: "@manova_support",
	}
	spy := &spyCloudProvider{}
	svc := NewService(nil, cfg, store, spy)

	victimID := int64(1001)
	adminID := int64(9999)

	_ = store.Users().Save(ctx, user.New(victimID, "victim", "Victim", "", user.RoleUser, time.Now().UTC()))
	_ = store.Users().Save(ctx, user.New(adminID, "admin", "Admin", "", user.RoleAdmin, time.Now().UTC()))

	res := &billing.ProvisionedResource{
		ID:             "res_victim_srv2",
		UserID:         victimID,
		ExternalID:     "hetzner_ext_789",
		ServerName:     "victim-server-2",
		OrderItemID:    "cx23",
		Location:       "fsn1",
		ImageName:      "debian-12",
		IPv4Address:    "192.0.2.30",
		IPv6Address:    "2001:db8::3",
		RootPassword:   "victim2-pwd",
		PowerState:     billing.PowerStateRunning,
		Status:         billing.ResourceStatusActive,
		BillingStartAt: time.Now().UTC(),
	}
	if err := store.Billing().SaveResource(ctx, res); err != nil {
		t.Fatalf("save resource failed: %v", err)
	}

	adminCbMsg := &telego.Message{Chat: telego.Chat{ID: adminID}}
	adminCb := &telego.CallbackQuery{ID: "cb_admin", Message: adminCbMsg}

	// 1. Admin Power Off on victim's server
	if err := svc.handleServerPowerOff(ctx, adminCb, res.ID, adminID); err != nil {
		t.Fatalf("admin PowerOff failed: %v", err)
	}
	if spy.powerOffCalls != 1 {
		t.Fatalf("expected 1 PowerOff call by admin, got %d", spy.powerOffCalls)
	}
	resCurrent, _ := store.Billing().GetResource(ctx, res.ID)
	if resCurrent.PowerState != billing.PowerStateOff {
		t.Fatalf("expected power state off, got %s", resCurrent.PowerState)
	}

	// 2. Admin Power On
	if err := svc.handleServerPowerOn(ctx, adminCb, res.ID, adminID); err != nil {
		t.Fatalf("admin PowerOn failed: %v", err)
	}
	if spy.powerOnCalls != 1 {
		t.Fatalf("expected 1 PowerOn call by admin, got %d", spy.powerOnCalls)
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.PowerState != billing.PowerStateRunning {
		t.Fatalf("expected power state running, got %s", resCurrent.PowerState)
	}

	// 3. Admin Reboot
	if err := svc.handleServerReboot(ctx, adminCb, res.ID, adminID); err != nil {
		t.Fatalf("admin Reboot failed: %v", err)
	}
	if spy.rebootCalls != 1 {
		t.Fatalf("expected 1 Reboot call by admin, got %d", spy.rebootCalls)
	}

	// 4. Admin Reset Password
	if err := svc.handleServerResetPassword(ctx, adminCb, res.ID, adminID); err != nil {
		t.Fatalf("admin ResetPassword failed: %v", err)
	}
	if spy.resetPwdCalls != 1 {
		t.Fatalf("expected 1 ResetPassword call by admin, got %d", spy.resetPwdCalls)
	}

	// 5. Admin Enable Rescue
	if err := svc.handleServerRescue(ctx, adminCb, res.ID, adminID); err != nil {
		t.Fatalf("admin EnableRescue failed: %v", err)
	}
	if spy.rescueCalls != 1 {
		t.Fatalf("expected 1 EnableRescue call by admin, got %d", spy.rescueCalls)
	}

	// 6. Admin Delete Prompt
	if err := svc.handleServerDeletePrompt(ctx, tu.ID(adminID), res.ID, adminID); err != nil {
		t.Fatalf("admin DeletePrompt failed: %v", err)
	}

	// 7. Admin Delete Confirm
	if err := svc.handleServerDeleteConfirm(ctx, adminCb, res.ID, adminID); err != nil {
		t.Fatalf("admin DeleteConfirm failed: %v", err)
	}
	if spy.deleteCalls != 1 {
		t.Fatalf("expected 1 DeleteServer call by admin, got %d", spy.deleteCalls)
	}
	resCurrent, _ = store.Billing().GetResource(ctx, res.ID)
	if resCurrent.Status != billing.ResourceStatusDeleted {
		t.Fatalf("expected status deleted, got %s", resCurrent.Status)
	}
}
