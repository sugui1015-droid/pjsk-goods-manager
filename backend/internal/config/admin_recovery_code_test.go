package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestAdminRecoveryCodeKeyRequiredInProduction(t *testing.T) {
	t.Setenv("ADMIN_RECOVERY_CODE_HMAC_KEY", "")
	if _, err := loadAdminRecoveryCodeHMACKey("production", nil); err == nil || !strings.Contains(err.Error(), "required in production") {
		t.Fatalf("expected required-in-production error, got %v", err)
	}
}

func TestAdminRecoveryCodeKeyOptionalOutsideProduction(t *testing.T) {
	t.Setenv("ADMIN_RECOVERY_CODE_HMAC_KEY", "")
	key, err := loadAdminRecoveryCodeHMACKey("development", nil)
	if err != nil || key != nil {
		t.Fatalf("expected nil key without error, got %v / %v", key, err)
	}
}

func TestAdminRecoveryCodeKeyRejectsBadEncodingAndShortKeys(t *testing.T) {
	t.Setenv("ADMIN_RECOVERY_CODE_HMAC_KEY", "not-base64!!!")
	if _, err := loadAdminRecoveryCodeHMACKey("production", nil); err == nil {
		t.Fatal("expected decode error")
	}
	t.Setenv("ADMIN_RECOVERY_CODE_HMAC_KEY", base64.StdEncoding.EncodeToString([]byte("short")))
	if _, err := loadAdminRecoveryCodeHMACKey("production", nil); err == nil {
		t.Fatal("expected short-key error")
	}
}

func TestAdminRecoveryCodeKeyMustNotReuseQueryCodeKey(t *testing.T) {
	shared := bytes.Repeat([]byte{0x24}, 32)
	t.Setenv("ADMIN_RECOVERY_CODE_HMAC_KEY", base64.StdEncoding.EncodeToString(shared))
	if _, err := loadAdminRecoveryCodeHMACKey("production", shared); err == nil || !strings.Contains(err.Error(), "must not reuse") {
		t.Fatalf("expected reuse rejection, got %v", err)
	}

	different := bytes.Repeat([]byte{0x25}, 32)
	key, err := loadAdminRecoveryCodeHMACKey("production", different)
	if err != nil {
		t.Fatalf("distinct keys must be accepted: %v", err)
	}
	if !bytes.Equal(key, shared) {
		t.Fatal("decoded key must round-trip")
	}
}
