package order

import (
	"errors"
	"fmt"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// State represents order status in the finite state machine.
type State string

const (
	StateAwaitingPayment       State = "awaiting_payment"
	StateRequoteRequired       State = "requote_required"
	StateReceiptPending        State = "receipt_pending"
	StatePaid                  State = "paid"
	StateCheckoutCredited      State = "checkout_credited"
	StatePurchaseFailed        State = "purchase_failed"
	StateFulfilling            State = "fulfilling"
	StateDelivered             State = "delivered"
	StateFulfillmentFailed     State = "fulfillment_failed"
	StateOSReselectRequired    State = "os_reselect_required"
	StateRefundRequested       State = "refund_requested"
	StateRefundPendingApproval State = "refund_pending_owner_approval"
	StateRefundApproved        State = "refund_approved"
	StateRefundRejected        State = "refund_rejected"
	StateRefunded              State = "refunded"
	StateExpired               State = "expired"
	StateCancelled             State = "cancelled"
)

// CheckoutMode specifies payment method.
type CheckoutMode string

const (
	CheckoutModeDirect        CheckoutMode = "direct"
	CheckoutModeWallet        CheckoutMode = "wallet"
	CheckoutModeMonthCheckout CheckoutMode = "month_checkout"
)

var (
	ErrInvalidTransition = errors.New("invalid order state transition")
	ErrOrderExpired      = errors.New("order has expired")
	ErrOrderHeld         = errors.New("order is currently on hold")
)

// OrderEvent represents an append-only event recording state change.
type OrderEvent struct {
	ID         string
	OrderID    string
	FromStatus State
	ToStatus   State
	Source     string
	SourceRef  string
	CreatedAt  time.Time
}

// Order represents a purchase order.
type Order struct {
	ID                string
	UserID            int64
	Status            State
	CheckoutMode      CheckoutMode
	HoldReason        *string
	QuoteID           string
	LockedQuote       pricing.Quote
	CheckoutCreditIRT *pricing.IRT
	CardAccountID     *string
	PolicyVersion     string
	PolicyAcceptedAt  time.Time
	ExpiresAt         time.Time
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// IsOnHold returns true if order has an active hold reason.
func (o *Order) IsOnHold() bool {
	return o.HoldReason != nil && *o.HoldReason != ""
}

// Transition performs state transition, validating allowed graph edges.
func (o *Order) Transition(to State, source string, sourceRef string, now time.Time) (*OrderEvent, error) {
	if !isAllowedTransition(o.Status, to) {
		return nil, fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidTransition, o.Status, to)
	}

	event := &OrderEvent{
		OrderID:    o.ID,
		FromStatus: o.Status,
		ToStatus:   to,
		Source:     source,
		SourceRef:  sourceRef,
		CreatedAt:  now,
	}

	o.Status = to
	o.Version++
	o.UpdatedAt = now

	return event, nil
}

// isAllowedTransition implements the state diagram rules from Section 3.2.
func isAllowedTransition(from, to State) bool {
	switch from {
	case StateAwaitingPayment:
		return to == StateRequoteRequired ||
			to == StateReceiptPending ||
			to == StateExpired ||
			to == StateCancelled

	case StateRequoteRequired:
		return to == StateAwaitingPayment ||
			to == StateExpired ||
			to == StateCancelled

	case StateReceiptPending:
		return to == StateAwaitingPayment || // receipt rejected
			to == StatePaid || // direct order approved & debited
			to == StateCheckoutCredited || // month checkout approved
			to == StatePurchaseFailed // direct order approved, debit refused

	case StateCheckoutCredited:
		return to == StatePaid || // automatic purchase debited
			to == StatePurchaseFailed // purchase cannot complete

	case StatePurchaseFailed:
		return to == StatePaid || // user buys again / with credit
			to == StateRefundRequested // user requests refund of unconsumed credit

	case StatePaid:
		return to == StateFulfilling

	case StateFulfilling:
		return to == StateDelivered ||
			to == StateFulfillmentFailed ||
			to == StateOSReselectRequired

	case StateOSReselectRequired:
		return to == StateFulfilling || // user picks available version
			to == StateRefundRequested // 72 hours timeout without choice

	case StateFulfillmentFailed:
		return to == StateFulfilling || // admin retry
			to == StateRefundRequested // automatic refund request

	case StateDelivered:
		return to == StateRefundRequested // eligible user refund request

	case StateRefundRequested:
		return to == StateRefundPendingApproval

	case StateRefundPendingApproval:
		return to == StateRefundApproved ||
			to == StateRefundRejected

	case StateRefundApproved:
		return to == StateRefunded // TOTP-gated bank payout completed

	case StateExpired, StateCancelled, StateRefunded, StateRefundRejected:
		return false // terminal states

	default:
		return false
	}
}
