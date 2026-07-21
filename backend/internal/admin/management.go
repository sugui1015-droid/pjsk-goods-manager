package admin

import (
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/logsafe"
)

// Owner-only admin management endpoints. The router wraps every route in
// RequireAuthentication + RequireOwner, and the mutating routes additionally
// in RequireRecentReauthWhen(MutatingMatch, …); the storage layer then refuses
// owner targets again, so a frontend bug can never turn into privilege
// escalation. Temporary passwords are system-generated from crypto/rand,
// returned exactly once in the response, and never logged.

// adminUsernamePattern keeps appointed usernames login-safe and unambiguous:
// lowercase letters, digits, and separators, 3–32 characters.
var adminUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)

// tempPasswordBytes yields 16-character base64url temporary passwords, well
// above the 10-character policy minimum.
const tempPasswordBytes = 12

// ConfigureManagement wires the owner admin-management storage surface.
func (h *Handler) ConfigureManagement(store ManagementStore) {
	h.management = store
}

type appointAdminRequest struct {
	UserID      string  `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name,omitempty"`
}

type managementActionRequest struct {
	Reason string `json:"reason,omitempty"`
}

type managedAdminResponse struct {
	Admin ManagedAdmin `json:"admin"`
	// TempPassword is present only on appointment and password reset — the
	// single time the plaintext is ever shown.
	TempPassword string `json:"temp_password,omitempty"`
}

