package config

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnvironment                   string
	Host                             string
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
	QueryCodeRecoveryHMACKey         []byte
	AdminRecoveryCodeHMACKey         []byte
	RecoveryEmailSenderMode          string
	RecoveryEmailSMTP                RecoveryEmailSMTPConfig
	TrustedProxyCIDRs                []netip.Prefix
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

const maxSMTPFromNameBytes = 128

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
	queryCodeRecoveryHMACKey, err := loadQueryCodeRecoveryHMACKey(appEnvironment)
	if err != nil {
		return Config{}, err
	}
	adminRecoveryCodeHMACKey, err := loadAdminRecoveryCodeHMACKey(appEnvironment, queryCodeRecoveryHMACKey)
	if err != nil {
		return Config{}, err
	}
	trustedProxyCIDRs, err := loadTrustedProxyCIDRs()
	if err != nil {
		return Config{}, err
	}
	host, err := loadServerHost(appEnvironment)
	if err != nil {
		return Config{}, err
	}
	frontendOrigins, err := loadCORSAllowedOrigins(appEnvironment)
	if err != nil {
		return Config{}, err
	}

	return Config{
		AppEnvironment:                   appEnvironment,
		Host:                             host,
		Port:                             loadPort(),
		DatabaseURL:                      databaseURL,
		LegacyAdminPort:                  EnvOr("LEGACY_STREAMLIT_ADMIN_PORT", "8512"),
		LegacyUserPort:                   EnvOr("LEGACY_STREAMLIT_USER_PORT", "8513"),
		FrontendOrigins:                  frontendOrigins,
		AdminSessionTTL:                  adminSessionTTL,
		CookieSecure:                     cookieSecure,
		RecoveryEmailEncryptionKey:       recoveryEmailEncryptionKey,
		RecoveryEmailHMACKey:             recoveryEmailHMACKey,
		RecoveryEmailVerificationHMACKey: verificationHMACKey,
		QueryCodeRecoveryHMACKey:         queryCodeRecoveryHMACKey,
		AdminRecoveryCodeHMACKey:         adminRecoveryCodeHMACKey,
		RecoveryEmailSenderMode:          senderMode,
		RecoveryEmailSMTP:                smtpConfig,
		TrustedProxyCIDRs:                trustedProxyCIDRs,
	}, nil
}

// loadCORSAllowedOrigins parses CORS_ALLOWED_ORIGINS, a comma-separated list
// of exact origins allowed to make credentialed cross-origin requests. When
// unset, development and test keep the two local Vite dev origins;
// production defaults to no cross-origin access at all — the recommended
// production deployment serves the frontend and API from one origin.
func loadCORSAllowedOrigins(appEnvironment string) ([]string, error) {
	const name = "CORS_ALLOWED_ORIGINS"
	raw := os.Getenv(name)
	if strings.TrimSpace(raw) == "" {
		if strings.EqualFold(strings.TrimSpace(appEnvironment), "production") {
			return nil, nil
		}
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}, nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	origins := make([]string, 0, len(parts))
	for i, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			return nil, fmt.Errorf("%s entry %d is empty; remove stray commas", name, i+1)
		}
		normalized, err := normalizeCORSOrigin(entry)
		if err != nil {
			return nil, fmt.Errorf("%s entry %d %s", name, i+1, err)
		}
		if seen[normalized] {
			return nil, fmt.Errorf("%s entry %d duplicates an earlier origin", name, i+1)
		}
		seen[normalized] = true
		origins = append(origins, normalized)
	}
	return origins, nil
}

