package admin

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/logsafe"
)

type emailRecoveryRequestRequest struct {
	Username string `json:"username"`
}

// RecoveryEmailResetRequest is the unauthenticated "send me a reset code"
// entry. It always answers 202 for well-formed requests whether or not the
// account exists or has a verified email, so it cannot enumerate accounts.
// With delivery disabled it answers an explicit 503 instead of pretending.
func (h *Handler) RecoveryEmailResetRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !h.emailRecoveryAvailable() {
		writeError(w, http.StatusServiceUnavailable, "email recovery is not enabled")
		return
	}

	var request emailRecoveryRequestRequest
	if err := decodeJSON(w, r, &request); err != nil || strings.TrimSpace(request.Username) == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	username := strings.TrimSpace(request.Username)

	ip := h.resolveClientIP(r)
	limiterKey := "email-reset-request:" + normalizeLimiterUsername(username)
	now := h.now()
	if !h.limiter.allow(ip, limiterKey, now) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, please try again later")
		return
	}

	// Every non-sendable outcome below still answers 202. The limiter records
	// a failure so repeated probing of unknown names burns the same budget.
	accepted := func() { writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"}) }

	account, err := h.store.FindByUsername(r.Context(), username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("find admin for email recovery: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if errors.Is(err, ErrNotFound) || account.Status != "active" {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		accepted()
		return
	}
	record, err := h.security.RecoveryEmail(r.Context(), account.ID)
	if err != nil {
		log.Printf("read recovery email for reset: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !record.HasRecoveryEmail || record.Status != "verified" {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryEmailResetFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonRecoveryEmailNotConfigured)
		accepted()
		return
	}

	code, err := generateEmailCode(h.random)
	if err != nil {
		log.Printf("generate reset code: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	expiresAt := h.now().Add(emailCodeTTL)
	if err := h.security.CreateEmailCode(r.Context(), account.ID, "reset", hashEmailCode(h.recoveryCodeHMACKey, "reset", code), expiresAt); err != nil {
		log.Printf("store reset code: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	address, err := h.emailProtector.RevealEncrypted(record.EncryptedEmail)
	if err != nil || h.emailSender.SendRecoveryVerification(r.Context(), address, code, expiresAt) != nil {
		// The address itself must not reach logs; the admin id is enough.
		log.Printf("send reset code failed for admin %s", account.ID)
		writeError(w, http.StatusBadGateway, "failed to send verification email")
		return
	}
	accepted()
}

type emailRecoveryResetRequest struct {
	Username    string `json:"username"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

// RecoveryEmailReset is the unauthenticated single-step email recovery:
// username + emailed code + new password. Like the recovery-code flow it
// never issues a session; success revokes every session of the account in
// the same transaction as the password change.
func (h *Handler) RecoveryEmailReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.security == nil || len(h.recoveryCodeHMACKey) == 0 {
		writeError(w, http.StatusServiceUnavailable, "email recovery is not enabled")
		return
	}

	var request emailRecoveryResetRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	username := strings.TrimSpace(request.Username)
	code := strings.TrimSpace(request.Code)
	if username == "" || code == "" || request.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "username, code and new_password are required")
		return
	}

	ip := h.resolveClientIP(r)
	limiterKey := "email-reset:" + normalizeLimiterUsername(username)
	now := h.now()
	if !h.limiter.allow(ip, limiterKey, now) {
		if h.limiter.shouldAuditRateLimited(ip, limiterKey, now) {
			h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryEmailResetFailed, nil, username, ip, AdminAuthResultFailure, AdminAuthReasonRateLimited)
		}
		writeError(w, http.StatusTooManyRequests, "too many attempts, please try again later")
		return
	}

	failGeneric := func(adminID *string, auditUsername string, reason AdminAuthReasonCode) {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryEmailResetFailed, adminID, auditUsername, ip, AdminAuthResultFailure, reason)
		writeError(w, http.StatusUnauthorized, "invalid username or verification code")
	}

	account, err := h.store.FindByUsername(r.Context(), username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("find admin for email reset: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if errors.Is(err, ErrNotFound) || account.Status != "active" {
		failGeneric(nil, username, AdminAuthReasonInvalidVerificationCode)
		return
	}
	if err := validateAdminPassword(request.NewPassword, account.Username); err != nil {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventRecoveryEmailResetFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonWeakPassword)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash email reset password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	event := h.adminAuthAuditEvent(r, AdminAuthEventRecoveryEmailResetSucceeded, &account.ID, account.Username, ip, AdminAuthResultSuccess, AdminAuthReasonNone)
	err = h.security.ResetPasswordWithEmailCode(r.Context(), account.ID, hashEmailCode(h.recoveryCodeHMACKey, "reset", code), string(newHash), event)
	if errors.Is(err, ErrNoUsableCode) {
		failGeneric(&account.ID, account.Username, AdminAuthReasonInvalidVerificationCode)
		return
	}
	if err != nil {
		log.Printf("reset password with email code: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.limiter.recordSuccess(ip, limiterKey)
	w.WriteHeader(http.StatusNoContent)
}