// ManagementCollection handles /api/admin/owner/admins (GET list, POST appoint).
func (h *Handler) ManagementCollection(w http.ResponseWriter, r *http.Request) {
	if h.management == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	switch r.Method {
	case http.MethodGet:
		entries, err := h.management.ListManagedAdmins(r.Context())
		if err != nil {
			log.Printf("list managed admins: %s", logsafe.Category(err))
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admins": entries})
	case http.MethodPost:
		h.appointAdmin(w, r)
	default:
		methodNotAllowed(w)
	}
}

// ManagementItem handles /api/admin/owner/admins/{id}[/action].
func (h *Handler) ManagementItem(w http.ResponseWriter, r *http.Request) {
	if h.management == nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/owner/admins/")
	targetID, action, _ := strings.Cut(rest, "/")
	if targetID == "" || strings.Contains(action, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	switch {
	case action == "audit" && r.Method == http.MethodGet:
		entries, err := h.management.ListManagedAdminAudit(r.Context(), targetID, 50)
		if err != nil {
			log.Printf("list managed admin audit: %s", logsafe.Category(err))
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": entries})
	case action == "" && r.Method == http.MethodGet:
		entry, err := h.management.GetManagedAdmin(r.Context(), targetID)
		if err != nil {
			h.writeManagementError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, managedAdminResponse{Admin: entry})
	case r.Method == http.MethodPost:
		h.managementAction(w, r, targetID, action)
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) appointAdmin(w http.ResponseWriter, r *http.Request) {
	actor, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request appointAdminRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	request.UserID = strings.TrimSpace(request.UserID)
	username := strings.ToLower(strings.TrimSpace(request.Username))
	if request.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if !adminUsernamePattern.MatchString(username) {
		writeError(w, http.StatusBadRequest, "username must be 3-32 characters of a-z, 0-9, '_', '.', '-'")
		return
	}
	var displayName *string
	if request.DisplayName != nil {
		cleaned := normalizeAuditText(*request.DisplayName, 64)
		if cleaned != "" {
			displayName = &cleaned
		}
	}

	tempPassword, hash, err := h.newTemporaryPassword(username)
	if err != nil {
		log.Printf("generate temporary admin password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	event := h.managementEvent(r, AdminAuthEventAdminAppointed, actor, username, "")
	entry, err := h.management.AppointAdmin(r.Context(), AppointAdminInput{
		UserID:       request.UserID,
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: hash,
	}, event)
	if err != nil {
		h.auditManagementFailure(r, AdminAuthEventAdminAppointed, actor, username, err)
		h.writeManagementError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, managedAdminResponse{Admin: entry, TempPassword: tempPassword})
}

func (h *Handler) managementAction(w http.ResponseWriter, r *http.Request, targetID string, action string) {
	actor, ok := CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request managementActionRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
	}
	reason := normalizeAuditText(request.Reason, 256)

	var eventType AdminAuthEventType
	switch action {
	case "enable":
		eventType = AdminAuthEventAdminEnabled
	case "disable":
		eventType = AdminAuthEventAdminDisabled
	case "revoke":
		eventType = AdminAuthEventAdminRevoked
	case "reset-password":
		eventType = AdminAuthEventAdminPasswordResetByOwner
	default:
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	// The audit username is resolved from the target row so the trail names
	// the real account even when the caller only has the id.
	targetName := targetID
	if existing, err := h.management.GetManagedAdmin(r.Context(), targetID); err == nil {
		targetName = existing.Username
	}
	event := h.managementEvent(r, eventType, actor, targetName, reason)

	var entry ManagedAdmin
	var tempPassword string
	var err error
	switch action {
	case "enable":
		entry, err = h.management.SetManagedAdminStatus(r.Context(), targetID, "active", event)
	case "disable":
		entry, err = h.management.SetManagedAdminStatus(r.Context(), targetID, "disabled", event)
	case "revoke":
		entry, err = h.management.RevokeManagedAdmin(r.Context(), targetID, actor.ID, event)
	case "reset-password":
		var hash string
		tempPassword, hash, err = h.newTemporaryPassword(targetName)
		if err != nil {
			log.Printf("generate temporary admin password: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		entry, err = h.management.ResetManagedAdminPassword(r.Context(), targetID, hash, event)
	}
	if err != nil {
		h.auditManagementFailure(r, eventType, actor, targetName, err)
		h.writeManagementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, managedAdminResponse{Admin: entry, TempPassword: tempPassword})
}

// newTemporaryPassword generates the system temporary password: crypto/rand,
// shown once by the caller, bcrypt-hashed for storage, validated against the
// shared policy so it can always be typed into the login form.
func (h *Handler) newTemporaryPassword(username string) (string, string, error) {
	raw := make([]byte, tempPasswordBytes)
	if _, err := io.ReadFull(h.random, raw); err != nil {
		return "", "", err
	}
	password := base64.RawURLEncoding.EncodeToString(raw)
	if err := validateAdminPassword(password, username); err != nil {
		return "", "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	return password, string(hash), nil
}

// managementEvent builds the success audit event: target in AdminID (filled by
// the store), actor in ActorAdminID, optional operator reason.
func (h *Handler) managementEvent(r *http.Request, eventType AdminAuthEventType, actor Admin, targetUsername string, reason string) AdminAuthAuditEvent {
	actorID := actor.ID
	event := buildAdminAuthAuditEvent(
		&httpRequestSummary{UserAgent: r.UserAgent()},
		eventType,
		nil,
		targetUsername,
		h.resolveClientIP(r),
		AdminAuthResultSuccess,
		AdminAuthReasonNone,
		h.now(),
	)
	event.ActorAdminID = &actorID
	if reason != "" {
		event.ManagementReason = &reason
	}
	return event
}

// auditManagementFailure records refused management attempts (owner target,
// missing user, conflicts) best-effort; transport-level errors keep their
// generic category.
func (h *Handler) auditManagementFailure(r *http.Request, eventType AdminAuthEventType, actor Admin, targetUsername string, cause error) {
	reason := AdminAuthReasonDatabaseError
	switch {
	case errors.Is(cause, ErrTargetIsOwner):
		reason = AdminAuthReasonTargetIsOwner
	case errors.Is(cause, ErrUserNotFound):
		reason = AdminAuthReasonUserNotFound
	case errors.Is(cause, ErrUsernameTaken):
		reason = AdminAuthReasonUsernameTaken
	case errors.Is(cause, ErrUserAlreadyAdmin):
		reason = AdminAuthReasonUserAlreadyAdmin
	case errors.Is(cause, ErrInvalidTransition), errors.Is(cause, ErrNotFound):
		reason = AdminAuthReasonValidationFailed
	}
	actorID := actor.ID
	event := buildAdminAuthAuditEvent(
		&httpRequestSummary{UserAgent: r.UserAgent()},
		eventType,
		nil,
		targetUsername,
		h.resolveClientIP(r),
		AdminAuthResultFailure,
		reason,
		h.now(),
	)
	event.ActorAdminID = &actorID
	if err := h.store.RecordAdminAuthEvent(r.Context(), event); err != nil {
		log.Printf("record admin management audit event: %s", logsafe.Category(err))
	}
}

func (h *Handler) writeManagementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTargetIsOwner):
		writeError(w, http.StatusForbidden, "苏归账号不能在此管理")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "管理员不存在")
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusBadRequest, "用户不存在或已停用")
	case errors.Is(err, ErrUsernameTaken):
		writeError(w, http.StatusConflict, "该登录用户名已被占用")
	case errors.Is(err, ErrUserAlreadyAdmin):
		writeError(w, http.StatusConflict, "该用户已拥有管理员账号")
	case errors.Is(err, ErrInvalidTransition):
		writeError(w, http.StatusConflict, "当前状态不允许该操作")
	default:
		log.Printf("admin management: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
