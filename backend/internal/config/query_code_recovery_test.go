package config

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"pjsk/backend/internal/querycoderecovery"
	"pjsk/backend/internal/recoveryemailverification"
)

func TestLoadQueryCodeRecoveryHMACKeyUsesCurrentKey(t *testing.T) {
	clearQueryCodeRecoveryKeyEnv(t)
	currentValue := randomBase64Key(t, 32)
	legacyValue := randomBase64Key(t, 48)
	t.Setenv("QUERY_CODE_RECOVERY_HMAC_KEY", currentValue)
	t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", legacyValue)

	key, err := loadQueryCodeRecoveryHMACKey("development")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(key, mustDecodeBase64Key(t, legacyValue)) || !bytes.Equal(key, mustDecodeBase64Key(t, currentValue)) {
		t.Fatal("current query-code recovery key was not preferred")
	}
}

func TestLoadQueryCodeRecoveryHMACKeyRejectsInvalidCurrentKey(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "invalid base64", value: "sensitive-invalid-key-value"},
		{name: "short", value: randomBase64Key(t, 31)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearQueryCodeRecoveryKeyEnv(t)
			t.Setenv("QUERY_CODE_RECOVERY_HMAC_KEY", test.value)
			t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", randomBase64Key(t, 48))
			_, err := loadQueryCodeRecoveryHMACKey("development")
			if err == nil {
				t.Fatal("invalid current key was accepted")
			}
			if strings.Contains(err.Error(), test.value) {
				t.Fatal("error exposed the configured key value")
			}
			if !strings.Contains(err.Error(), "QUERY_CODE_RECOVERY_HMAC_KEY") {
				t.Fatal("error did not identify the invalid variable")
			}
		})
	}
}

func TestLoadQueryCodeRecoveryHMACKeyProductionRequiresCurrentKey(t *testing.T) {
	for _, appEnvironment := range []string{"production", " PrOdUcTiOn "} {
		t.Run(appEnvironment, func(t *testing.T) {
			clearQueryCodeRecoveryKeyEnv(t)
			legacyValue := randomBase64Key(t, 32)
			t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", legacyValue)
			_, err := loadQueryCodeRecoveryHMACKey(appEnvironment)
			if err == nil || !strings.Contains(err.Error(), "QUERY_CODE_RECOVERY_HMAC_KEY") {
				t.Fatalf("production missing-key error = %v", err)
			}
			if strings.Contains(err.Error(), legacyValue) {
				t.Fatal("production error exposed the legacy key value")
			}
		})
	}

	clearQueryCodeRecoveryKeyEnv(t)
	currentValue := randomBase64Key(t, 32)
	t.Setenv("QUERY_CODE_RECOVERY_HMAC_KEY", currentValue)
	key, err := loadQueryCodeRecoveryHMACKey(" production ")
	if err != nil || !bytes.Equal(key, mustDecodeBase64Key(t, currentValue)) {
		t.Fatalf("valid production key = %d bytes, err %v", len(key), err)
	}
}

func TestLoadQueryCodeRecoveryHMACKeyNonProductionFallback(t *testing.T) {
	for _, appEnvironment := range []string{"", "development", "test", " TEST "} {
		t.Run(appEnvironment, func(t *testing.T) {
			clearQueryCodeRecoveryKeyEnv(t)
			legacyValue := randomBase64Key(t, 32)
			t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", legacyValue)

			warning := captureConfigLog(t, func() {
				key, err := loadQueryCodeRecoveryHMACKey(appEnvironment)
				if err != nil || !bytes.Equal(key, mustDecodeBase64Key(t, legacyValue)) {
					t.Fatalf("fallback key = %d bytes, err %v", len(key), err)
				}
			})
			if !strings.Contains(warning, "QUERY_CODE_RECOVERY_HMAC_KEY") || !strings.Contains(warning, "RECOVERY_EMAIL_VERIFICATION_HMAC_KEY") {
				t.Fatal("fallback warning did not identify both variable names")
			}
			if strings.Contains(warning, legacyValue) {
				t.Fatal("fallback warning exposed the legacy key value")
			}
			if strings.Count(warning, "compatibility fallback") != 1 {
				t.Fatalf("fallback warning count = %d, want 1", strings.Count(warning, "compatibility fallback"))
			}
		})
	}
}

func TestLoadQueryCodeRecoveryHMACKeyRejectsMissingOrInvalidFallback(t *testing.T) {
	tests := []struct {
		name        string
		legacyValue string
	}{
		{name: "both missing"},
		{name: "legacy invalid", legacyValue: "legacy-sensitive-invalid-value"},
		{name: "legacy short", legacyValue: randomBase64Key(t, 31)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearQueryCodeRecoveryKeyEnv(t)
			t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", test.legacyValue)
			_, err := loadQueryCodeRecoveryHMACKey("development")
			if err == nil {
				t.Fatal("missing or invalid fallback was accepted")
			}
			if test.legacyValue != "" && strings.Contains(err.Error(), test.legacyValue) {
				t.Fatal("fallback error exposed the configured key value")
			}
		})
	}
}

func TestQueryCodeRecoveryAndVerificationUseDifferentRootKeys(t *testing.T) {
	verificationKey := bytes.Repeat([]byte{0x31}, 32)
	queryRecoveryKey := bytes.Repeat([]byte{0x72}, 32)

	verificationManager, err := recoveryemailverification.NewManager(verificationKey)
	if err != nil {
		t.Fatal(err)
	}
	verificationWithQueryKey, err := recoveryemailverification.NewManager(queryRecoveryKey)
	if err != nil {
		t.Fatal(err)
	}
	verificationHash, err := verificationManager.Hash("recovery-email-id", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if verificationWithQueryKey.Matches("recovery-email-id", "123456", verificationHash) {
		t.Fatal("query-code recovery root key matched an email verification hash")
	}

	queryManager, err := querycoderecovery.NewManager(queryRecoveryKey)
	if err != nil {
		t.Fatal(err)
	}
	queryWithVerificationKey, err := querycoderecovery.NewManager(verificationKey)
	if err != nil {
		t.Fatal(err)
	}
	queryHash, err := queryWithVerificationKey.HashCode("user-id", "recovery-email-id", "CN001", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if queryManager.MatchesCode("user-id", "recovery-email-id", "CN001", "123456", queryHash) {
		t.Fatal("email verification root key matched a query-code recovery hash")
	}
}

func clearQueryCodeRecoveryKeyEnv(t *testing.T) {
	t.Helper()
	t.Setenv("QUERY_CODE_RECOVERY_HMAC_KEY", "")
	t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", "")
}

func mustDecodeBase64Key(t *testing.T, value string) []byte {
	t.Helper()
	key, err := decodeHMACKey("TEST_KEY", value)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func captureConfigLog(t *testing.T, action func()) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := log.Writer()
	flags := log.Flags()
	prefix := log.Prefix()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	action()
	return buffer.String()
}
