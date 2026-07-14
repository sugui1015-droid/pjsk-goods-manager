package querycoderecovery

import (
	"bytes"
	"errors"
	"testing"
)

func TestManagerCodePurposeAndBinding(t *testing.T) {
	manager, err := NewManager(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	code, err := manager.GenerateCode()
	if err != nil || !ValidCode(code) {
		t.Fatal("generated recovery code has invalid format")
	}
	hash, err := manager.HashCode("user-a", "email-a", "CN A", code)
	if err != nil {
		t.Fatal(err)
	}
	if !manager.MatchesCode("user-a", "email-a", " cn   a ", code, hash) {
		t.Fatal("expected bound code to match")
	}
	if manager.MatchesCode("user-b", "email-a", "CN A", code, hash) ||
		manager.MatchesCode("user-a", "email-b", "CN A", code, hash) ||
		manager.MatchesCode("user-a", "email-a", "CN B", code, hash) {
		t.Fatal("code crossed a user, email, or CN boundary")
	}
	if manager.MatchesCode("user-a", "email-a", "CN A", "000000", "not-hex") {
		t.Fatal("invalid stored hash matched")
	}
}

func TestManagerResetTokenIsRandomScopedAndValidated(t *testing.T) {
	manager, err := NewManager(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.GenerateToken()
	if err != nil || !ValidToken(first) {
		t.Fatal("generated reset token has invalid format")
	}
	second, err := manager.GenerateToken()
	if err != nil || first == second {
		t.Fatal("reset tokens were not independently generated")
	}
	firstHash, err := manager.HashToken(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := manager.HashToken(second)
	if err != nil || firstHash == secondHash {
		t.Fatal("reset-token hashes were not isolated")
	}
	if _, err := manager.HashToken("invalid"); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("invalid reset token was accepted")
	}
}

func TestManagerIdentifierHashAndRandomFailure(t *testing.T) {
	manager, err := NewManager(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cnHash, err := manager.IdentifierHash("cn", "NORMALIZED-CN")
	if err != nil {
		t.Fatal(err)
	}
	ipHash, err := manager.IdentifierHash("ip", "127.0.0.1")
	if err != nil || cnHash == ipHash {
		t.Fatal("identifier hash contexts were not isolated")
	}
	manager.random = errorReader{}
	if _, err := manager.GenerateCode(); err == nil {
		t.Fatal("code generation did not propagate random-source failure")
	}
	if _, err := manager.GenerateToken(); err == nil {
		t.Fatal("token generation did not propagate random-source failure")
	}
}

func TestManagerRejectsShortKey(t *testing.T) {
	if _, err := NewManager(make([]byte, 31)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("short HMAC key was accepted")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }
