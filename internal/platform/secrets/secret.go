package secrets

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

const Redacted = "[REDACTED]"

// Secret wraps sensitive string types ensuring they are never printed in logs or errors.
type Secret[T ~string] struct {
	value T
}

func New[T ~string](val T) Secret[T] {
	return Secret[T]{value: val}
}

// Expose returns the raw secret value when explicitly required for external API calls.
func (s Secret[T]) Expose() T {
	return s.value
}

// IsZero reports whether the secret holds an empty value.
func (s Secret[T]) IsZero() bool {
	return s.value == ""
}

// String implements fmt.Stringer to prevent accidental leak in %s or %v.
func (s Secret[T]) String() string {
	return Redacted
}

// GoString implements fmt.GoStringer to prevent leak in %#v.
func (s Secret[T]) GoString() string {
	return Redacted
}

// MarshalJSON implements json.Marshaler returning "[REDACTED]".
func (s Secret[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(Redacted)
}

// LogValue implements slog.LogValuer returning "[REDACTED]".
func (s Secret[T]) LogValue() slog.Value {
	return slog.StringValue(Redacted)
}

// Format ensures custom format specifiers also yield [REDACTED].
func (s Secret[T]) Format(f fmt.State, c rune) {
	_, _ = f.Write([]byte(Redacted))
}
