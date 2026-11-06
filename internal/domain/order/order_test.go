package order

import (
	"testing"
	"time"
)

func TestOrderLifecycleDirectOrder(t *testing.T) {
	now := time.Now()
	o := &Order{
		ID:        "order-123",
		Status:    StateAwaitingPayment,
		Version:   1,
		CreatedAt: now,
	}

	// 1. Submit receipt
	evt, err := o.Transition(StateReceiptPending, "telegram", "msg-1", now)
	if err != nil || o.Status != StateReceiptPending || o.Version != 2 {
		t.Fatalf("expected transition to receipt_pending, err %v", err)
	}
	if evt.FromStatus != StateAwaitingPayment || evt.ToStatus != StateReceiptPending {
		t.Fatalf("unexpected event: %+v", evt)
	}

	// 2. Receipt approved -> Paid
	_, err = o.Transition(StatePaid, "admin", "decision-1", now)
	if err != nil || o.Status != StatePaid {
		t.Fatalf("expected transition to paid, err %v", err)
	}

	// 3. Paid -> Fulfilling
	_, err = o.Transition(StateFulfilling, "job", "provision-1", now)
	if err != nil || o.Status != StateFulfilling {
		t.Fatalf("expected transition to fulfilling, err %v", err)
	}

	// 4. Delivered
	_, err = o.Transition(StateDelivered, "provider", "action-1", now)
	if err != nil || o.Status != StateDelivered {
		t.Fatalf("expected transition to delivered, err %v", err)
	}

	// 5. Cannot transition from delivered directly to paid
	_, err = o.Transition(StatePaid, "invalid", "x", now)
	if err == nil {
		t.Fatalf("expected error on invalid transition from delivered to paid")
	}
}

func TestOrderMonthCheckoutFlow(t *testing.T) {
	now := time.Now()
	o := &Order{
		ID:           "order-month",
		Status:       StateAwaitingPayment,
		CheckoutMode: CheckoutModeMonthCheckout,
	}

	// Receipt submitted
	_, err := o.Transition(StateReceiptPending, "user", "rcpt-1", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Receipt approved -> CheckoutCredited
	_, err = o.Transition(StateCheckoutCredited, "admin", "approve-1", now)
	if err != nil || o.Status != StateCheckoutCredited {
		t.Fatalf("expected checkout_credited, got %s err %v", o.Status, err)
	}

	// Purchase debit succeeded -> Paid
	_, err = o.Transition(StatePaid, "checkout_purchase_job", "job-1", now)
	if err != nil || o.Status != StatePaid {
		t.Fatalf("expected paid, got %s err %v", o.Status, err)
	}
}

func TestRefundStateGraph(t *testing.T) {
	now := time.Now()
	o := &Order{
		ID:     "order-refund",
		Status: StateDelivered,
	}

	// User requests refund
	_, err := o.Transition(StateRefundRequested, "user", "req-1", now)
	if err != nil || o.Status != StateRefundRequested {
		t.Fatalf("expected refund_requested")
	}

	// Sent to admin
	_, err = o.Transition(StateRefundPendingApproval, "system", "notify-1", now)
	if err != nil || o.Status != StateRefundPendingApproval {
		t.Fatalf("expected refund_pending_owner_approval")
	}

	// Admin approves (status notice, no money moves)
	_, err = o.Transition(StateRefundApproved, "admin", "approve-1", now)
	if err != nil || o.Status != StateRefundApproved {
		t.Fatalf("expected refund_approved")
	}

	// Owner pays out with TOTP -> Refunded (money moves)
	_, err = o.Transition(StateRefunded, "admin_totp", "bank-ref-1", now)
	if err != nil || o.Status != StateRefunded {
		t.Fatalf("expected refunded")
	}

	// Terminal state cannot transition
	_, err = o.Transition(StatePaid, "invalid", "x", now)
	if err == nil {
		t.Fatalf("terminal state should not allow transitions")
	}
}
