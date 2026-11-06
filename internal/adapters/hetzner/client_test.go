package hetzner

import (
	"context"
	"testing"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
)

func TestSimulatorLifecycle(t *testing.T) {
	ctx := context.Background()
	client := NewClient(Config{Token: ""})

	// 1. Create Server with auto root password
	res, err := client.CreateServer(ctx, "test-srv-1", "cx23", "ubuntu-24.04", "nbg1", nil)
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if res.ExternalID == "" {
		t.Fatalf("expected non-empty external ID")
	}
	if res.RootPassword == "" {
		t.Fatalf("expected auto-generated root password")
	}
	if res.IPv4Address == "" || res.IPv6Address == "" {
		t.Fatalf("expected allocated IP addresses, got v4=%s v6=%s", res.IPv4Address, res.IPv6Address)
	}

	// 2. Get Server facts
	status, err := client.GetServer(ctx, res.ExternalID)
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if status.PowerState != billing.PowerStateRunning {
		t.Errorf("expected PowerStateRunning, got %s", status.PowerState)
	}

	// 3. Power Off
	if err := client.PowerOff(ctx, res.ExternalID); err != nil {
		t.Fatalf("PowerOff failed: %v", err)
	}
	status, _ = client.GetServer(ctx, res.ExternalID)
	if status.PowerState != billing.PowerStateOff {
		t.Errorf("expected PowerStateOff, got %s", status.PowerState)
	}

	// 4. Power On
	if err := client.PowerOn(ctx, res.ExternalID); err != nil {
		t.Fatalf("PowerOn failed: %v", err)
	}
	status, _ = client.GetServer(ctx, res.ExternalID)
	if status.PowerState != billing.PowerStateRunning {
		t.Errorf("expected PowerStateRunning, got %s", status.PowerState)
	}

	// 5. Reboot
	if err := client.Reboot(ctx, res.ExternalID); err != nil {
		t.Fatalf("Reboot failed: %v", err)
	}

	// 6. Reset Password
	newPwd, err := client.ResetPassword(ctx, res.ExternalID)
	if err != nil {
		t.Fatalf("ResetPassword failed: %v", err)
	}
	if newPwd == "" || newPwd == res.RootPassword {
		t.Errorf("expected fresh non-empty password")
	}

	// 7. Rescue Mode
	rescuePwd, err := client.EnableRescue(ctx, res.ExternalID)
	if err != nil {
		t.Fatalf("EnableRescue failed: %v", err)
	}
	if rescuePwd == "" {
		t.Errorf("expected rescue password")
	}

	if err := client.DisableRescue(ctx, res.ExternalID); err != nil {
		t.Fatalf("DisableRescue failed: %v", err)
	}

	// 8. Delete
	if err := client.DeleteServer(ctx, res.ExternalID); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}
}

func TestSimulatorWithSSHKey(t *testing.T) {
	ctx := context.Background()
	client := NewClient(Config{Token: "simulated"})

	// Create with SSH key -> root password should be empty
	res, err := client.CreateServer(ctx, "test-srv-ssh", "cx33", "debian-12", "fsn1", []string{"my-key-id"})
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if res.RootPassword != "" {
		t.Errorf("expected empty root password when SSH key provided, got %s", res.RootPassword)
	}
}
