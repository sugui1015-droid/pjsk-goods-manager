package config

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnvironment                   string
	Port                             string
	DatabaseURL                      string
	LegacyAdminPort                  string
	LegacyUserPort                   string
	FrontendOrigins                  []string
	AdminSessionTTL                  time.Duration
	CookieSecure                     bool
	RecoveryEmailEncryptionKey       []byte
	RecoveryEmailHMACKey             []byte
	RecoveryEmailVerificationHMACKey []byte
	RecoveryEmailSenderMode          string
	RecoveryEmailSMTP                RecoveryEmailSMTPConfig
}

type RecoveryEmailSMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	TLSMode  string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Printf(".env not loaded: %v", err)
	}

	databaseURL, err := databaseURLFromEnv()
	if err != nil {
		return Config{}, err
	}

	adminSessionTTL, err := time.ParseDuration(EnvOr("ADMIN_SESSION_TTL", "12h"))
	if err != nil || adminSessionTTL <= 0 {
		return Config{}, fmt.Errorf("ADMIN_SESSION_TTL must be a positive duration")
	}

	cookieSecure, err := strconv.ParseBool(EnvOr("ADMIN_COOKIE_SECURE", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("ADMIN_COOKIE_SECURE must be true or false")
	}

	recoveryEmailEncryptionKey, recoveryEmailHMACKey, err := loadRecoveryEmailKeys()
	if err != nil {
		return Config{}, err
	}
	appEnvironment := EnvOr("APP_ENV", "development")
	verificationHMACKey, senderMode, smtpConfig, err := loadRecoveryEmailVerificationConfig(appEnvironment)
	if err != nil {
		return Config{}, err
	}

	return Config{
		AppEnvironment:  appEnvironment,
		Port:            EnvOr("APP_PORT", EnvOr("SERVER_PORT", EnvOr("BACKEND_PORT", "8080"))),
		DatabaseURL:     databaseURL,
		LegacyAdminPort: EnvOr("LEGACY_STREAMLIT_ADMIN_PORT", "8512"),
		LegacyUserPort:  EnvOr("LEGACY_STREAMLIT_USER_PORT", "8513"),
		FrontendOrigins: []string{
			"http://localhost:5173",
			"http://127.0.0.1:5173",
		},
		AdminSessionTTL:                  adminSessionTTL,
		CookieSecure:                     cookieSecure,
		RecoveryEmailEncryptionKey:       recoveryEmailEncryptionKey,
		RecoveryEmailHMACKey:             recoveryEmailHMACKey,
		RecoveryEmailVerificationHMACKey: verificationHMACKey,
		RecoveryEmailSenderMode:          senderMode,
		RecoveryEmailSMTP:                smtpConfig,
	}, nil
}

func loadRecoveryEmailKeys() ([]byte, []byte, error) {
	encryptionValue := strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_ENCRYPTION_KEY"))
	hmacValue := strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_HMAC_KEY"))
	if encryptionValue == "" && hmacValue == "" {
		return nil, nil, nil
	}
	if encryptionValue == "" || hmacValue == "" {
		return nil, nil, fmt.Errorf("RECOVERY_EMAIL_ENCRYPTION_KEY and RECOVERY_EMAIL_HMAC_KEY must be configured together")
	}

	encryptionKey, err := base64.StdEncoding.DecodeString(encryptionValue)
	if err != nil || len(encryptionKey) != 32 {
		return nil, nil, fmt.Errorf("RECOVERY_EMAIL_ENCRYPTION_KEY must be base64-encoded 32-byte data")
	}
	hmacKey, err := base64.StdEncoding.DecodeString(hmacValue)
	if err != nil || len(hmacKey) < 32 {
		return nil, nil, fmt.Errorf("RECOVERY_EMAIL_HMAC_KEY must be base64-encoded data of at least 32 bytes")
	}
	return encryptionKey, hmacKey, nil
}

