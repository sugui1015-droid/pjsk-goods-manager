package recoveryemailverification

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"
)

const (
	CodeLength         = 6
	DefaultTTL         = 10 * time.Minute
	DefaultCooldown    = 60 * time.Second
	DefaultWindow      = time.Hour
	DefaultWindowLimit = 5
	DefaultMaxAttempts = 5
)

var (
	ErrUnavailable = errors.New("recovery email verification is not configured")
	ErrInvalidCode = errors.New("invalid recovery email verification code")
)

var hashContext = []byte("pjsk:recovery-email-verification:v1:")

type Policy struct {
	TTL         time.Duration
	Cooldown    time.Duration
	Window      time.Duration
	WindowLimit int
	MaxAttempts int
}

func DefaultPolicy() Policy {
	return Policy{
		TTL:         DefaultTTL,
		Cooldown:    DefaultCooldown,
		Window:      DefaultWindow,
		WindowLimit: DefaultWindowLimit,
		MaxAttempts: DefaultMaxAttempts,
	}
}

type Manager struct {
	hmacKey []byte
	random  io.Reader
	policy  Policy
}

func NewManager(hmacKey []byte) (*Manager, error) {
	if len(hmacKey) < 32 {
		return nil, ErrUnavailable
	}
	return &Manager{
		hmacKey: append([]byte(nil), hmacKey...),
		random:  rand.Reader,
		policy:  DefaultPolicy(),
	}, nil
}

func (m *Manager) Policy() Policy {
	return m.policy
}

func (m *Manager) Generate() (string, error) {
	code := make([]byte, CodeLength)
	for index := range code {
		for {
			var value [1]byte
			if _, err := io.ReadFull(m.random, value[:]); err != nil {
				return "", err
			}
			// 250 is the largest multiple of 10 below 256, avoiding modulo bias.
			if value[0] < 250 {
				code[index] = '0' + value[0]%10
				break
			}
		}
	}
	return string(code), nil
}

func ValidCode(code string) bool {
	if len(code) != CodeLength || strings.TrimSpace(code) != code {
		return false
	}
	for index := range code {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	return true
}

func (m *Manager) Hash(recoveryEmailID string, code string) (string, error) {
	if strings.TrimSpace(recoveryEmailID) == "" || !ValidCode(code) {
		return "", ErrInvalidCode
	}
	mac := hmac.New(sha256.New, m.hmacKey)
	_, _ = mac.Write(hashContext)
	_, _ = mac.Write([]byte(recoveryEmailID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (m *Manager) Matches(recoveryEmailID string, code string, storedHash string) bool {
	want, err := m.Hash(recoveryEmailID, code)
	if err != nil {
		return false
	}
	wantBytes, err := hex.DecodeString(want)
	if err != nil {
		return false
	}
	storedBytes, err := hex.DecodeString(storedHash)
	if err != nil || len(storedBytes) != len(wantBytes) {
		return false
	}
	return subtle.ConstantTimeCompare(wantBytes, storedBytes) == 1
}

type Sender interface {
	SendRecoveryVerification(ctx context.Context, to string, code string, expiresAt time.Time) error
}
