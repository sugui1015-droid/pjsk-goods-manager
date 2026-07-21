package querycode

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var ErrInvalid = errors.New("invalid query code")

// bindTokenAlphabet deliberately excludes the easily confused characters
// 0, O, 1, I, and L so an admin can read the code to a user over chat or
// voice without ambiguity.
const bindTokenAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// BindTokenLength is the length of a one-time bind token. 10 characters
// over a 31-character alphabet gives ~49.5 bits of entropy, which combined
// with the 30-minute expiry and the failed-attempt cap makes online
// guessing infeasible.
const BindTokenLength = 10

// GenerateBindToken returns a cryptographically random one-time bind token.
// crypto/rand is used deliberately: math/rand, timestamps, user ids, or
// sequential numbers are not acceptable sources for this value.
func GenerateBindToken() (string, error) {
	buffer := make([]byte, BindTokenLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	for index := range buffer {
		// 31 does not divide 256 evenly, but the resulting bias (<1.6%) is
		// irrelevant at this entropy level for a short-lived, attempt-capped
		// token; rejection sampling is not worth the complexity here.
		buffer[index] = bindTokenAlphabet[int(buffer[index])%len(bindTokenAlphabet)]
	}
	return string(buffer), nil
}

// HashBindToken returns the hex SHA-256 digest of a bind token. SHA-256
// (rather than bcrypt) is appropriate because the token is high-entropy,
// short-lived, and attempt-capped — unlike a human-chosen query code.
// Only this hash is ever persisted.
func HashBindToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Normalize is the single canonical form for a user-supplied query code.
// Every flow that hashes, stores, or compares a query code must run the raw
// input through it first, so that "the value we wrote" and "the value we
// later compare against" can never diverge.
//
// It trims leading and trailing whitespace only — mobile keyboards, paste,
// and autofill routinely append a space, and a trailing space silently
// locking a user out of a code they just set is a far worse failure than
// tolerating it. It deliberately does NOT case-fold (query codes are
// case-sensitive) and does NOT touch interior whitespace (that is part of
// the secret).
func Normalize(value string) string {
	return strings.TrimSpace(value)
}

func Validate(value string) error {
	if len(value) < 6 || len(value) > 32 {
		return ErrInvalid
	}
	hasLetterOrDigit := false
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			hasLetterOrDigit = true
		case char >= 'A' && char <= 'Z':
			hasLetterOrDigit = true
		case char >= '0' && char <= '9':
			hasLetterOrDigit = true
		case char == '-' || char == '_' || char == '@' || char == '#' || char == '.':
		default:
			return ErrInvalid
		}
	}
	if !hasLetterOrDigit {
		return ErrInvalid
	}
	return nil
}
