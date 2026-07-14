package recoveryemail

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trim and lower domain", input: "  Alice.Tag@EXAMPLE.COM  ", want: "Alice.Tag@example.com"},
		{name: "preserve local case", input: "CaseSensitive@example.org", want: "CaseSensitive@example.org"},
		{name: "single-character local", input: "a@example.com", want: "a@example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Normalize(test.input)
			if err != nil || got != test.want {
				t.Fatalf("Normalize() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestNormalizeRejectsInvalidBoundaries(t *testing.T) {
	longLocal := strings.Repeat("a", maxLocalBytes+1) + "@example.com"
	longEmail := "a@" + strings.Repeat("b", maxEmailBytes) + ".example.com"
	values := []string{
		"a b@example.com",
		"a\nb@example.com",
		"a\x00b@example.com",
		"a@example",
		".a@example.com",
		"a..b@example.com",
		"a@\u4f8b\u5b50.example.com",
		longLocal,
		longEmail,
	}
	for _, value := range values {
		if got, err := Normalize(value); err == nil || got != "" {
			t.Fatalf("invalid email was accepted (length %d)", len(value))
		}
	}
}

func TestMask(t *testing.T) {
	for input, want := range map[string]string{
		"a@example.com":     "a***@example.com",
		"ab@example.com":    "a***@example.com",
		"alice@example.com": "a***@example.com",
	} {
		got, err := Mask(input)
		if err != nil || got != want {
			t.Fatalf("Mask(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, invalid := range []string{"", "not-an-email", "用户@example.com"} {
		if got, err := Mask(invalid); err == nil || got != "" {
			t.Fatalf("invalid mask input leaked data: %q", got)
		}
	}
}

func TestProtectorEncryptsRandomlyAndIndexesDeterministically(t *testing.T) {
	protector := newTestProtector(t)
	first, err := protector.Protect("Alice@EXAMPLE.COM")
	if err != nil {
		t.Fatal(err)
	}
	second, err := protector.Protect("Alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Encrypted, second.Encrypted) {
		t.Fatal("same email produced identical ciphertext")
	}
	if bytes.Contains(first.Encrypted, []byte("Alice@example.com")) || bytes.Contains(second.Encrypted, []byte("Alice@example.com")) {
		t.Fatal("ciphertext contains plaintext email")
	}
	if first.LookupHash != second.LookupHash {
		t.Fatal("same normalized email produced different blind indexes")
	}
	other, err := protector.Protect("Other@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if first.LookupHash == other.LookupHash {
		t.Fatal("different emails produced the same blind index")
	}
	masked, err := protector.MaskEncrypted(first.Encrypted)
	if err != nil || masked != "A***@example.com" {
		t.Fatalf("MaskEncrypted() = %q, %v", masked, err)
	}
}

func TestProtectorRejectsMissingInvalidKeysAndCorruptData(t *testing.T) {
	if _, err := NewProtector(nil, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing keys error = %v", err)
	}
	if _, err := NewProtector(randomBytes(t, 31), randomBytes(t, 32)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("short encryption key error = %v", err)
	}
	if _, err := NewProtector(randomBytes(t, 32), randomBytes(t, 31)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("short hmac key error = %v", err)
	}
	protector := newTestProtector(t)
	if value, err := protector.MaskEncrypted([]byte("corrupt")); err == nil || value != "" {
		t.Fatalf("corrupt ciphertext revealed %q", value)
	}
}

func newTestProtector(t *testing.T) *Protector {
	t.Helper()
	protector, err := NewProtector(randomBytes(t, 32), randomBytes(t, 48))
	if err != nil {
		t.Fatal(err)
	}
	return protector
}

func randomBytes(t *testing.T, size int) []byte {
	t.Helper()
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return value
}
