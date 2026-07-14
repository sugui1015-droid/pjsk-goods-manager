package config

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestLoadRecoveryEmailKeysAllowsBothMissing(t *testing.T) {
	t.Setenv("RECOVERY_EMAIL_ENCRYPTION_KEY", "")
	t.Setenv("RECOVERY_EMAIL_HMAC_KEY", "")
	encryptionKey, hmacKey, err := loadRecoveryEmailKeys()
	if err != nil || encryptionKey != nil || hmacKey != nil {
		t.Fatalf("missing keys = (%d, %d, %v), want empty configuration", len(encryptionKey), len(hmacKey), err)
	}
}

func TestLoadRecoveryEmailKeysValidatesPairAndLength(t *testing.T) {
	validEncryption := randomBase64Key(t, 32)
	validHMAC := randomBase64Key(t, 48)
	tests := []struct {
		name       string
		encryption string
		hmac       string
	}{
		{name: "missing hmac", encryption: validEncryption},
		{name: "missing encryption", hmac: validHMAC},
		{name: "invalid encryption base64", encryption: "not-base64", hmac: validHMAC},
		{name: "short encryption", encryption: randomBase64Key(t, 31), hmac: validHMAC},
		{name: "invalid hmac base64", encryption: validEncryption, hmac: "not-base64"},
		{name: "short hmac", encryption: validEncryption, hmac: randomBase64Key(t, 31)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("RECOVERY_EMAIL_ENCRYPTION_KEY", test.encryption)
			t.Setenv("RECOVERY_EMAIL_HMAC_KEY", test.hmac)
			if _, _, err := loadRecoveryEmailKeys(); err == nil {
				t.Fatal("invalid recovery email key configuration was accepted")
			}
		})
	}

	t.Setenv("RECOVERY_EMAIL_ENCRYPTION_KEY", validEncryption)
	t.Setenv("RECOVERY_EMAIL_HMAC_KEY", validHMAC)
	encryptionKey, hmacKey, err := loadRecoveryEmailKeys()
	if err != nil || len(encryptionKey) != 32 || len(hmacKey) != 48 {
		t.Fatalf("valid keys = (%d, %d, %v)", len(encryptionKey), len(hmacKey), err)
	}
}

func randomBase64Key(t *testing.T, size int) string {
	t.Helper()
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(value)
}