// normalizeCORSOrigin validates one configured origin and returns its
// canonical form: lowercase scheme and host, optional port kept verbatim
// (default ports are deliberately not folded), no trailing slash. Error text
// never echoes the configured value.
func normalizeCORSOrigin(value string) (string, error) {
	if value == "*" || strings.EqualFold(value, "null") {
		return "", fmt.Errorf("must be an exact http or https origin, not a wildcard or null")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Opaque != "" {
		return "", fmt.Errorf("must be a valid absolute URL origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("must use the http or https scheme")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("must not contain a username or password")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("must include a host")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("must not contain a path")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return "", fmt.Errorf("must not contain a query")
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("must not contain a fragment")
	}
	return scheme + "://" + strings.ToLower(parsed.Host), nil
}

// loadPort keeps the long-standing port precedence: APP_PORT, then
// SERVER_PORT, then BACKEND_PORT, then 8080.
func loadPort() string {
	return EnvOr("APP_PORT", EnvOr("SERVER_PORT", EnvOr("BACKEND_PORT", "8080")))
}

// loadServerHost parses SERVER_HOST, the IP address the HTTP server binds
// to. The default is loopback in every environment; listening on all
// interfaces requires an explicit 0.0.0.0 or ::. Only literal IPs are
// accepted — hostnames (including localhost) would add DNS ambiguity to the
// bind address.
func loadServerHost(appEnvironment string) (string, error) {
	const name = "SERVER_HOST"
	value := EnvOr(name, "127.0.0.1")
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return "", fmt.Errorf("%s must be a literal IPv4 or IPv6 address without port, zone, or hostname", name)
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() && strings.EqualFold(strings.TrimSpace(appEnvironment), "production") {
		log.Printf("%s is set to an all-interfaces address in production; ensure a firewall or reverse proxy fronts the backend", name)
	}
	return addr.String(), nil
}

// loadTrustedProxyCIDRs parses TRUSTED_PROXY_CIDRS, a comma-separated list of
// CIDRs whose peers are allowed to speak for clients via X-Forwarded-For.
// Empty means no proxy is trusted. Errors identify the entry by position
// only, never echoing the configured value.
func loadTrustedProxyCIDRs() ([]netip.Prefix, error) {
	const name = "TRUSTED_PROXY_CIDRS"
	raw := os.Getenv(name)
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	seen := make(map[netip.Prefix]bool, len(parts))
	for i, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			return nil, fmt.Errorf("%s entry %d is empty; remove stray commas", name, i+1)
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("%s entry %d must be a valid IPv4 or IPv6 CIDR", name, i+1)
		}
		if prefix.Addr().Zone() != "" {
			return nil, fmt.Errorf("%s entry %d must not contain a zone", name, i+1)
		}
		if prefix.Bits() == 0 {
			return nil, fmt.Errorf("%s entry %d must not cover the whole address space", name, i+1)
		}
		normalized := prefix.Masked()
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		prefixes = append(prefixes, normalized)
	}
	return prefixes, nil
}

func loadQueryCodeRecoveryHMACKey(appEnvironment string) ([]byte, error) {
	const currentName = "QUERY_CODE_RECOVERY_HMAC_KEY"
	const legacyName = "RECOVERY_EMAIL_VERIFICATION_HMAC_KEY"

	value := strings.TrimSpace(os.Getenv(currentName))
	if value != "" {
		key, err := decodeHMACKey(currentName, value)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	if strings.EqualFold(strings.TrimSpace(appEnvironment), "production") {
		return nil, fmt.Errorf("%s is required in production", currentName)
	}

	legacyValue := strings.TrimSpace(os.Getenv(legacyName))
	if legacyValue == "" {
		return nil, fmt.Errorf("%s is required; %s compatibility fallback is unavailable", currentName, legacyName)
	}
	key, err := decodeHMACKey(legacyName, legacyValue)
	if err != nil {
		return nil, err
	}
	log.Printf("%s is not configured; using temporary %s compatibility fallback outside production", currentName, legacyName)
	return key, nil
}

// loadAdminRecoveryCodeHMACKey parses ADMIN_RECOVERY_CODE_HMAC_KEY, the key
// that hashes admin one-time recovery codes. It is deliberately independent:
// reusing the query-code recovery key would let one leaked key forge both
// user and admin recovery material, so an identical value is rejected.
func loadAdminRecoveryCodeHMACKey(appEnvironment string, queryCodeRecoveryHMACKey []byte) ([]byte, error) {
	const name = "ADMIN_RECOVERY_CODE_HMAC_KEY"

	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		if strings.EqualFold(strings.TrimSpace(appEnvironment), "production") {
			return nil, fmt.Errorf("%s is required in production", name)
		}
		return nil, nil
	}
	key, err := decodeHMACKey(name, value)
	if err != nil {
		return nil, err
	}
	if len(queryCodeRecoveryHMACKey) > 0 && subtle.ConstantTimeCompare(key, queryCodeRecoveryHMACKey) == 1 {
		return nil, fmt.Errorf("%s must not reuse QUERY_CODE_RECOVERY_HMAC_KEY", name)
	}
	return key, nil
}

func decodeHMACKey(name string, value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) < 32 {
		return nil, fmt.Errorf("%s must be base64-encoded data of at least 32 bytes", name)
	}
	return key, nil
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
		if strings.ContainsAny(smtpConfig.Username, "\r\n") {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SMTP_USERNAME contains invalid characters")
		}
		if (smtpConfig.Username == "") != (smtpConfig.Password == "") {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("SMTP username and password must be configured together")
		}
		if smtpConfig.TLSMode != "starttls" && smtpConfig.TLSMode != "tls" {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SMTP_TLS_MODE must be starttls or tls")
		}
		if strings.ContainsAny(smtpConfig.FromName, "\r\n") || len([]byte(smtpConfig.FromName)) > maxSMTPFromNameBytes {
			return nil, "", RecoveryEmailSMTPConfig{}, fmt.Errorf("RECOVERY_EMAIL_SMTP_FROM_NAME must not contain line breaks and must be at most 128 bytes")
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
