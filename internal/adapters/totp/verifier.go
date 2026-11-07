package totp

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
)

var _ port.TotpVerifier = (*Verifier)(nil)

// Verifier implements RFC 6238 TOTP verification with HMAC-SHA1, 30-second steps,
// 6 digits, and a ±1 step skew window.
type Verifier struct {
	mu      sync.RWMutex
	secrets map[int64]string
	clock   func() time.Time
}

// NewVerifier initializes a new TOTP verifier.
func NewVerifier() *Verifier {
	return &Verifier{
		secrets: make(map[int64]string),
		clock:   time.Now,
	}
}

// SetClock sets a custom clock function (useful for deterministic unit tests).
func (v *Verifier) SetClock(clock func() time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if clock != nil {
		v.clock = clock
	}
}

// RegisterSecret associates an admin Telegram ID with a base32-encoded TOTP secret.
func (v *Verifier) RegisterSecret(adminID int64, base32Secret string) error {
	if adminID == 0 {
		return errors.New("admin ID must not be zero")
	}
	if _, err := decodeBase32(base32Secret); err != nil {
		return fmt.Errorf("invalid base32 secret: %w", err)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.secrets[adminID] = base32Secret
	return nil
}

// VerifyCode verifies a 6-digit TOTP code for the given admin ID against their registered secret.
// It checks current time window and ±1 step skew window (30s before and 30s after).
func (v *Verifier) VerifyCode(ctx context.Context, adminID int64, code string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false, nil
	}
	for i := 0; i < 6; i++ {
		if code[i] < '0' || code[i] > '9' {
			return false, nil
		}
	}

	v.mu.RLock()
	secret, exists := v.secrets[adminID]
	clock := v.clock
	v.mu.RUnlock()

	if !exists || secret == "" {
		return false, nil
	}

	key, err := decodeBase32(secret)
	if err != nil {
		return false, err
	}

	var now time.Time
	if clock != nil {
		now = clock()
	} else {
		now = time.Now()
	}

	currStep := now.Unix() / 30
	skewSteps := []int64{currStep - 1, currStep, currStep + 1}

	for _, step := range skewSteps {
		expected, err := generateHOTP(key, step)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return true, nil
		}
	}

	return false, nil
}

// GenerateCode computes a 6-digit TOTP code for the given secret at time t.
func (v *Verifier) GenerateCode(secret string, t time.Time) (string, error) {
	return GenerateCode(secret, t)
}

// GenerateCode computes a 6-digit TOTP code for the given base32 secret at time t.
func GenerateCode(secret string, t time.Time) (string, error) {
	key, err := decodeBase32(secret)
	if err != nil {
		return "", err
	}
	step := t.Unix() / 30
	return generateHOTP(key, step)
}

func decodeBase32(secret string) ([]byte, error) {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	if clean == "" {
		return nil, errors.New("empty secret")
	}
	if rem := len(clean) % 8; rem != 0 {
		clean += strings.Repeat("=", 8-rem)
	}
	return base32.StdEncoding.DecodeString(clean)
}

func generateHOTP(key []byte, counter int64) (string, error) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))

	mac := hmac.New(sha1.New, key)
	if _, err := mac.Write(buf[:]); err != nil {
		return "", err
	}
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	binaryCode := ((int(h[offset]) & 0x7f) << 24) |
		((int(h[offset+1]) & 0xff) << 16) |
		((int(h[offset+2]) & 0xff) << 8) |
		(int(h[offset+3]) & 0xff)

	otp := binaryCode % 1_000_000
	return fmt.Sprintf("%06d", otp), nil
}
