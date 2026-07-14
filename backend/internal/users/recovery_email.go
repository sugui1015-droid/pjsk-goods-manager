package users

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/recoveryemail"
)

var (
	ErrRecoveryEmailUnavailable = errors.New("recovery email is not configured")
	ErrRecoveryEmailReason      = errors.New("recovery email reason is required")
	ErrRecoveryEmailUserMerged  = errors.New("merged user cannot manage recovery email")
)

type RecoveryEmailProtector interface {
	Protect(string) (recoveryemail.Protected, error)
	MaskEncrypted([]byte) (string, error)
}

type RecoveryEmailStore interface {
	GetRecoveryEmail(context.Context, string) (RecoveryEmailRecord, error)
	PutRecoveryEmail(context.Context, string, string, string, recoveryemail.Protected) (RecoveryEmailMutation, error)
	UnbindRecoveryEmail(context.Context, string, string, string) (bool, error)
}

type RecoveryEmailRecord = recoveryemail.Record

type RecoveryEmailMutation struct {
	Record RecoveryEmailRecord
	Action string
}

type RecoveryEmailResponse struct {
	HasRecoveryEmail bool   `json:"has_recovery_email"`
	Status           string `json:"status,omitempty"`
	MaskedEmail      string `json:"masked_email,omitempty"`
	VerifiedAt       string `json:"verified_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	Message          string `json:"message,omitempty"`
}

type recoveryEmailPutRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type recoveryEmailDeleteRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) ConfigureRecoveryEmail(store RecoveryEmailStore, protector RecoveryEmailProtector) {
	h.recoveryEmailStore = store
	h.recoveryEmailProtector = protector
}

func (h *Handler) RecoveryEmail(w http.ResponseWriter, r *http.Request, userID string) {
	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "需要管理员登录")
		return
	}
	if h.recoveryEmailStore == nil {
		writeError(w, http.StatusServiceUnavailable, "找回邮箱功能尚未配置")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getRecoveryEmail(w, r, userID)
	case http.MethodPut:
		h.putRecoveryEmail(w, r, userID, account.ID)
	case http.MethodDelete:
		h.deleteRecoveryEmail(w, r, userID, account.ID)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *Handler) getRecoveryEmail(w http.ResponseWriter, r *http.Request, userID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	record, err := h.recoveryEmailStore.GetRecoveryEmail(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		log.Printf("get recovery email: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	response, err := h.recoveryEmailResponse(record)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "找回邮箱功能尚未配置")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) putRecoveryEmail(w http.ResponseWriter, r *http.Request, userID string, adminID string) {
	if h.recoveryEmailProtector == nil {
		writeError(w, http.StatusServiceUnavailable, "找回邮箱功能尚未配置")
		return
	}
	var request recoveryEmailPutRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "请填写操作原因")
		return
	}
	protected, err := h.recoveryEmailProtector.Protect(request.Email)
	if errors.Is(err, recoveryemail.ErrInvalidEmail) {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	if err != nil {
		log.Printf("protect recovery email: %v", err)
		writeError(w, http.StatusServiceUnavailable, "找回邮箱功能尚未配置")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	mutation, err := h.recoveryEmailStore.PutRecoveryEmail(ctx, userID, adminID, reason, protected)
	if h.writeRecoveryEmailMutationError(w, err) {
		return
	}
	response, err := h.recoveryEmailResponse(mutation.Record)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	switch mutation.Action {
	case "recovery_email_created":
		response.Message = "找回邮箱已登记，当前状态为待验证。"
	case "recovery_email_replaced":
		response.Message = "找回邮箱已替换，当前状态为待验证。"
	default:
		response.Message = "找回邮箱与当前记录一致，未重复变更。"
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) deleteRecoveryEmail(w http.ResponseWriter, r *http.Request, userID string, adminID string) {
	var request recoveryEmailDeleteRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, "请填写解绑原因")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	changed, err := h.recoveryEmailStore.UnbindRecoveryEmail(ctx, userID, adminID, reason)
	if h.writeRecoveryEmailMutationError(w, err) {
		return
	}
	message := "当前没有已登记的找回邮箱。"
	if changed {
		message = "找回邮箱已解绑。"
	}
	writeJSON(w, http.StatusOK, RecoveryEmailResponse{HasRecoveryEmail: false, Message: message})
}

func (h *Handler) writeRecoveryEmailMutationError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "用户不存在")
	case errors.Is(err, ErrRecoveryEmailReason):
		writeError(w, http.StatusBadRequest, "请填写操作原因")
	case errors.Is(err, ErrRecoveryEmailUserMerged):
		writeError(w, http.StatusConflict, "已合并用户不能维护找回邮箱")
	default:
		log.Printf("mutate recovery email: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
	}
	return true
}

func (h *Handler) recoveryEmailResponse(record RecoveryEmailRecord) (RecoveryEmailResponse, error) {
	if !record.HasRecoveryEmail {
		return RecoveryEmailResponse{HasRecoveryEmail: false}, nil
	}
	if h.recoveryEmailProtector == nil {
		return RecoveryEmailResponse{}, ErrRecoveryEmailUnavailable
	}
	masked, err := h.recoveryEmailProtector.MaskEncrypted(record.EncryptedEmail)
	if err != nil {
		return RecoveryEmailResponse{}, err
	}
	return RecoveryEmailResponse{
		HasRecoveryEmail: true,
		Status:           record.Status,
		MaskedEmail:      masked,
		VerifiedAt:       record.VerifiedAt,
		UpdatedAt:        record.UpdatedAt,
	}, nil
}

func (s *PostgresStore) GetRecoveryEmail(ctx context.Context, userID string) (RecoveryEmailRecord, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `select exists(select 1 from users where id = $1::uuid)`, userID).Scan(&exists); err != nil {
		return RecoveryEmailRecord{}, err
	}
	if !exists {
		return RecoveryEmailRecord{}, ErrUserNotFound
	}

	var record RecoveryEmailRecord
	err := s.pool.QueryRow(ctx, `
		select encrypted_email, email_lookup_hash, status,
			coalesce(to_char(verified_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			to_char(updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
	`, userID).Scan(&record.EncryptedEmail, &record.LookupHash, &record.Status, &record.VerifiedAt, &record.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoveryEmailRecord{HasRecoveryEmail: false}, nil
	}
	if err != nil {
		return RecoveryEmailRecord{}, err
	}
	record.HasRecoveryEmail = true
	return record, nil
}

func (s *PostgresStore) PutRecoveryEmail(ctx context.Context, userID string, adminID string, reason string, protected recoveryemail.Protected) (RecoveryEmailMutation, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return RecoveryEmailMutation{}, ErrRecoveryEmailReason
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RecoveryEmailMutation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userStatus string
	if err := tx.QueryRow(ctx, `select status from users where id = $1::uuid for update`, userID).Scan(&userStatus); errors.Is(err, pgx.ErrNoRows) {
		return RecoveryEmailMutation{}, ErrUserNotFound
	} else if err != nil {
		return RecoveryEmailMutation{}, err
	}
	if userStatus == "merged" {
		return RecoveryEmailMutation{}, ErrRecoveryEmailUserMerged
	}

	var currentID string
	var current RecoveryEmailRecord
	err = tx.QueryRow(ctx, `
		select id::text, encrypted_email, email_lookup_hash, status,
			coalesce(to_char(verified_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			to_char(updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
		for update
	`, userID).Scan(&currentID, &current.EncryptedEmail, &current.LookupHash, &current.Status, &current.VerifiedAt, &current.UpdatedAt)
	hadCurrent := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return RecoveryEmailMutation{}, err
	}
	if hadCurrent && current.LookupHash == protected.LookupHash {
		current.HasRecoveryEmail = true
		return RecoveryEmailMutation{Record: current}, nil
	}

	action := "recovery_email_created"
	if hadCurrent {
		action = "recovery_email_replaced"
		if _, err := tx.Exec(ctx, `
			update user_recovery_emails
			set status = 'disabled', invalidated_at = now(), updated_at = now(), updated_by_admin_id = $2::uuid
			where id = $1::uuid
		`, currentID, adminID); err != nil {
			return RecoveryEmailMutation{}, err
		}
		if _, err := tx.Exec(ctx, `
			update recovery_email_verification_codes
			set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
			where recovery_email_id = $1::uuid and status in ('sending', 'active') and invalidated_at is null
		`, currentID); err != nil {
			return RecoveryEmailMutation{}, err
		}
		if err := invalidateQueryCodeRecoveryForEmail(ctx, tx, currentID); err != nil {
			return RecoveryEmailMutation{}, err
		}
	}

	var record RecoveryEmailRecord
	err = tx.QueryRow(ctx, `
		insert into user_recovery_emails (
			user_id, encrypted_email, email_lookup_hash, status,
			created_by_admin_id, updated_by_admin_id
		) values ($1::uuid, $2, $3, 'pending', $4::uuid, $4::uuid)
		returning encrypted_email, email_lookup_hash, status,
			coalesce(to_char(verified_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			to_char(updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	`, userID, protected.Encrypted, protected.LookupHash, adminID).Scan(
		&record.EncryptedEmail, &record.LookupHash, &record.Status, &record.VerifiedAt, &record.UpdatedAt,
	)
	if err != nil {
		return RecoveryEmailMutation{}, err
	}
	record.HasRecoveryEmail = true
	metadata, err := json.Marshal(map[string]any{
		"new_status":        "pending",
		"old_record_exists": hadCurrent,
		"operation":         action,
		"masked_email":      protected.Masked,
	})
	if err != nil {
		return RecoveryEmailMutation{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into account_security_audit_logs (
			actor_type, admin_id, target_user_id, action, result, reason, metadata
		) values ('admin', $1::uuid, $2::uuid, $3, 'success', $4, $5::jsonb)
	`, adminID, userID, action, reason, metadata); err != nil {
		return RecoveryEmailMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RecoveryEmailMutation{}, err
	}
	return RecoveryEmailMutation{Record: record, Action: action}, nil
}

func (s *PostgresStore) UnbindRecoveryEmail(ctx context.Context, userID string, adminID string, reason string) (bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false, ErrRecoveryEmailReason
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userStatus string
	if err := tx.QueryRow(ctx, `select status from users where id = $1::uuid for update`, userID).Scan(&userStatus); errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUserNotFound
	} else if err != nil {
		return false, err
	}
	if userStatus == "merged" {
		return false, ErrRecoveryEmailUserMerged
	}

	var currentID string
	err = tx.QueryRow(ctx, `
		select id::text from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
		for update
	`, userID).Scan(&currentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		update user_recovery_emails
		set status = 'disabled', invalidated_at = now(), updated_at = now(), updated_by_admin_id = $2::uuid
		where id = $1::uuid
	`, currentID, adminID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where recovery_email_id = $1::uuid and status in ('sending', 'active') and invalidated_at is null
	`, currentID); err != nil {
		return false, err
	}
	if err := invalidateQueryCodeRecoveryForEmail(ctx, tx, currentID); err != nil {
		return false, err
	}
	metadata, err := json.Marshal(map[string]any{
		"new_status":        "disabled",
		"old_record_exists": true,
		"operation":         "recovery_email_unbound",
	})
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		insert into account_security_audit_logs (
			actor_type, admin_id, target_user_id, action, result, reason, metadata
		) values ('admin', $1::uuid, $2::uuid, 'recovery_email_unbound', 'success', $3, $4::jsonb)
	`, adminID, userID, reason, metadata); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func invalidateQueryCodeRecoveryForEmail(ctx context.Context, tx pgx.Tx, recoveryEmailID string) error {
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where recovery_email_id = $1::uuid and purpose = 'query_code_recovery'
		  and status in ('sending', 'active') and invalidated_at is null
	`, recoveryEmailID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_sessions
		set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where recovery_email_id = $1::uuid and purpose = 'query_code_recovery'
		  and status = 'active' and invalidated_at is null
	`, recoveryEmailID); err != nil {
		return err
	}
	return nil
}
