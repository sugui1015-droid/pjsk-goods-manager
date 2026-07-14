package recoveryemailverification

import (
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func TestVerificationCodeFormatAndBasicVariation(t *testing.T) {
	manager := testManager(t)
	seen := map[string]bool{}
	for range 100 {
		code, err := manager.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidCode(code) {
			t.Fatalf("generated code has invalid format")
		}
		seen[code] = true
	}
	if len(seen) < 90 {
		t.Fatalf("generated codes show insufficient basic variation: %d unique", len(seen))
	}
}

func TestVerificationHashIsScopedAndComparedSafely(t *testing.T) {
	manager := testManager(t)
	hash, err := manager.Hash("email-version-a", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "123456" || len(hash) != sha256HexLength {
		t.Fatal("verification hash has unsafe shape")
	}
	if !manager.Matches("email-version-a", "123456", hash) {
		t.Fatal("correct code did not match")
	}
	if manager.Matches("email-version-a", "654321", hash) {
		t.Fatal("wrong code matched")
	}
	if manager.Matches("email-version-b", "123456", hash) {
		t.Fatal("code hash was not scoped to the recovery email version")
	}
	if manager.Matches("email-version-a", "123456", "not-hex") {
		t.Fatal("invalid stored hash matched")
	}
}

func TestVerificationCodeValidationAndConfiguration(t *testing.T) {
	for _, value := range []string{"", "12345", "1234567", "12345a", " 123456", "123456 "} {
		if ValidCode(value) {
			t.Fatal("invalid verification code was accepted")
		}
	}
	if _, err := NewManager(make([]byte, 31)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("short HMAC key error = %v", err)
	}
	manager := testManager(t)
	if _, err := manager.Hash("", "123456"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("empty email version error = %v", err)
	}
}

func TestVerificationCodePropagatesRandomFailure(t *testing.T) {
	manager := testManager(t)
	manager.random = failingReader{}
	if _, err := manager.Generate(); err == nil {
		t.Fatal("random source failure was ignored")
	}
}

const sha256HexLength = 64

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func testManager(t *testing.T) *Manager {
	t.Helper()
	key := make([]byte, 48)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(key)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
