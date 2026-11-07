package totp

import (
	"context"
	"testing"
	"time"
)

func TestRFC4226And6238TestVectors(t *testing.T) {
	// RFC 4226 / 6238 test secret: "12345678901234567890" in base32
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

	tests := []struct {
		name     string
		timeUnix int64
		expected string
	}{
		{
			name:     "step 0 (0s)",
			timeUnix: 0,
			expected: "755224",
		},
		{
			name:     "step 1 (59s)",
			timeUnix: 59,
			expected: "287082",
		},
		{
			name:     "step 4 (120s)",
			timeUnix: 120,
			expected: "338314",
		},
		{
			name:     "step 5 (179s)",
			timeUnix: 179,
			expected: "254676",
		},
		{
			name:     "step 6 (180s)",
			timeUnix: 180,
			expected: "287922",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := time.Unix(tt.timeUnix, 0).UTC()
			code, err := GenerateCode(secret, tm)
			if err != nil {
				t.Fatalf("GenerateCode failed: %v", err)
			}
			if code != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, code)
			}
		})
	}
}

func TestVerifierSkewWindow(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP" // standard base32 test key
	adminID := int64(123456789)

	v := NewVerifier()
	if err := v.RegisterSecret(adminID, secret); err != nil {
		t.Fatalf("failed to register secret: %v", err)
	}

	baseTime := time.Unix(1700000000, 0).UTC()
	v.SetClock(func() time.Time {
		return baseTime
	})

	ctx := context.Background()

	// 1. Current step (t)
	codeCurrent, err := v.GenerateCode(secret, baseTime)
	if err != nil {
		t.Fatalf("failed to generate code for current: %v", err)
	}
	valid, err := v.VerifyCode(ctx, adminID, codeCurrent)
	if err != nil || !valid {
		t.Errorf("expected code at t to be valid, got valid=%v, err=%v", valid, err)
	}

	// 2. Previous step (t - 30s) -> within skew window
	codePrev, err := v.GenerateCode(secret, baseTime.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("failed to generate code for t-30s: %v", err)
	}
	valid, err = v.VerifyCode(ctx, adminID, codePrev)
	if err != nil || !valid {
		t.Errorf("expected code at t-30s to be valid, got valid=%v, err=%v", valid, err)
	}

	// 3. Next step (t + 30s) -> within skew window
	codeNext, err := v.GenerateCode(secret, baseTime.Add(30*time.Second))
	if err != nil {
		t.Fatalf("failed to generate code for t+30s: %v", err)
	}
	valid, err = v.VerifyCode(ctx, adminID, codeNext)
	if err != nil || !valid {
		t.Errorf("expected code at t+30s to be valid, got valid=%v, err=%v", valid, err)
	}

	// 4. Expired code (t - 60s) -> outside skew window
	codeExpired, err := v.GenerateCode(secret, baseTime.Add(-60*time.Second))
	if err != nil {
		t.Fatalf("failed to generate code for t-60s: %v", err)
	}
	valid, err = v.VerifyCode(ctx, adminID, codeExpired)
	if err != nil {
		t.Errorf("unexpected error on expired code: %v", err)
	}
	if valid {
		t.Errorf("expected code at t-60s to be rejected, but was accepted")
	}

	// 5. Far future code (t + 60s) -> outside skew window
	codeFarFuture, err := v.GenerateCode(secret, baseTime.Add(60*time.Second))
	if err != nil {
		t.Fatalf("failed to generate code for t+60s: %v", err)
	}
	valid, err = v.VerifyCode(ctx, adminID, codeFarFuture)
	if err != nil {
		t.Errorf("unexpected error on far future code: %v", err)
	}
	if valid {
		t.Errorf("expected code at t+60s to be rejected, but was accepted")
	}
}

func TestVerifierRejections(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	adminID := int64(1001)

	v := NewVerifier()
	if err := v.RegisterSecret(adminID, secret); err != nil {
		t.Fatalf("failed to register secret: %v", err)
	}

	ctx := context.Background()

	// 1. Unregistered admin
	valid, err := v.VerifyCode(ctx, 9999999, "123456")
	if err != nil || valid {
		t.Errorf("expected unregistered admin to be rejected with valid=false, got valid=%v, err=%v", valid, err)
	}

	// 2. Empty code
	valid, err = v.VerifyCode(ctx, adminID, "")
	if err != nil || valid {
		t.Errorf("expected empty code to be rejected, got valid=%v, err=%v", valid, err)
	}

	// 3. Short code (< 6 digits)
	valid, err = v.VerifyCode(ctx, adminID, "12345")
	if err != nil || valid {
		t.Errorf("expected short code to be rejected, got valid=%v, err=%v", valid, err)
	}

	// 4. Long code (> 6 digits)
	valid, err = v.VerifyCode(ctx, adminID, "1234567")
	if err != nil || valid {
		t.Errorf("expected long code to be rejected, got valid=%v, err=%v", valid, err)
	}

	// 5. Non-numeric code
	valid, err = v.VerifyCode(ctx, adminID, "abcdef")
	if err != nil || valid {
		t.Errorf("expected non-numeric code to be rejected, got valid=%v, err=%v", valid, err)
	}

	// 6. Invalid 6-digit code
	baseTime := time.Unix(1700000000, 0).UTC()
	v.SetClock(func() time.Time { return baseTime })
	validCode, _ := v.GenerateCode(secret, baseTime)
	// Create an inverted or clearly wrong code
	wrongCode := "000000"
	if validCode == "000000" {
		wrongCode = "111111"
	}
	valid, err = v.VerifyCode(ctx, adminID, wrongCode)
	if err != nil || valid {
		t.Errorf("expected wrong code to be rejected, got valid=%v, err=%v", valid, err)
	}
}

func TestRegisterSecretValidation(t *testing.T) {
	v := NewVerifier()

	// Zero admin ID
	if err := v.RegisterSecret(0, "JBSWY3DPEHPK3PXP"); err == nil {
		t.Errorf("expected error for admin ID 0")
	}

	// Empty secret
	if err := v.RegisterSecret(123, ""); err == nil {
		t.Errorf("expected error for empty secret")
	}

	// Invalid base32 secret characters
	if err := v.RegisterSecret(123, "invalid!base32@123"); err == nil {
		t.Errorf("expected error for invalid base32 characters")
	}

	// Secret with spaces and lowercase (should succeed)
	if err := v.RegisterSecret(123, "jbsw y3dp ehpk 3pxp"); err != nil {
		t.Errorf("expected success for secret with spaces and lowercase, got: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	adminID := int64(1001)

	v := NewVerifier()
	_ = v.RegisterSecret(adminID, secret)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	valid, err := v.VerifyCode(ctx, adminID, "123456")
	if err == nil {
		t.Errorf("expected context cancellation error, got valid=%v, err=nil", valid)
	}
}
