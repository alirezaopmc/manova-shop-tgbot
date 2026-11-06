package invite

import (
	"testing"
	"time"
)

func TestSingleUseInvite(t *testing.T) {
	now := time.Now()
	inv := New(100, 1, nil, now)

	if inv.Code == "" {
		t.Fatalf("expected non-empty invite code")
	}

	// First redemption by user 200 succeeds
	err := inv.Claim(200, now)
	if err != nil {
		t.Fatalf("expected first claim to succeed, got %v", err)
	}

	if inv.IsActive {
		t.Fatalf("single use invite should be inactive after 1 claim")
	}

	// Second redemption by user 300 fails
	err = inv.Claim(300, now)
	if err != ErrInviteUsed {
		t.Fatalf("expected ErrInviteUsed, got %v", err)
	}
}

func TestUnlimitedInvite(t *testing.T) {
	now := time.Now()
	inv := New(100, 0, nil, now)

	for u := int64(201); u <= 205; u++ {
		if err := inv.Claim(u, now); err != nil {
			t.Fatalf("expected claim %d to succeed, got %v", u, err)
		}
	}

	if !inv.IsActive || inv.UsesCount != 5 {
		t.Fatalf("expected active invite with 5 uses")
	}
}

func TestSelfRedeemRejected(t *testing.T) {
	now := time.Now()
	inv := New(100, 1, nil, now)

	err := inv.Claim(100, now)
	if err != ErrInviteSelf {
		t.Fatalf("expected ErrInviteSelf, got %v", err)
	}
}

func TestExpiredInvite(t *testing.T) {
	now := time.Now()
	dur := 1 * time.Hour
	inv := New(100, 5, &dur, now)

	// Claim before expiry
	err := inv.Claim(200, now.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("expected claim to succeed before expiry")
	}

	// Claim after expiry
	err = inv.Claim(300, now.Add(2*time.Hour))
	if err != ErrInviteExpired {
		t.Fatalf("expected ErrInviteExpired, got %v", err)
	}
}

func TestCleanCode(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"/start invite_AbCd1234", "abcd1234"},
		{"invite_XYZ789", "xyz789"},
		{"  FOOBAR  ", "foobar"},
		{"/start 1234abcd", "1234abcd"},
		{"/start ref_6246642516", "6246642516"},
		{"/start invite", "invite"},
	}

	for _, c := range cases {
		out := CleanCode(c.input)
		if out != c.expected {
			t.Fatalf("for %q expected %q, got %q", c.input, c.expected, out)
		}
	}
}
