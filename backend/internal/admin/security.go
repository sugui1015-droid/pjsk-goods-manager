package admin

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/recoveryemail"
)

const (
	minAdminPasswordLength = 10
	maxAdminPasswordLength = 128
	emailCodeTTL           = 10 * time.Minute
)

// AdminRecoveryEmailAAD isolates admin recovery email ciphertexts from the
// user table's ciphertexts under the same AES key.
const AdminRecoveryEmailAAD = "pjsk:admin-recovery-email:v1"

// ConfigureSecurity wires the optional owner/security capabilities into the
// handler. A nil protector or sender leaves the matching capability in an
// explicit "not enabled" state — endpoints answer 503, they never pretend.
func (h *Handler) ConfigureSecurity(
	store SecurityStore,
	recoveryCodeHMACKey []byte,
	emailProtector *recoveryemail.Protector,
	emailSender RecoveryVerificationSender,
) {
	h.security = store
	h.recoveryCodeHMACKey = append([]byte(nil), recoveryCodeHMACKey...)
	h.emailProtector = emailProtector
	h.emailSender = emailSender
}

// validateAdminPassword is the shared password policy for change, recovery,
// and CLI reset. It stays deliberately simple: length bounds plus "not your
// username"; strength beyond that is the password manager's job.
func validateAdminPassword(password string, username string) error {
	if len(password) < minAdminPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minAdminPasswordLength)
	}
	if len(password) > maxAdminPasswordLength {
		return fmt.Errorf("password must be at most %d characters", maxAdminPasswordLength)
	}
	if strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(username)) {
		return errors.New("password must not equal the username")
	}
	return nil
}

