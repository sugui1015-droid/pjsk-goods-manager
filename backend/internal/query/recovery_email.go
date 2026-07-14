package query

import (
	"context"
	"log"
	"net/http"
	"time"

	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/recoveryemail"
)

const recoveryEmailFoundationMessage = "当前仅完成找回邮箱登记，尚未开放邮箱自助找回。"

type RecoveryEmailReader interface {
	GetRecoveryEmail(context.Context, string) (recoveryemail.Record, error)
}

type RecoveryEmailProtector interface {
	MaskEncrypted([]byte) (string, error)
}

type recoveryEmailResponse struct {
	HasRecoveryEmail bool   `json:"has_recovery_email"`
	Status           string `json:"status,omitempty"`
	MaskedEmail      string `json:"masked_email,omitempty"`
	VerifiedAt       string `json:"verified_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	Message          string `json:"message"`
}

func (h *Handler) ConfigureRecoveryEmail(store RecoveryEmailReader, protector RecoveryEmailProtector) {
	h.recoveryEmailStore = store
	h.recoveryEmailProtector = protector
}

func (h *Handler) RecoveryEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	user, ok := h.userFromRequest(w, r)
	if !ok {
		return
	}
	if h.recoveryEmailStore == nil {
		writeError(w, http.StatusServiceUnavailable, "找回邮箱功能尚未配置")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	record, err := h.recoveryEmailStore.GetRecoveryEmail(ctx, user.ID)
	if err != nil {
		log.Printf("get query recovery email: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	response := recoveryEmailResponse{
		HasRecoveryEmail: record.HasRecoveryEmail,
		Status:           record.Status,
		VerifiedAt:       record.VerifiedAt,
		UpdatedAt:        record.UpdatedAt,
		Message:          recoveryEmailFoundationMessage,
	}
	if record.HasRecoveryEmail {
		if h.recoveryEmailProtector == nil {
			writeError(w, http.StatusServiceUnavailable, "找回邮箱功能尚未配置")
			return
		}
		masked, err := h.recoveryEmailProtector.MaskEncrypted(record.EncryptedEmail)
		if err != nil {
			log.Printf("mask query recovery email: %v", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
			return
		}
		response.MaskedEmail = masked
	}
	writeJSON(w, http.StatusOK, response)
}
