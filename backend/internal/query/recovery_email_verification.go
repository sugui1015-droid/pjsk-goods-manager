package query

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"pjsk/backend/internal/recoveryemailverification"
)

type RecoveryEmailVerificationService interface {
	Send(context.Context, string) (recoveryemailverification.SendResult, error)
	Verify(context.Context, string, string) (recoveryemailverification.VerifyResult, error)
}

type verificationCodeRequest struct {
	Code string `json:"code"`
}

type recoveryEmailVerificationResponse struct {
	Success           bool   `json:"success"`
	Message           string `json:"message"`
	Status            string `json:"status,omitempty"`
	MaskedEmail       string `json:"masked_email,omitempty"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	VerifiedAt        string `json:"verified_at,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

func (h *Handler) ConfigureRecoveryEmailVerification(service RecoveryEmailVerificationService) {
	h.recoveryEmailVerification = service
}

func (h *Handler) SendRecoveryEmailVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	user, ok := h.userFromRequest(w, r)
	if !ok {
		return
	}
	if h.recoveryEmailVerification == nil {
		writeError(w, http.StatusServiceUnavailable, "邮箱验证服务暂不可用")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := h.recoveryEmailVerification.Send(ctx, user.ID)
	if err != nil {
		h.writeRecoveryEmailVerificationError(w, err, "发送邮箱验证码")
		return
	}
	writeJSON(w, http.StatusOK, recoveryEmailVerificationResponse{
		Success:           true,
		Message:           "验证码已发送，请查收邮件。",
		Status:            "pending",
		MaskedEmail:       result.MaskedEmail,
		ExpiresAt:         result.ExpiresAt.UTC().Format(time.RFC3339),
		RetryAfterSeconds: result.RetryAfterSeconds,
	})
}

func (h *Handler) VerifyRecoveryEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	user, ok := h.userFromRequest(w, r)
	if !ok {
		return
	}
	if h.recoveryEmailVerification == nil {
		writeError(w, http.StatusServiceUnavailable, "邮箱验证服务暂不可用")
		return
	}

	var request verificationCodeRequest
	if err := decodeJSON(r, &request); err != nil || !recoveryemailverification.ValidCode(strings.TrimSpace(request.Code)) {
		writeError(w, http.StatusBadRequest, "请输入 6 位数字验证码")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	result, err := h.recoveryEmailVerification.Verify(ctx, user.ID, strings.TrimSpace(request.Code))
	if err != nil {
		h.writeRecoveryEmailVerificationError(w, err, "验证找回邮箱")
		return
	}
	writeJSON(w, http.StatusOK, recoveryEmailVerificationResponse{
		Success:     true,
		Message:     "找回邮箱验证成功。",
		Status:      "verified",
		MaskedEmail: result.MaskedEmail,
		VerifiedAt:  result.VerifiedAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) writeRecoveryEmailVerificationError(w http.ResponseWriter, err error, operation string) {
	switch {
	case errors.Is(err, recoveryemailverification.ErrInvalidCode):
		writeError(w, http.StatusBadRequest, "请输入 6 位数字验证码")
	case errors.Is(err, recoveryemailverification.ErrCodeMismatch):
		writeError(w, http.StatusBadRequest, "验证码不正确")
	case errors.Is(err, recoveryemailverification.ErrAttemptsExhausted):
		writeError(w, http.StatusTooManyRequests, "验证码错误次数过多，请重新发送验证码")
	case recoveryemailverification.RetryAfterSeconds(err) > 0:
		seconds := recoveryemailverification.RetryAfterSeconds(err)
		writeJSON(w, http.StatusTooManyRequests, recoveryEmailVerificationResponse{
			Success: false, Message: "发送过于频繁，请稍后再试。", RetryAfterSeconds: seconds,
		})
	case errors.Is(err, recoveryemailverification.ErrUserInactive):
		h.clearSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "查询登录已失效，请重新登录")
	case recoveryemailverification.IsStateConflict(err):
		writeError(w, http.StatusConflict, recoveryEmailVerificationConflictMessage(err))
	case errors.Is(err, recoveryemailverification.ErrUnavailable), errors.Is(err, recoveryemailverification.ErrDeliveryFailed):
		writeError(w, http.StatusServiceUnavailable, "邮箱验证服务暂不可用，请稍后再试")
	default:
		log.Printf("%s: %v", operation, err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
	}
}

func recoveryEmailVerificationConflictMessage(err error) string {
	switch {
	case errors.Is(err, recoveryemailverification.ErrNoRecoveryEmail):
		return "尚未登记找回邮箱，请联系管理员登记。"
	case errors.Is(err, recoveryemailverification.ErrEmailDisabled):
		return "当前找回邮箱不可用，请联系管理员。"
	case errors.Is(err, recoveryemailverification.ErrAlreadyVerified):
		return "当前找回邮箱已经验证。"
	case errors.Is(err, recoveryemailverification.ErrCodeExpired):
		return "验证码已过期，请重新发送。"
	case errors.Is(err, recoveryemailverification.ErrCodeUsed):
		return "验证码已经使用。"
	default:
		return "当前验证码不可用，请重新发送。"
	}
}
