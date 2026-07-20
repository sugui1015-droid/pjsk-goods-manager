package admin

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/logsafe"
)

// ReauthWindow is how long a successful re-authentication stays fresh for
// high-risk operations. After it lapses the admin must prove the password
// again; an old session alone is never enough.
const ReauthWindow = 10 * time.Minute

// reauthRequiredMessage is the machine-readable marker the frontend matches
// to open its re-authentication dialog. It rides on 403 rather than 401 so
// the frontend's "session expired, log out" handling never fires for it.
const reauthRequiredMessage = "reauth_required"

type reauthRequest struct {
	Password string `json:"password"`
}

// Reauth re-verifies the current admin's password and stamps the session's
// reauth_at. Failures are rate-limited with the login limiter (separate
// bucket) and audited.
func (h *Handler) Reauth(w http.ResponseWriter, r *http.Request) {
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

	var request reauthRequest
	if err := decodeJSON(w, r, &request); err != nil || request.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
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

	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(request.Password)) != nil {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		h.recordAdminAuthEventBestEffort(r, AdminAuthEventReauthFailed, &account.ID, account.Username, ip, AdminAuthResultFailure, AdminAuthReasonInvalidCredentials)
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	h.limiter.recordSuccess(ip, limiterKey)

	event := h.adminAuthAuditEvent(r, AdminAuthEventReauthSucceeded, &account.ID, account.Username, ip, AdminAuthResultSuccess, AdminAuthReasonNone)
	if err := h.security.SetSessionReauth(r.Context(), tokenHash, h.now(), event); err != nil {
		log.Printf("set session reauth: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// hasFreshReauth reports whether the request's session re-authenticated
// within ReauthWindow.
func (h *Handler) hasFreshReauth(r *http.Request) bool {
	if h.security == nil {
		return false
	}
	tokenHash, ok := sessionHashFromRequest(r)
	if !ok {
		return false
	}
	reauthAt, ok, err := h.security.SessionReauthAt(r.Context(), tokenHash)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			log.Printf("read session reauth: %s", logsafe.Category(err))
		}
		return false
	}
	return ok && h.now().Sub(reauthAt) <= ReauthWindow
}

// RequireRecentReauth gates every request on a fresh re-authentication.
// It must wrap handlers that already sit behind RequireAuthentication.
func (h *Handler) RequireRecentReauth(next http.Handler) http.Handler {
	return h.RequireRecentReauthWhen(func(*http.Request) bool { return true }, next)
}

// RequireRecentReauthWhen gates only requests matching the predicate, so a
// prefix route can protect its mutating sub-path (for example POST …/void)
// without locking read-only detail views behind re-authentication.
func (h *Handler) RequireRecentReauthWhen(match func(*http.Request) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if match(r) && !h.hasFreshReauth(r) {
			writeError(w, http.StatusForbidden, reauthRequiredMessage)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MutatingSuffixMatch matches mutating requests whose path ends with the
// given suffix — the shape of the revert/void sub-routes.
func MutatingSuffixMatch(suffix string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			return false
		}
		return strings.HasSuffix(r.URL.Path, suffix)
	}
}

// MutatingMatch matches every non-read request, for prefix routes whose
// writes are all high-risk (for example the admin payment QR endpoints).
func MutatingMatch(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead
}

// RequireOwner allows only the (already authenticated) owner through.
func (h *Handler) RequireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := CurrentAdmin(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if account.Role != "owner" {
			writeError(w, http.StatusForbidden, "owner privileges required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
