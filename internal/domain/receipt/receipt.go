package receipt

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

// Purpose defines what the receipt pays for.
type Purpose string

const (
	PurposeWalletTopup Purpose = "wallet_topup"
	PurposeOrder       Purpose = "order"
	PurposeUSDTopup    Purpose = "usd_topup"
)

// Status tracks receipt review state.
type Status string

const (
	StatusSubmitted     Status = "submitted"
	StatusPendingReview Status = "pending_review"
	StatusApproved      Status = "approved"
	StatusRejected      Status = "rejected"
	StatusWithdrawn     Status = "withdrawn"
	StatusAutoClosed    Status = "auto_closed"
)

// RejectReason defines standardized rejection categories.
type RejectReason string

const (
	RejectUnreadable     RejectReason = "unreadable"
	RejectAmountMismatch RejectReason = "amount_mismatch"
	RejectCardNotOurs    RejectReason = "card_not_ours"
	RejectDuplicate      RejectReason = "duplicate"
	RejectOther          RejectReason = "other"
)

var (
	ErrEmptyImageHash        = errors.New("receipt image hash cannot be empty")
	ErrEmptyTrackingCode     = errors.New("receipt tracking code cannot be empty")
	ErrAmountBelowMinimum    = errors.New("receipt amount is below minimum allowed")
	ErrAmountAboveMaximum    = errors.New("receipt amount exceeds maximum allowed")
	ErrReceiptAlreadyDecided = errors.New("receipt is already decided")
	ErrPhoneNotVerified      = errors.New("phone verification required before submitting receipts")
)

// Receipt models a submitted payment proof.
type Receipt struct {
	ID               string
	UserID           int64
	Purpose          Purpose
	OrderID          *string
	QuoteID          *string
	AmountIRR        pricing.IRR
	AmountIRT        pricing.IRT
	TrackingCodeNorm string
	ImageSHA256      string
	ImageRef         string
	Status           Status
	DecidedByAdminID *int64
	DecidedAt        *time.Time
	RejectReason     *RejectReason
	Note             *string
	CreatedAt        time.Time
}

// NormalizeTrackingCode converts Persian and Arabic digits to Latin and removes whitespace and punctuation.
func NormalizeTrackingCode(raw string) string {
	var sb strings.Builder
	for _, r := range raw {
		switch {
		case r >= '۰' && r <= '۹': // Persian digits
			sb.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩': // Arabic-Indic digits
			sb.WriteRune('0' + (r - '٠'))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			sb.WriteRune(unicode.ToUpper(r))
		}
	}
	return sb.String()
}

// ValidateIntake verifies input parameters before persisting receipt.
func ValidateIntake(
	amountIRR pricing.IRR,
	trackingCode string,
	imageHash string,
	minIRR pricing.IRR,
	maxIRR pricing.IRR,
	phoneVerified bool,
) (pricing.IRT, string, error) {
	if !phoneVerified {
		return 0, "", ErrPhoneNotVerified
	}
	if strings.TrimSpace(imageHash) == "" {
		return 0, "", ErrEmptyImageHash
	}
	normCode := NormalizeTrackingCode(trackingCode)
	if normCode == "" {
		return 0, "", ErrEmptyTrackingCode
	}

	irt, err := pricing.IRRToIRT(amountIRR)
	if err != nil {
		return 0, "", err
	}

	if amountIRR < minIRR {
		return 0, "", ErrAmountBelowMinimum
	}
	if amountIRR > maxIRR {
		return 0, "", ErrAmountAboveMaximum
	}

	return irt, normCode, nil
}

// Approve marks receipt approved.
func (r *Receipt) Approve(adminID int64, now time.Time) error {
	if r.Status != StatusPendingReview && r.Status != StatusSubmitted {
		return ErrReceiptAlreadyDecided
	}
	r.Status = StatusApproved
	r.DecidedByAdminID = &adminID
	r.DecidedAt = &now
	return nil
}

// Reject marks receipt rejected with a standard reason.
func (r *Receipt) Reject(adminID int64, reason RejectReason, note string, now time.Time) error {
	if r.Status != StatusPendingReview && r.Status != StatusSubmitted {
		return ErrReceiptAlreadyDecided
	}
	r.Status = StatusRejected
	r.DecidedByAdminID = &adminID
	r.DecidedAt = &now
	r.RejectReason = &reason
	if note != "" {
		r.Note = &note
	}
	return nil
}
