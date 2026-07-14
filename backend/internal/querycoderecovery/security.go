package querycoderecovery

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"
)

const (
	Purpose              = "query_code_recovery"
	CodeLength           = 6
	ResetTokenBytes      = 32
	DefaultCodeTTL       = 10 * time.Minute
	DefaultTokenTTL      = 10 * time.Minute
	DefaultCooldown      = 60 * time.Second
	DefaultWindow        = time.Hour
	DefaultWindowLimit   = 5
	DefaultIPWindowLimit = 20
	DefaultMaxAttempts   = 5
)

var (
	ErrUnavailable  = errors.New("query code recovery is not configured")
	ErrInvalidCode  = errors.New("invalid query code recovery code")
	ErrInvalidToken = errors.New("invalid query code recovery token")
)

var (
	codeHashContext  = []byte("pjsk:query-code-recovery:code:v1:")
	tokenHashContext = []byte("pjsk:query-code-recovery:token:v1:")
	keyHashContext   = []byte("pjsk:query-code-recovery:identifier:v1:")
)

type Policy struct {
	CodeTTL       time.Duration
	TokenTTL      time.Duration
	Cooldown      time.Duration
	Window        time.Duration
	WindowLimit   int
	IPWindowLimit int
	MaxAttempts   int
}

func DefaultPolicy() Policy {
	return Policy{
		CodeTTL:       DefaultCodeTTL,
		TokenTTL:      DefaultTokenTTL,
		Cooldown:      DefaultCooldown,
		Window:        DefaultWindow,
		WindowLimit:   DefaultWindowLimit,
		IPWindowLimit: DefaultIPWindowLimit,
		MaxAttempts:   DefaultMaxAttempts,
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

func (m *Manager) Policy() Policy { return m.policy }

func (m *Manager) GenerateCode() (string, error) {
	code := make([]byte, CodeLength)
	for index := range code {
		for {
			var value [1]byte
			if _, err := io.ReadFull(m.random, value[:]); err != nil {
				return "", err
			}
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

func (m *Manager) HashCode(userID string, recoveryEmailID string, cn string, code string) (string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(recoveryEmailID) == "" || strings.TrimSpace(cn) == "" || !ValidCode(code) {
		return "", ErrInvalidCode
	}
	return m.hmacHex(codeHashContext, userID, recoveryEmailID, NormalizeCN(cn), Purpose, code), nil
}

func (m *Manager) MatchesCode(userID string, recoveryEmailID string, cn string, code string, storedHash string) bool {
	want, err := m.HashCode(userID, recoveryEmailID, cn, code)
	if err != nil {
		return false
	}
	return constantTimeHexEqual(want, storedHash)
}

func (m *Manager) GenerateToken() (string, error) {
	raw := make([]byte, ResetTokenBytes)
	if _, err := io.ReadFull(m.random, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func ValidToken(token string) bool {
	if strings.TrimSpace(token) != token || token == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == ResetTokenBytes
}

func (m *Manager) HashToken(token string) (string, error) {
	if !ValidToken(token) {
		return "", ErrInvalidToken
	}
	return m.hmacHex(tokenHashContext, Purpose, token), nil
}

func (m *Manager) IdentifierHash(kind string, value string) (string, error) {
	kind = strings.TrimSpace(kind)
	value = strings.TrimSpace(value)
	if kind == "" || value == "" {
		return "", ErrUnavailable
	}
	return m.hmacHex(keyHashContext, kind, value), nil
}

func (m *Manager) hmacHex(context []byte, parts ...string) string {
	mac := hmac.New(sha256.New, m.hmacKey)
	_, _ = mac.Write(context)
	for _, part := range parts {
		_, _ = mac.Write([]byte(part))
		_, _ = mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func constantTimeHexEqual(want string, stored string) bool {
	wantBytes, err := hex.DecodeString(want)
	if err != nil {
		return false
	}
	storedBytes, err := hex.DecodeString(strings.TrimSpace(stored))
	if err != nil || len(storedBytes) != len(wantBytes) {
		return false
	}
	return subtle.ConstantTimeCompare(wantBytes, storedBytes) == 1
}

func NormalizeCN(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
