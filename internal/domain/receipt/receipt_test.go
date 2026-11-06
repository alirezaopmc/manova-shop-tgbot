package receipt

import (
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

func TestNormalizeTrackingCode(t *testing.T) {
	// Persian digits with dash
	raw := "۱۲۳-۴۵۶-۷۸۹"
	norm := NormalizeTrackingCode(raw)
	if norm != "123456789" {
		t.Fatalf("expected 123456789, got %s", norm)
	}

	// Arabic digits with spaces
	raw = "  ١٢٣ ٤٥٦  "
	norm = NormalizeTrackingCode(raw)
	if norm != "123456" {
		t.Fatalf("expected 123456, got %s", norm)
	}
}

func TestValidateIntake(t *testing.T) {
	minIRR := pricing.IRR(1_000_000)
	maxIRR := pricing.IRR(500_000_000)

	// Valid
	irt, code, err := ValidateIntake(10_000_000, "TRK-999", "sha256hash", minIRR, maxIRR, true)
	if err != nil || irt != 1_000_000 || code != "TRK999" {
		t.Fatalf("expected valid intake, got irt %d code %s err %v", irt, code, err)
	}

	// Phone not verified
	_, _, err = ValidateIntake(10_000_000, "TRK-999", "sha256hash", minIRR, maxIRR, false)
	if err != ErrPhoneNotVerified {
		t.Fatalf("expected ErrPhoneNotVerified, got %v", err)
	}

	// Amount below min
	_, _, err = ValidateIntake(500_000, "TRK-999", "sha256hash", minIRR, maxIRR, true)
	if err != ErrAmountBelowMinimum {
		t.Fatalf("expected ErrAmountBelowMinimum, got %v", err)
	}

	// Amount above max
	_, _, err = ValidateIntake(600_000_000, "TRK-999", "sha256hash", minIRR, maxIRR, true)
	if err != ErrAmountAboveMaximum {
		t.Fatalf("expected ErrAmountAboveMaximum, got %v", err)
	}

	// Amount not multiple of 10
	_, _, err = ValidateIntake(10_000_005, "TRK-999", "sha256hash", minIRR, maxIRR, true)
	if err != pricing.ErrInvalidRialMultiple {
		t.Fatalf("expected ErrInvalidRialMultiple, got %v", err)
	}
}

func TestReceiptDecisions(t *testing.T) {
	now := time.Now()
	r := &Receipt{
		Status: StatusPendingReview,
	}

	// Approve
	err := r.Approve(6246642516, now)
	if err != nil || r.Status != StatusApproved {
		t.Fatalf("expected approved")
	}

	// Cannot reject after approved
	err = r.Reject(6246642516, RejectAmountMismatch, "", now)
	if err != ErrReceiptAlreadyDecided {
		t.Fatalf("expected ErrReceiptAlreadyDecided, got %v", err)
	}
}
