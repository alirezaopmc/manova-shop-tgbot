package secrets

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
)

func TestSecretRedaction(t *testing.T) {
	s := New("super-sensitive-token")

	if s.Expose() != "super-sensitive-token" {
		t.Fatalf("expected expose to return token, got %s", s.Expose())
	}

	if s.String() != Redacted {
		t.Fatalf("expected %s, got %s", Redacted, s.String())
	}

	if fmt.Sprintf("%v", s) != Redacted {
		t.Fatalf("expected %s, got %s", Redacted, fmt.Sprintf("%v", s))
	}

	if fmt.Sprintf("%#v", s) != Redacted {
		t.Fatalf("expected %s, got %s", Redacted, fmt.Sprintf("%#v", s))
	}

	marshaled, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	expectedJSON := fmt.Sprintf("%q", Redacted)
	if string(marshaled) != expectedJSON {
		t.Fatalf("expected json %s, got %s", expectedJSON, string(marshaled))
	}

	lv := s.LogValue()
	if lv.Kind() != slog.KindString || lv.String() != Redacted {
		t.Fatalf("expected slog string %s, got %v", Redacted, lv)
	}

	empty := New("")
	if !empty.IsZero() {
		t.Fatalf("expected empty secret to be zero")
	}
}