func loadRecoveryEmailVerificationConfig(appEnvironment string) ([]byte, string, RecoveryEmailSMTPConfig, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_SENDER_MODE")))
	if mode == "" {
		mode = "disabled"
	}
	keyValue := strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY"))
	var key []byte
	if keyValue != "" {
		decoded, err := base64.StdEncoding.DecodeString(keyValue)
		if err != nil || len(decoded) < 32 {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY must be base64-encoded data of at least 32 bytes")
		}
		key = decoded
	}

	smtpConfig := RecoveryEmailSMTPConfig{
		Host:     strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_SMTP_HOST")),
		Username: strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_SMTP_USERNAME")),
		Password: os.Getenv("RECOVERY_EMAIL_SMTP_PASSWORD"),
		From:     strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_SMTP_FROM")),
		FromName: strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_SMTP_FROM_NAME")),
		TLSMode:  strings.ToLower(strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_SMTP_TLS_MODE"))),
	}
	portValue := strings.TrimSpace(os.Getenv("RECOVERY_EMAIL_SMTP_PORT"))
	anySMTP := smtpConfig.Host != "" || portValue != "" || smtpConfig.Username != "" || smtpConfig.Password != "" || smtpConfig.From != "" || smtpConfig.FromName != "" || smtpConfig.TLSMode != ""

	switch mode {
	case "disabled":
		if anySMTP {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("SMTP settings require RECOVERY_EMAIL_SENDER_MODE=smtp")
		}
		return key, mode, RecoveryEmailSMTPConfig{}, nil
	case "fake":
		if !strings.EqualFold(strings.TrimSpace(appEnvironment), "test") || len(key) == 0 || anySMTP {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("fake recovery email sender is only available in APP_ENV=test with a verification HMAC key and no SMTP settings")
		}
		return key, mode, RecoveryEmailSMTPConfig{}, nil
	case "smtp":
		if len(key) == 0 {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_VERIFICATION_HMAC_KEY is required for SMTP delivery")
		}
		port, err := strconv.Atoi(portValue)
		if err != nil || port < 1 || port > 65535 {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SMTP_PORT must be an integer from 1 to 65535")
		}
		smtpConfig.Port = port
		if smtpConfig.Host == "" || strings.ContainsAny(smtpConfig.Host, "\r\n") || smtpConfig.From == "" {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("SMTP host and from address are required")
		}
		from, err := mail.ParseAddress(smtpConfig.From)
		if err != nil || from.Address != smtpConfig.From {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SMTP_FROM must be a valid address")
		}
		if (smtpConfig.Username == "") != (smtpConfig.Password == "") {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("SMTP username and password must be configured together")
		}
		if smtpConfig.TLSMode != "starttls" && smtpConfig.TLSMode != "tls" {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SMTP_TLS_MODE must be starttls or tls")
		}
		if strings.ContainsAny(smtpConfig.FromName, "\r\n") {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SMTP_FROM_NAME contains invalid characters")
		}
		return key, mode, smtpConfig, nil
	default:
		return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SENDER_MODE must be disabled, smtp, or fake")
	}
}

func EnvOr(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	return value
}

func databaseURLFromEnv() (string, error) {
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		return databaseURL, nil
	}

	host := EnvOr("DATABASE_HOST", "localhost")
	port := EnvOr("DATABASE_PORT", "5432")
	user := strings.TrimSpace(os.Getenv("DATABASE_USER"))
	password := os.Getenv("DATABASE_PASSWORD")
	name := strings.TrimSpace(os.Getenv("DATABASE_NAME"))

	if user == "" {
		return "", fmt.Errorf("DATABASE_USER is missing")
	}
	if name == "" {
		return "", fmt.Errorf("DATABASE_NAME is missing")
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   name,
	}
	query := u.Query()
	query.Set("sslmode", EnvOr("DATABASE_SSLMODE", "disable"))
	u.RawQuery = query.Encode()

	return u.String(), nil
}
