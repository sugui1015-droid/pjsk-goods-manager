package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/logsafe"
)

const (
	// recoveryCodeCharset deliberately omits lookalikes (0/O, 1/I/L, U/V
	// confusion is avoided by dropping U) so hand-typed codes survive paper.
	recoveryCodeCharset  = "ABCDEFGHJKMNPQRSTVWXYZ23456789"
	recoveryCodeGroups   = 4
	recoveryCodeGroupLen = 5
	// RecoveryCodeBatchSize is how many one-time codes one generation yields.
	RecoveryCodeBatchSize = 10

	recoveryCodeHMACContext = "pjsk:admin-recovery-code:v1:"
	emailCodeHMACContext    = "pjsk:admin-recovery-email-code:v1:"
)

// generateRecoveryCode returns one formatted code like
// "ABCDE-FGHJK-MNPQR-STVWX" built from crypto-random charset picks
// (~98 bits of entropy at 20 characters over a 30-symbol alphabet).
func generateRecoveryCode(random io.Reader) (string, error) {
	raw := make([]byte, recoveryCodeGroups*recoveryCodeGroupLen)
	// Rejection sampling keeps the distribution uniform over the charset.
	buf := make([]byte, 1)
	limit := byte(256 - (256 % len(recoveryCodeCharset)))
	for i := range raw {
		for {
			if _, err := io.ReadFull(random, buf); err != nil {
				return "", err
			}
			if buf[0] < limit {
				raw[i] = recoveryCodeCharset[int(buf[0])%len(recoveryCodeCharset)]
				break
			}
		}
	}
	groups := make([]string, 0, recoveryCodeGroups)
	for i := 0; i < len(raw); i += recoveryCodeGroupLen {
		groups = append(groups, string(raw[i:i+recoveryCodeGroupLen]))
	}
	return strings.Join(groups, "-"), nil
}

// normalizeRecoveryCode strips separators and upper-cases, then validates
// length and charset. It returns "" for anything that cannot be a code.
func normalizeRecoveryCode(input string) string {
	cleaned := strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\t", "").Replace(strings.TrimSpace(input)))
	if len(cleaned) != recoveryCodeGroups*recoveryCodeGroupLen {
		return ""
	}
	for _, r := range cleaned {
		if !strings.ContainsRune(recoveryCodeCharset, r) {
			return ""
		}
	}
	return cleaned
}

// hashRecoveryCode is the only representation that ever reaches storage:
// a raw 32-byte domain-separated HMAC-SHA256 of the normalized code.
func hashRecoveryCode(key []byte, normalized string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(recoveryCodeHMACContext + normalized))
	return mac.Sum(nil)
}

// hashEmailCode hex-hashes a 6-digit email verification code with purpose
// domain separation under the same dedicated admin recovery key.
func hashEmailCode(key []byte, purpose string, code string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(emailCodeHMACContext + purpose + ":" + code))
	return fmt.Sprintf("%x", mac.Sum(nil))
}

func generateEmailCode(random io.Reader) (string, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", err
	}
	value := (uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])) % 1000000
	return fmt.Sprintf("%06d", value), nil
}

type recoveryCodesStatusResponse struct {
	Enabled        bool       `json:"enabled"`
	RemainingCodes int        `json:"remaining_codes"`
	GeneratedAt    *time.Time `json:"generated_at,omitempty"`
}

type recoveryCodesGenerateResponse struct {
	Codes       []string  `json:"codes"`
	GeneratedAt time.Time `json:"generated_at"`
}

