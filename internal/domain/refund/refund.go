package refund

import (
	"errors"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// Trigger specifies why a refund was initiated.
type Trigger string

const (
	TriggerFailedPurchase       Trigger = "failed_purchase"
	TriggerFulfillmentFailed    Trigger = "fulfillment_failed"
	TriggerOSReselectTimeout    Trigger = "os_reselect_timeout"
	TriggerDeliveredUserRequest Trigger = "delivered_user_request"
	TriggerUnusedCredit         Trigger = "unused_credit"
)

// Status tracks refund request lifecycle.
type Status string

const (
	StatusRequested            Status = "requested"
	StatusPendingOwnerApproval Status = "pending_owner_approval"
	StatusApproved             Status = "approved"
	StatusPaidOut              Status = "paid_out"
	StatusRejected             Status = "rejected"
)

const CommissionPercent = 10

var (
	ErrInvalidRefundBase = errors.New("refund base amount must be positive")
	ErrCannotPayout      = errors.New("cannot pay out refund that is not approved")
)

// Request models a refund record.
type Request struct {
	ID                string
	OrderID           string
	Trigger           Trigger
	BaseIRT           pricing.IRT
	CommissionIRT     pricing.IRT
	PayoutIRT         pricing.IRT
	CardLast4         string
	Status            Status
	RequestedAt       time.Time
	ApprovedByAdminID *int64
	ApprovedAt        *time.Time
	RejectedByAdminID *int64
	RejectedAt        *time.Time
	RejectReason      *string
	BankReference     *string
	PaidOutByAdminID  *int64
	PaidOutAt         *time.Time
}

// CalculateRefundMath computes 10% commission and payout amount:
// commission_irt = ceil(base_irt * 10 / 100)
// payout_irt = base_irt - commission_irt
func CalculateRefundMath(baseIRT pricing.IRT) (commission pricing.IRT, payout pricing.IRT, err error) {
	if baseIRT <= 0 {
		return 0, 0, ErrInvalidRefundBase
	}
	// ceil division: (base * 10 + 99) / 100
	num := int64(baseIRT)*int64(CommissionPercent) + 99
	den := int64(100)
	comm := pricing.IRT(num / den)

	pay := baseIRT - comm
	return comm, pay, nil
}

// NewRequest creates a refund request in requested state.
func NewRequest(id, orderID string, trigger Trigger, baseIRT pricing.IRT, cardLast4 string, now time.Time) (*Request, error) {
	comm, pay, err := CalculateRefundMath(baseIRT)
	if err != nil {
		return nil, err
	}

	return &Request{
		ID:            id,
		OrderID:       orderID,
		Trigger:       trigger,
		BaseIRT:       baseIRT,
		CommissionIRT: comm,
		PayoutIRT:     pay,
		CardLast4:     cardLast4,
		Status:        StatusRequested,
		RequestedAt:   now,
	}, nil
}

// Approve sets status to approved. Notice: does NOT move money.
func (r *Request) Approve(adminID int64, now time.Time) {
	r.Status = StatusApproved
	r.ApprovedByAdminID = &adminID
	r.ApprovedAt = &now
}

// Reject sets status to rejected with a reason. Notice: does NOT move money.
func (r *Request) Reject(adminID int64, reason string, now time.Time) {
	r.Status = StatusRejected
	r.RejectedByAdminID = &adminID
	r.RejectedAt = &now
	r.RejectReason = &reason
}

// Payout marks the refund paid out after TOTP verification. Moves money!
func (r *Request) Payout(adminID int64, bankRef string, now time.Time) error {
	if r.Status != StatusApproved {
		return ErrCannotPayout
	}
	r.Status = StatusPaidOut
	r.PaidOutByAdminID = &adminID
	r.BankReference = &bankRef
	r.PaidOutAt = &now
	return nil
}
