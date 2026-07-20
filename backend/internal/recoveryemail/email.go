package recoveryemail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"unicode"
)

const (
	maxEmailBytes = 254
	maxLocalBytes = 64
	cipherVersion = byte(1)
)

var (
	ErrUnavailable  = errors.New("recovery email encryption is not configured")
	ErrInvalidEmail = errors.New("invalid recovery email")
	ErrInvalidData  = errors.New("invalid encrypted recovery email")
)

var emailAAD = []byte("pjsk:user-recovery-email:v1")

type Protected struct {
	Encrypted  []byte
	LookupHash string
	Masked     string
}

type Record struct {
	HasRecoveryEmail bool
	EncryptedEmail   []byte
	LookupHash       string
	Status           string
	VerifiedAt       string
	UpdatedAt        string
}

type Protector struct {
	aead    cipher.AEAD
	hmacKey []byte
	random  io.Reader
	aad     []byte
}

// NewProtectorWithAAD builds a Protector bound to a caller-supplied additional
// authenticated data context, so ciphertexts from different tables (for
// example user vs admin recovery emails) can never be spliced across domains.
func NewProtectorWithAAD(encryptionKey []byte, hmacKey []byte, aad string) (*Protector, error) {
	protector, err := NewProtector(encryptionKey, hmacKey)
	if err != nil {
		return nil, err
	}
	if aad != "" {
		protector.aad = []byte(aad)
	}
	return protector, nil
}

func NewProtector(encryptionKey []byte, hmacKey []byte) (*Protector, error) {
	if len(encryptionKey) != 32 || len(hmacKey) < 32 {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Protector{aead: aead, hmacKey: append([]byte(nil), hmacKey...), random: rand.Reader, aad: emailAAD}, nil
}

func (p *Protector) Protect(value string) (Protected, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return Protected{}, err
	}
	masked, err := Mask(normalized)
	if err != nil {
		return Protected{}, err
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(p.random, nonce); err != nil {
		return Protected{}, err
	}
	result := make([]byte, 1, 1+len(nonce)+len(normalized)+p.aead.Overhead())
	result[0] = cipherVersion
	result = append(result, nonce...)
	result = p.aead.Seal(result, nonce, []byte(normalized), p.aad)
	return Protected{Encrypted: result, LookupHash: p.LookupHash(normalized), Masked: masked}, nil
}

func (p *Protector) reveal(encrypted []byte) (string, error) {
	nonceSize := p.aead.NonceSize()
	if len(encrypted) < 1+nonceSize+p.aead.Overhead() || encrypted[0] != cipherVersion {
		return "", ErrInvalidData
	}
	nonce := encrypted[1 : 1+nonceSize]
	plaintext, err := p.aead.Open(nil, nonce, encrypted[1+nonceSize:], p.aad)
	if err != nil {
		return "", ErrInvalidData
	}
	normalized, err := Normalize(string(plaintext))
	if err != nil {
		return "", ErrInvalidData
	}
	return normalized, nil
}

func (p *Protector) RevealEncrypted(encrypted []byte) (string, error) {
	return p.reveal(encrypted)
}

func (p *Protector) MaskEncrypted(encrypted []byte) (string, error) {
	value, err := p.reveal(encrypted)
	if err != nil {
		return "", err
	}
	return Mask(value)
}

func (p *Protector) LookupHash(normalized string) string {
	mac := hmac.New(sha256.New, p.hmacKey)
	_, _ = mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

func Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > maxEmailBytes || strings.Count(value, "@") != 1 {
		return "", ErrInvalidEmail
	}
	for _, char := range value {
		if char > unicode.MaxASCII || unicode.IsSpace(char) || unicode.IsControl(char) {
			return "", ErrInvalidEmail
		}
	}

	at := strings.LastIndexByte(value, '@')
	local, domain := value[:at], value[at+1:]
	if local == "" || len(local) > maxLocalBytes || domain == "" || len(domain) > 253 {
		return "", ErrInvalidEmail
	}
	if local[0] == '.' || local[len(local)-1] == '.' || strings.Contains(local, "..") {
		return "", ErrInvalidEmail
	}
	for index := range local {
		if !isLocalCharacter(local[index]) {
			return "", ErrInvalidEmail
		}
	}

	domain = strings.ToLower(domain)
	if !strings.Contains(domain, ".") {
		return "", ErrInvalidEmail
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidEmail
		}
		for index := range label {
			char := label[index]
			if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') {
				return "", ErrInvalidEmail
			}
		}
	}
	return local + "@" + domain, nil
}

func Mask(value string) (string, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return "", err
	}
	at := strings.LastIndexByte(normalized, '@')
	return normalized[:1] + "***" + normalized[at:], nil
}

func isLocalCharacter(char byte) bool {
	if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
		return true
	}
	return strings.ContainsRune(".!#$%&'*+/=?^_`{|}~-", rune(char))
}
