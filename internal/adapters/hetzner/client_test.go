package hetzner

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/secrets"
)

func TestSimulatorLifecycle(t *testing.T) {
	ctx := context.Background()
	client := NewClient(Config{SimulationMode: true})

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
	client := NewClient(Config{SimulationMode: true})

	// Create with SSH key -> root password should be empty
	res, err := client.CreateServer(ctx, "test-srv-ssh", "cx33", "debian-12", "fsn1", []string{"my-key-id"})
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if res.RootPassword != "" {
		t.Errorf("expected empty root password when SSH key provided, got %s", res.RootPassword)
	}
}

func TestMissingTokenInProduction(t *testing.T) {
	ctx := context.Background()
	// When SimulationMode is false and token is empty -> must fail with ErrMissingToken
	client := NewClient(Config{Token: "", SimulationMode: false})

	_, err := client.CreateServer(ctx, "real-srv", "cx23", "ubuntu-24.04", "nbg1", nil)
	if err == nil || err != ErrMissingToken {
		t.Fatalf("expected ErrMissingToken, got %v", err)
	}

	if err := client.PowerOn(ctx, "123"); err == nil || err != ErrMissingToken {
		t.Fatalf("expected ErrMissingToken on PowerOn, got %v", err)
	}
}

func TestResourceCaching(t *testing.T) {
	ctx := context.Background()
	client := NewClient(Config{SimulationMode: true})

	// 1. Server types cache
	stList, err := client.ListServerTypes(ctx)
	if err != nil || len(stList) == 0 {
		t.Fatalf("ListServerTypes failed: %v", err)
	}

	// 2. Locations cache
	locList, err := client.ListLocations(ctx)
	if err != nil || len(locList) == 0 {
		t.Fatalf("ListLocations failed: %v", err)
	}

	// 3. Images cache
	imgList, err := client.ListImages(ctx)
	if err != nil || len(imgList) == 0 {
		t.Fatalf("ListImages failed: %v", err)
	}
}

func TestExternalIDValidation(t *testing.T) {
	ctx := context.Background()
	client := NewClient(Config{SimulationMode: true})

	invalidIDs := []string{
		"",
		"../servers/123",
		"123/actions/delete",
		"abc",
		"12a34",
		"-1",
		"123 456",
		"123;rm -rf",
		"../../admin",
		" 123",
		"123 ",
	}

	for _, invalidID := range invalidIDs {
		t.Run("ID_"+invalidID, func(t *testing.T) {
			if _, err := client.GetServer(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("GetServer expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if _, err := client.GetServerStatus(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("GetServerStatus expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.DeleteServer(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("DeleteServer expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.PowerOn(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("PowerOn expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.PowerOnServer(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("PowerOnServer expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.PowerOff(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("PowerOff expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.PowerOffServer(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("PowerOffServer expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.Shutdown(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("Shutdown expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.Reboot(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("Reboot expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.RebootServer(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("RebootServer expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if _, err := client.ResetPassword(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("ResetPassword expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if _, err := client.ResetRootPassword(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("ResetRootPassword expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if _, err := client.EnableRescue(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("EnableRescue expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
			if err := client.DisableRescue(ctx, invalidID); !errors.Is(err, ErrInvalidExternalID) {
				t.Errorf("DisableRescue expected ErrInvalidExternalID for %q, got: %v", invalidID, err)
			}
		})
	}

	// Valid numeric ID passes validation
	if _, err := client.GetServer(ctx, "999999"); err != nil {
		t.Fatalf("unexpected error for valid numeric ID: %v", err)
	}
}

func TestTokenSecretWrapping(t *testing.T) {
	// Secret[string] configuration
	tok := secrets.New("hcloud-super-secret-token")
	c1 := NewClient(Config{Token: tok})
	if c1.token.String() != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", c1.token.String())
	}
	if fmt.Sprintf("%v", c1.token) != "[REDACTED]" {
		t.Errorf("expected formatted [REDACTED], got %q", fmt.Sprintf("%v", c1.token))
	}
	if c1.token.Expose() != "hcloud-super-secret-token" {
		t.Errorf("unexpected exposed token: %q", c1.token.Expose())
	}
	if !c1.HasToken() {
		t.Errorf("expected HasToken true for non-empty token")
	}

	// Raw string configuration
	c2 := NewClient(Config{Token: "raw-string-token"})
	if c2.token.String() != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", c2.token.String())
	}
	if c2.token.Expose() != "raw-string-token" {
		t.Errorf("unexpected exposed token: %q", c2.token.Expose())
	}
	if !c2.HasToken() {
		t.Errorf("expected HasToken true for raw string token")
	}

	// Empty token without simulation
	c3 := NewClient(Config{SimulationMode: false})
	if c3.HasToken() {
		t.Errorf("expected HasToken false for zero token without simulation")
	}

	// Empty token with simulation
	c4 := NewClient(Config{SimulationMode: true})
	if !c4.HasToken() {
		t.Errorf("expected HasToken true in simulation mode")
	}
}
