package clock

import "time"

// Clock abstracts system time for deterministic testing.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// RealClock uses standard time package functions.
type RealClock struct{}

func NewRealClock() *RealClock {
	return &RealClock{}
}

func (RealClock) Now() time.Time {
	return time.Now().UTC()
}

func (c RealClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

// MockClock provides controllable time for tests.
type MockClock struct {
	current time.Time
}

func NewMockClock(initial time.Time) *MockClock {
	return &MockClock{current: initial.UTC()}
}

func (m *MockClock) Now() time.Time {
	return m.current
}

func (m *MockClock) Since(t time.Time) time.Duration {
	return m.current.Sub(t)
}

func (m *MockClock) Set(t time.Time) {
	m.current = t.UTC()
}

func (m *MockClock) Advance(d time.Duration) {
	m.current = m.current.Add(d)
}
