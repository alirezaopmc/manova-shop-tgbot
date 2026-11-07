package crypto

import (
	"strings"
	"testing"
)

func TestEnvelopeEncryptDecrypt(t *testing.T) {
	kek := "super-secret-kek-value-12345"
	plaintexts := []string{
		"root-password-999",
		"a",
		"complex-pass!@#$%^&*()_+~`|}{[]:;?><,./",
		"رمزعبور_فارسی_۱۲۳۴",
		strings.Repeat("long-secret-key-material-", 100),
	}

	for _, pt := range plaintexts {
		enc, err := Encrypt(pt, kek)
		if err != nil {
			t.Fatalf("Encrypt(%q) failed: %v", pt, err)
		}
		if enc == pt {
			t.Fatalf("expected encrypted text to differ from plaintext, got: %s", enc)
		}
		if !strings.Contains(enc, ":") {
			t.Fatalf("expected enc to be in nonce:ciphertext format, got: %s", enc)
		}

		dec, err := Decrypt(enc, kek)
		if err != nil {
			t.Fatalf("Decrypt(%q) failed: %v", enc, err)
		}
		if dec != pt {
			t.Fatalf("expected decrypted text %q, got %q", pt, dec)
		}
	}
}

func TestEnvelopeEmptyKEK(t *testing.T) {
	pt := "plaintext-without-kek"
	enc, err := Encrypt(pt, "")
	if err != nil {
		t.Fatalf("unexpected error with empty kek: %v", err)
	}
	if enc != pt {
		t.Fatalf("expected plaintext when kek is empty, got %q", enc)
	}

	dec, err := Decrypt(enc, "")
	if err != nil {
		t.Fatalf("unexpected error with empty kek: %v", err)
	}
	if dec != pt {
		t.Fatalf("expected decrypted == plaintext when kek is empty, got %q", dec)
	}
}

func TestEnvelopeEmptyText(t *testing.T) {
	kek := "my-kek"
	enc, err := Encrypt("", kek)
	if err != nil {
		t.Fatalf("unexpected error encrypting empty text: %v", err)
	}
	if enc != "" {
		t.Fatalf("expected empty string from empty plaintext, got %q", enc)
	}

	dec, err := Decrypt("", kek)
	if err != nil {
		t.Fatalf("unexpected error decrypting empty text: %v", err)
	}
	if dec != "" {
		t.Fatalf("expected empty string from empty ciphertext, got %q", dec)
	}
}

func TestEnvelopeNonceRandomness(t *testing.T) {
	kek := "random-kek"
	pt := "same-plaintext"

	enc1, err1 := Encrypt(pt, kek)
	enc2, err2 := Encrypt(pt, kek)
	if err1 != nil || err2 != nil {
		t.Fatalf("encrypt failed: %v, %v", err1, err2)
	}
	if enc1 == enc2 {
		t.Fatalf("expected different ciphertexts for successive encryptions, got identical: %s", enc1)
	}

	dec1, err := Decrypt(enc1, kek)
	if err != nil || dec1 != pt {
		t.Fatalf("dec1 failed: %v, got %s", err, dec1)
	}
	dec2, err := Decrypt(enc2, kek)
	if err != nil || dec2 != pt {
		t.Fatalf("dec2 failed: %v, got %s", err, dec2)
	}
}

func TestEnvelopeWrongKEK(t *testing.T) {
	kek1 := "original-kek"
	kek2 := "different-kek"
	pt := "confidential-data"

	enc, err := Encrypt(pt, kek1)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	_, err = Decrypt(enc, kek2)
	if err == nil {
		t.Fatalf("expected error decrypting with wrong KEK, got nil")
	}
}

func TestEnvelopeTamperedCiphertext(t *testing.T) {
	kek := "my-kek"
	pt := "secret-message"

	enc, err := Encrypt(pt, kek)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	parts := strings.Split(enc, ":")
	if len(parts) != 2 {
		t.Fatalf("invalid format: %s", enc)
	}

	// Tamper ciphertext
	tampered := parts[0] + ":" + parts[1][:len(parts[1])-2] + "AA"
	_, err = Decrypt(tampered, kek)
	if err == nil {
		t.Fatalf("expected error on tampered ciphertext, got nil")
	}

	// Tamper nonce
	tamperedNonce := "AAAA" + parts[0][4:] + ":" + parts[1]
	_, err = Decrypt(tamperedNonce, kek)
	if err == nil {
		t.Fatalf("expected error on tampered nonce, got nil")
	}
}

func TestEnvelopeInvalidFormat(t *testing.T) {
	kek := "my-kek"

	invalidInputs := []string{
		"not-valid-base64:$$$",
		"$$$:not-valid-base64",
		"short",
	}

	for _, in := range invalidInputs {
		_, err := Decrypt(in, kek)
		if err == nil {
			t.Errorf("expected error for invalid input %q, got nil", in)
		}
	}
}
