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