// ValidatePassword exposes the shared admin password policy to the CLI.
func ValidatePassword(password string, username string) error {
	return validateAdminPassword(password, username)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword lets the authenticated admin rotate their own password.
// Presenting the current password is itself re-authentication, so the flow
// does not additionally require a fresh reauth stamp; success revokes every
// other session and freshens this session's reauth.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	account, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tokenHash, ok := sessionHashFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.security == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var request changePasswordRequest
	if err := decodeJSON(w, r, &request); err != nil || request.CurrentPassword == "" || request.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	ip := h.resolveClientIP(r)
	limiterKey := "reauth:" + normalizeLimiterUsername(account.Username)
	now := h.now()
	if !h.limiter.allow(ip, limiterKey, now) {
		if h.limiter.shouldAuditRateLimited(ip, limiterKey, now) {
			h.recordAdminAuthEventBestEffort(r, AdminAuthEventReauthFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonRateLimited)
		}
		writeError(w, http.StatusTooManyRequests, "too many attempts, please try again later")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(request.CurrentPassword)) != nil {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventReauthFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonInvalidCredentials)
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	h.limiter.recordSuccess(ip, limiterKey)

	if err := validateAdminPassword(request.NewPassword, account.Username); err != nil {
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventReauthFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonWeakPassword)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash new admin password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	event := h.adminAuthAuditEvent(r, AdminAuthEventPasswordChanged, &account.ID, account.Username, ip, AdminAuthResultSuccess, AdminAuthReasonNone)
	if err := h.security.UpdatePasswordKeepSession(r.Context(), account.ID, string(newHash), tokenHash, h.now(), event); err != nil {
		log.Printf("update admin password: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// emailRecoveryAvailability distinguishes "storage configured" (protector)
// from "delivery configured" (sender). Binding and recovery both need both.
func (h *Handler) emailRecoveryAvailable() bool {
	return h.security != nil && h.emailProtector != nil && h.emailSender != nil && len(h.recoveryCodeHMACKey) > 0
}

type recoveryEmailStatusResponse struct {
	StorageConfigured bool       `json:"storage_configured"`
	DeliveryEnabled   bool       `json:"delivery_enabled"`
	HasRecoveryEmail  bool       `json:"has_recovery_email"`
	Status            string     `json:"status,omitempty"`
	MaskedEmail       string     `json:"masked_email,omitempty"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
}

// RecoveryEmailStatus reports the admin's recovery email state with the
// address masked; the plaintext never leaves the protector.
func (h *Handler) RecoveryEmailStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	account, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	response := recoveryEmailStatusResponse{
		StorageConfigured: h.security != nil && h.emailProtector != nil,
		DeliveryEnabled:   h.emailRecoveryAvailable(),
	}
	if response.StorageConfigured {
		record, err := h.security.RecoveryEmail(r.Context(), account.ID)
		if err != nil {
			log.Printf("read admin recovery email: %s", logsafe.Category(err))
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if record.HasRecoveryEmail {
			masked, err := h.emailProtector.MaskEncrypted(record.EncryptedEmail)
			if err != nil {
				log.Printf("mask admin recovery email: %s", logsafe.Category(err))
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			response.HasRecoveryEmail = true
			response.Status = record.Status
			response.MaskedEmail = masked
			response.VerifiedAt = record.VerifiedAt
		}
	}
	writeJSON(w, http.StatusOK, response)
}

type recoveryEmailBindRequest struct {
	Email string `json:"email"`
}

// RecoveryEmailBindRequest stores the pending address and sends a bind code.
// It sits behind RequireRecentReauth in the router. With delivery disabled it
// answers an explicit 503 and stores nothing — it never fakes a send.
func (h *Handler) RecoveryEmailBindRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	account, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !h.emailRecoveryAvailable() {
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryEmailBindFailed, &account.ID, account.Username, h.resolveClientIP(r), AdminAuthResultFailure, AdminAuthReasonEmailDeliveryDisabled)
		writeError(w, http.StatusServiceUnavailable, "email recovery is not enabled")
		return
	}

	var request recoveryEmailBindRequest
	if err := decodeJSON(w, r, &request); err != nil || strings.TrimSpace(request.Email) == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	protected, err := h.emailProtector.Protect(request.Email)
	if err != nil {
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryEmailBindFailed, &account.ID, account.Username, h.resolveClientIP(r), AdminAuthResultFailure, AdminAuthReasonValidationFailed)
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	code, err := generateEmailCode(h.random)
	if err != nil {
		log.Printf("generate bind code: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	expiresAt := h.now().Add(emailCodeTTL)
	if err := h.security.UpsertPendingRecoveryEmail(r.Context(), account.ID, protected.Encrypted, protected.LookupHash); err != nil {
		log.Printf("store pending recovery email: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.security.CreateEmailCode(r.Context(), account.ID, "bind", hashEmailCode(h.recoveryCodeHMACKey, "bind", code), expiresAt); err != nil {
		log.Printf("store bind code: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	// The plaintext address goes only to the sender; logs and audit see the
	// masked form at most.
	address, err := h.emailProtector.RevealEncrypted(protected.Encrypted)
	if err != nil || h.emailSender.SendRecoveryVerification(r.Context(), address, code, expiresAt) != nil {
		log.Printf("send bind code failed for admin %s", account.ID)
		writeError(w, http.StatusBadGateway, "failed to send verification email")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"masked_email": protected.Masked})
}

type recoveryEmailConfirmRequest struct {
	Code string `json:"code"`
}

// RecoveryEmailBindConfirm verifies the emailed 6-digit code and marks the
// pending address verified.
func (h *Handler) RecoveryEmailBindConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	account, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.security == nil || len(h.recoveryCodeHMACKey) == 0 {
		writeError(w, http.StatusServiceUnavailable, "email recovery is not enabled")
		return
	}
	var request recoveryEmailConfirmRequest
	if err := decodeJSON(w, r, &request); err != nil || strings.TrimSpace(request.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	ip := h.resolveClientIP(r)
	limiterKey := "email-bind:" + normalizeLimiterUsername(account.Username)
	now := h.now()
	if !h.limiter.allow(ip, limiterKey, now) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, please try again later")
		return
	}

	err := h.security.ConsumeEmailCode(r.Context(), account.ID, "bind", hashEmailCode(h.recoveryCodeHMACKey, "bind", strings.TrimSpace(request.Code)))
	if errors.Is(err, ErrNoUsableCode) {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryEmailBindFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonInvalidVerificationCode)
		writeError(w, http.StatusUnauthorized, "invalid or expired verification code")
		return
	}
	if err != nil {
		log.Printf("consume bind code: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.limiter.recordSuccess(ip, limiterKey)

	event := h.adminAuthAuditEvent(r, AdminAuthEventRecoveryEmailBound, &account.ID, account.Username, ip, AdminAuthResultSuccess, AdminAuthReasonNone)
	if err := h.security.MarkRecoveryEmailVerified(r.Context(), account.ID, event); err != nil {
		log.Printf("mark recovery email verified: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AuditSummary returns the caller's own recent auth/security events.
func (h *Handler) AuditSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	account, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.security == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	entries, err := h.security.ListAuthEvents(r.Context(), account.ID, 20)
	if err != nil {
		log.Printf("list admin auth events: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": entries})
}
