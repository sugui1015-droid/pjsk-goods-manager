package config

import "testing"

func TestRecoveryEmailVerificationConfigDisabledAndKeyValidation(t *testing.T) {
	clearRecoveryEmailVerificationEnv(t)
	key, mode, smtpConfig, err := loadRecoveryEmailVerificationConfig("development")
	if err != nil || key != nil || mode != "disabled" || smtpConfig.Host != "" {
		t.Fatalf("disabled config = key %d mode %q host %t err %v", len(key), mode, smtpConfig.Host != "", err)
	}

	t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", "not-base64")
	if _, _, _, err := loadRecoveryEmailVerificationConfig("development"); err == nil {
		t.Fatal("invalid verification HMAC key was accepted")
	}
	t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", randomBase64Key(t, 31))
	if _, _, _, err := loadRecoveryEmailVerificationConfig("development"); err == nil {
		t.Fatal("short verification HMAC key was accepted")
	}
}

func TestRecoveryEmailVerificationConfigRejectsPartialSMTP(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "SMTP fields while disabled", values: map[string]string{"RECOVERY_EMAIL_SMTP_HOST": "smtp.example.com"}},
		{name: "missing host", values: map[string]string{"RECOVERY_EMAIL_SENDER_MODE": "smtp", "RECOVERY_EMAIL_SMTP_PORT": "587", "RECOVERY_EMAIL_SMTP_FROM": "no-reply@example.com", "RECOVERY_EMAIL_SMTP_TLS_MODE": "starttls"}},
		{name: "invalid port", values: map[string]string{"RECOVERY_EMAIL_SENDER_MODE": "smtp", "RECOVERY_EMAIL_SMTP_HOST": "smtp.example.com", "RECOVERY_EMAIL_SMTP_PORT": "invalid", "RECOVERY_EMAIL_SMTP_FROM": "no-reply@example.com", "RECOVERY_EMAIL_SMTP_TLS_MODE": "starttls"}},
		{name: "invalid TLS", values: map[string]string{"RECOVERY_EMAIL_SENDER_MODE": "smtp", "RECOVERY_EMAIL_SMTP_HOST": "smtp.example.com", "RECOVERY_EMAIL_SMTP_PORT": "587", "RECOVERY_EMAIL_SMTP_FROM": "no-reply@example.com", "RECOVERY_EMAIL_SMTP_TLS_MODE": "plain"}},
		{name: "password without username", values: map[string]string{"RECOVERY_EMAIL_SENDER_MODE": "smtp", "RECOVERY_EMAIL_SMTP_HOST": "smtp.example.com", "RECOVERY_EMAIL_SMTP_PORT": "587", "RECOVERY_EMAIL_SMTP_FROM": "no-reply@example.com", "RECOVERY_EMAIL_SMTP_TLS_MODE": "starttls", "RECOVERY_EMAIL_SMTP_PASSWORD": "test-only"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearRecoveryEmailVerificationEnv(t)
			t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", randomBase64Key(t, 48))
			for name, value := range test.values {
				t.Setenv(name, value)
			}
			if _, _, _, err := loadRecoveryEmailVerificationConfig("development"); err == nil {
				t.Fatal("partial or invalid SMTP configuration was accepted")
			}
		})
	}
}

func TestRecoveryEmailVerificationConfigAcceptsSMTPAndTestFake(t *testing.T) {
	clearRecoveryEmailVerificationEnv(t)
	t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", randomBase64Key(t, 48))
	t.Setenv("RECOVERY_EMAIL_SENDER_MODE", "smtp")
	t.Setenv("RECOVERY_EMAIL_SMTP_HOST", "smtp.example.com")
	t.Setenv("RECOVERY_EMAIL_SMTP_PORT", "587")
	t.Setenv("RECOVERY_EMAIL_SMTP_USERNAME", "test-user")
	t.Setenv("RECOVERY_EMAIL_SMTP_PASSWORD", "test-password")
	t.Setenv("RECOVERY_EMAIL_SMTP_FROM", "no-reply@example.com")
	t.Setenv("RECOVERY_EMAIL_SMTP_FROM_NAME", "PJSK")
	t.Setenv("RECOVERY_EMAIL_SMTP_TLS_MODE", "starttls")
	key, mode, smtpConfig, err := loadRecoveryEmailVerificationConfig("development")
	if err != nil || len(key) != 48 || mode != "smtp" || smtpConfig.Port != 587 || smtpConfig.TLSMode != "starttls" {
		t.Fatalf("valid SMTP config = key %d mode %q port %d TLS %q err %v", len(key), mode, smtpConfig.Port, smtpConfig.TLSMode, err)
	}

	clearRecoveryEmailVerificationEnv(t)
	t.Setenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY", randomBase64Key(t, 48))
	t.Setenv("RECOVERY_EMAIL_SENDER_MODE", "fake")
	if _, _, _, err := loadRecoveryEmailVerificationConfig("development"); err == nil {
		t.Fatal("fake sender was accepted outside APP_ENV=test")
	}
	key, mode, _, err = loadRecoveryEmailVerificationConfig("test")
	if err != nil || len(key) != 48 || mode != "fake" {
		t.Fatalf("test fake config = key %d mode %q err %v", len(key), mode, err)
	}
}

func clearRecoveryEmailVerificationEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"RECOVERY_EMAIL_VERIFICATION_HMAC_KEY",
		"RECOVERY_EMAIL_SENDER_MODE",
		"RECOVERY_EMAIL_SMTP_HOST",
		"RECOVERY_EMAIL_SMTP_PORT",
		"RECOVERY_EMAIL_SMTP_USERNAME",
		"RECOVERY_EMAIL_SMTP_PASSWORD",
		"RECOVERY_EMAIL_SMTP_FROM",
		"RECOVERY_EMAIL_SMTP_FROM_NAME",
		"RECOVERY_EMAIL_SMTP_TLS_MODE",
	} {
		t.Setenv(name, "")
	}
}
