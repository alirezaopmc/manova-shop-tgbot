package refund

import (
	"testing"
	"time"
)

func TestRefundMath(t *testing.T) {
	// Base 100,000 IRT
	comm, pay, err := CalculateRefundMath(100_000)
	if err != nil || comm != 10_000 || pay != 90_000 {
		t.Fatalf("expected 10,000 comm and 90,000 payout, got comm %d pay %d", comm, pay)
	}

	// Base 15 IRT -> 10% is 1.5 -> ceil is 2 IRT commission, 13 IRT payout
	comm, pay, err = CalculateRefundMath(15)
	if err != nil || comm != 2 || pay != 13 {
		t.Fatalf("expected 2 comm and 13 payout, got comm %d pay %d", comm, pay)
	}

	// Zero or negative base returns error
	_, _, err = CalculateRefundMath(0)
	if err != ErrInvalidRefundBase {
		t.Fatalf("expected ErrInvalidRefundBase")
	}
}

func TestRefundLifecycle(t *testing.T) {
	now := time.Now()
	req, err := NewRequest("ref-1", "order-1", TriggerFulfillmentFailed, 50_000, "1234", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Status != StatusRequested || req.PayoutIRT != 45_000 {
		t.Fatalf("unexpected init state: %+v", req)
	}

	// Payout cannot be executed before approval
	err = req.Payout(6246642516, "bank-tx-123", now)
	if err != ErrCannotPayout {
		t.Fatalf("expected ErrCannotPayout, got %v", err)
	}

	// Approve
	req.Approve(6246642516, now)
	if req.Status != StatusApproved || *req.ApprovedByAdminID != 6246642516 {
		t.Fatalf("expected approved state")
	}

	// Payout
	err = req.Payout(6246642516, "bank-tx-123", now)
	if err != nil || req.Status != StatusPaidOut || *req.BankReference != "bank-tx-123" {
		t.Fatalf("expected paid out state, got err %v", err)
	}
}
