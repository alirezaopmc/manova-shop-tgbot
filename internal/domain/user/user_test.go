package user

import (
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
)

func TestNormalizeIranianPhone(t *testing.T) {
	cases := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"09123456789", "+989123456789", false},
		{"+989123456789", "+989123456789", false},
		{"00989123456789", "+989123456789", false},
		{"989123456789", "+989123456789", false},
		{"9123456789", "+989123456789", false},
		{"۰۹۱۲۳۴۵۶۷۸۹", "+989123456789", false},
		{"0912-345-6789", "+989123456789", false},
		{"(0912) 345 6789", "+989123456789", false},
		{"02188888888", "", true},  // landline
		{"+15551234567", "", true}, // US number
		{"0912", "", true},         // too short
	}

	for _, c := range cases {
		out, err := NormalizeIranianPhone(c.input)
		if c.wantErr {
			if err == nil {
				t.Fatalf("expected error for input %q, got %q", c.input, out)
			}
		} else {
			if err != nil || out != c.expected {
				t.Fatalf("for input %q expected %q, got %q err %v", c.input, c.expected, out, err)
			}
		}
	}
}

func TestUserPhoneVerification(t *testing.T) {
	now := time.Now()
	u := New(12345, "alice", "Alice", "", RoleUser, now)

	if u.IsPhoneVerified() {
		t.Fatalf("new user should not be phone verified")
	}

	phone, err := NormalizeIranianPhone("09123456789")
	if err != nil {
		t.Fatalf("unexpected normalization error: %v", err)
	}

	secretKey := []byte("test-hmac-key")
	phoneHash := HashPhoneNumber(phone, secretKey)

	err = u.SetVerifiedPhone(phone, phoneHash, now)
	if err != nil {
		t.Fatalf("unexpected set phone error: %v", err)
	}

	if !u.IsPhoneVerified() || *u.PhoneE164 != "+989123456789" {
		t.Fatalf("expected user to be verified with +989123456789")
	}

	if u.Segment != fee.SegmentNew {
		t.Fatalf("expected SegmentNew default")
	}
}