// OwnerRecoveryCodes serves GET (status) and POST (generate) for the owner's
// one-time recovery codes. The router wraps it with RequireAuthentication and
// RequireOwner; POST additionally sits behind RequireRecentReauth.
func (h *Handler) OwnerRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	account, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.security == nil || len(h.recoveryCodeHMACKey) == 0 {
		writeError(w, http.StatusServiceUnavailable, "recovery codes are not configured")
		return
	}

	switch r.Method {
	case http.MethodGet:
		status, err := h.security.RecoveryCodeStatus(r.Context(), account.ID)
		if err != nil {
			log.Printf("recovery code status: %s", logsafe.Category(err))
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, recoveryCodesStatusResponse{
			Enabled:        true,
			RemainingCodes: status.RemainingCodes,
			GeneratedAt:    status.GeneratedAt,
		})
	case http.MethodPost:
		codes := make([]string, 0, RecoveryCodeBatchSize)
		hashes := make([][]byte, 0, RecoveryCodeBatchSize)
		seen := make(map[string]bool, RecoveryCodeBatchSize)
		for len(codes) < RecoveryCodeBatchSize {
			code, err := generateRecoveryCode(h.random)
			if err != nil {
				log.Printf("generate recovery code: %v", err)
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			normalized := normalizeRecoveryCode(code)
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			codes = append(codes, code)
			hashes = append(hashes, hashRecoveryCode(h.recoveryCodeHMACKey, normalized))
		}

		batchID, err := newRandomUUID(h.random)
		if err != nil {
			log.Printf("generate recovery batch id: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		event := h.adminAuthAuditEvent(r, AdminAuthEventRecoveryCodesGenerated, &account.ID, account.Username, h.resolveClientIP(r), AdminAuthResultSuccess, AdminAuthReasonNone)
		if err := h.security.ReplaceRecoveryCodes(r.Context(), account.ID, batchID, hashes, event); err != nil {
			log.Printf("replace recovery codes: %s", logsafe.Category(err))
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		// The plaintext codes exist only in this one response; the store has
		// nothing but raw HMAC digests.
		writeJSON(w, http.StatusOK, recoveryCodesGenerateResponse{Codes: codes, GeneratedAt: h.now()})
	default:
		methodNotAllowed(w)
	}
}

// newRandomUUID builds a version-4 UUID string from the handler's random
// source, keeping everything injectable for tests.
func newRandomUUID(random io.Reader) (string, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}

type recoveryCodeResetRequest struct {
	Username     string `json:"username"`
	RecoveryCode string `json:"recovery_code"`
	NewPassword  string `json:"new_password"`
}

// RecoveryCodeReset is the unauthenticated single-step recovery flow: one
// valid code plus a new password. It never issues a session — after success
// the admin still has to log in with the new password. All sessions of the
// account are revoked inside the same transaction.
func (h *Handler) RecoveryCodeReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.security == nil || len(h.recoveryCodeHMACKey) == 0 {
		writeError(w, http.StatusServiceUnavailable, "recovery codes are not configured")
		return
	}

	var request recoveryCodeResetRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	username := strings.TrimSpace(request.Username)
	if username == "" || request.RecoveryCode == "" || request.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "username, recovery_code and new_password are required")
		return
	}

	ip := h.resolveClientIP(r)
	limiterKey := "recovery-code:" + normalizeLimiterUsername(username)
	now := h.now()
	if !h.limiter.allow(ip, limiterKey, now) {
		if h.limiter.shouldAuditRateLimited(ip, limiterKey, now) {
			h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryCodeResetFailed, nil, username, ip, AdminAuthResultFailure, AdminAuthReasonRateLimited)
		}
		writeError(w, http.StatusTooManyRequests, "too many attempts, please try again later")
		return
	}

	failGeneric := func(adminID *string, auditUsername string, reason AdminAuthReasonCode) {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryCodeResetFailed, adminID, auditUsername, ip, AdminAuthResultFailure, reason)
		// One indistinguishable answer for unknown account, disabled account,
		// and wrong code, so the endpoint cannot be used for enumeration.
		writeError(w, http.StatusUnauthorized, "invalid username or recovery code")
	}

	account, err := h.store.FindByUsername(r.Context(), username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("find admin for recovery: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if errors.Is(err, ErrNotFound) || account.Status != "active" {
		failGeneric(nil, username, AdminAuthReasonInvalidRecoveryCode)
		return
	}

	normalized := normalizeRecoveryCode(request.RecoveryCode)
	if normalized == "" {
		failGeneric(&account.ID, account.Username, AdminAuthReasonInvalidRecoveryCode)
		return
	}
	if err := validateAdminPassword(request.NewPassword, account.Username); err != nil {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryCodeResetFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonWeakPassword)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash recovery password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	event := h.adminAuthAuditEvent(r, AdminAuthEventRecoveryCodeResetSucceeded, &account.ID, account.Username, ip, AdminAuthResultSuccess, AdminAuthReasonNone)
	err = h.security.ResetPasswordWithRecoveryCode(r.Context(), account.ID, hashRecoveryCode(h.recoveryCodeHMACKey, normalized), string(newHash), event)
	if errors.Is(err, ErrNoUsableCode) {
		failGeneric(&account.ID, account.Username, AdminAuthReasonInvalidRecoveryCode)
		return
	}
	if err != nil {
		log.Printf("reset password with recovery code: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.limiter.recordSuccess(ip, limiterKey)
	w.WriteHeader(http.StatusNoContent)
}
