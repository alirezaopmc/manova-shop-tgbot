package user

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
)

// Role defines user authorization tier.
type Role string

const (
	RoleUser  Role = "USER"
	RoleAdmin Role = "ADMIN"
)

var (
	ErrInvalidIranianNumber = errors.New("must be a valid Iranian mobile number (+989...)")
	ErrAlreadyVerified      = errors.New("phone number already verified")
)

// User represents an enrolled customer or administrator.
type User struct {
	TelegramUserID     int64
	Username           string
	FirstName          string
	LastName           string
	Role               Role
	Locale             string // default "fa"
	Segment            fee.Segment
	InvitedBy          *int64
	InviteCode         *string
	FirstPurchaseState fee.FirstPurchaseState
	PhoneVerifiedAt    *time.Time
	PhoneE164          *string
	PhoneHash          *string
	Blocked            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// New creates a new user entity with defaults.
func New(telegramID int64, username, firstName, lastName string, role Role, now time.Time) *User {
	return &User{
		TelegramUserID:     telegramID,
		Username:           username,
		FirstName:          firstName,
		LastName:           lastName,
		Role:               role,
		Locale:             "fa",
		Segment:            fee.SegmentNew,
		FirstPurchaseState: fee.FirstPurchaseEligible,
		Blocked:            false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// NormalizeIranianPhone formats raw phone into E.164 format (+989...).
func NormalizeIranianPhone(raw string) (string, error) {
	// Strip spaces, dashes, parentheses
	var sb strings.Builder
	for _, r := range raw {
		switch {
		case r >= '۰' && r <= '۹':
			sb.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩':
			sb.WriteRune('0' + (r - '٠'))
		case r >= '0' && r <= '9':
			sb.WriteRune(r)
		}
	}
	digits := sb.String()

	// Accept:
	// +989123456789 -> 12 digits starting 989
	// 00989123456789 -> strip 00 -> 989
	// 09123456789 -> 11 digits starting 09 -> replace 0 with 98 -> 989
	// 989123456789 -> 12 digits
	// 9123456789 -> 10 digits starting 9 -> prepend 98 -> 989

	if strings.HasPrefix(digits, "0098") {
		digits = strings.TrimPrefix(digits, "00")
	} else if strings.HasPrefix(digits, "0") {
		digits = "98" + strings.TrimPrefix(digits, "0")
	} else if len(digits) == 10 && strings.HasPrefix(digits, "9") {
		digits = "98" + digits
	}

	if len(digits) != 12 || !strings.HasPrefix(digits, "989") {
		return "", ErrInvalidIranianNumber
	}

	return "+" + digits, nil
}

// HashPhoneNumber generates keyed HMAC-SHA256 of E.164 phone number.
func HashPhoneNumber(phoneE164 string, secretKey []byte) string {
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(phoneE164))
	return hex.EncodeToString(mac.Sum(nil))
}

// SetVerifiedPhone registers verified contact number.
func (u *User) SetVerifiedPhone(phoneE164 string, phoneHash string, now time.Time) error {
	u.PhoneE164 = &phoneE164
	u.PhoneHash = &phoneHash
	u.PhoneVerifiedAt = &now
	u.UpdatedAt = now
	return nil
}

// IsPhoneVerified returns true if user has shared verified mobile contact.
func (u *User) IsPhoneVerified() bool {
	return u.PhoneVerifiedAt != nil && u.PhoneE164 != nil && *u.PhoneE164 != ""
}
