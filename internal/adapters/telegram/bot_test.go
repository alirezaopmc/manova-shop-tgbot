package telegram

import (
	"testing"
)

func TestFormatNumber(t *testing.T) {
	cases := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{100, "100"},
		{1000, "1,000"},
		{10500, "10,500"},
		{10500000, "10,500,000"},
	}

	for _, c := range cases {
		out := formatNumber(c.input)
		if out != c.expected {
			t.Fatalf("for %d expected %q, got %q", c.input, c.expected, out)
		}
	}
}

func TestPhoneStatusLabel(t *testing.T) {
	if phoneStatusLabel(true) != "🟢 تایید شده" {
		t.Fatalf("unexpected true label")
	}
	if phoneStatusLabel(false) != "🔴 تایید نشده (نیاز به احراز)" {
		t.Fatalf("unexpected false label")
	}
}

func TestParseAmountIRT(t *testing.T) {
	cases := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"100000", 100_000, false},
		{"500,000", 500_000, false},
		{"۵۰۰,۰۰۰", 500_000, false},
		{"۵۰۰۰۰۰ تومان", 500_000, false},
		{"۲۵۰ هزار", 250_000, false},
		{"100k", 100_000, false},
		{"2m", 2_000_000, false},
		{"۲ میلیون", 2_000_000, false},
		{"  ۷۵۰,۰۰۰ ت  ", 750_000, false},
		{"", 0, true},
		{"   ", 0, true},
		{"hello world", 0, true},
	}

	for _, c := range cases {
		out, err := parseAmountIRT(c.input)
		if c.wantErr {
			if err == nil {
				t.Fatalf("expected error for input %q, got %d", c.input, out)
			}
		} else {
			if err != nil || out != c.expected {
				t.Fatalf("for input %q expected %d, got %d (err: %v)", c.input, c.expected, out, err)
			}
		}
	}
}

func TestAmtLabel(t *testing.T) {
	if amtLabel(0) != "اعلام نشده" {
		t.Fatalf("expected 'اعلام نشده' for 0")
	}
	if amtLabel(500000) != "500,000 تومان" {
		t.Fatalf("expected '500,000 تومان' for 500000, got %q", amtLabel(500000))
	}
}

func TestPendingTopupSession(t *testing.T) {
	s := NewService(nil, nil, nil)
	userID := int64(987654)

	// Initially zero
	if amt := s.getPendingTopup(userID); amt != 0 {
		t.Fatalf("expected 0, got %d", amt)
	}

	// Set amount
	s.setPendingTopup(userID, 750000)
	if amt := s.getPendingTopup(userID); amt != 750000 {
		t.Fatalf("expected 750000, got %d", amt)
	}

	// Clear
	s.clearPendingTopup(userID)
	if amt := s.getPendingTopup(userID); amt != 0 {
		t.Fatalf("expected 0 after clear, got %d", amt)
	}
}
