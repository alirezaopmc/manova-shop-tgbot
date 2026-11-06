package clock

import (
	"testing"
	"time"
)

func TestMockClock(t *testing.T) {
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	clk := NewMockClock(base)

	if !clk.Now().Equal(base) {
		t.Fatalf("expected %v, got %v", base, clk.Now())
	}

	clk.Advance(5 * time.Minute)
	expected := base.Add(5 * time.Minute)
	if !clk.Now().Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, clk.Now())
	}

	if clk.Since(base) != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", clk.Since(base))
	}

	newBase := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	clk.Set(newBase)
	if !clk.Now().Equal(newBase) {
		t.Fatalf("expected %v, got %v", newBase, clk.Now())
	}
}

func TestRealClock(t *testing.T) {
	clk := NewRealClock()
	t1 := clk.Now()
	time.Sleep(10 * time.Millisecond)
	since := clk.Since(t1)
	if since < 10*time.Millisecond {
		t.Fatalf("expected >= 10ms, got %v", since)
	}
}
