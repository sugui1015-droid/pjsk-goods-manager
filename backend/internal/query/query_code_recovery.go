package query

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"pjsk/backend/internal/querycode"
	"pjsk/backend/internal/querycoderecovery"
)

const (
	queryCodeRecoveryPublicMessage = "如果该账号符合找回条件，验证码将发送至已登记邮箱。"
	queryCodeRecoveryRejectMessage = "验证码无效或已过期，请重新开始找回流程。"
	queryCodeRecoveryMinimumDelay  = 250 * time.Millisecond
)

type QueryCodeRecoveryService interface {
	Request(context.Context, string, string) error
	Verify(context.Context, string, string) (querycoderecovery.VerifiedCode, error)
	Reset(context.Context, string, string, string) error
}

type queryCodeRecoveryRequest struct {
	CN string `json:"cn"`
}

type queryCodeRecoveryVerifyRequest struct {
	CN   string `json:"cn"`
	Code string `json:"code"`
}

type queryCodeRecoveryResetRequest struct {
	ResetToken       string `json:"reset_token"`
	NewQueryCode     string `json:"new_query_code"`
	ConfirmQueryCode string `json:"confirm_query_code"`
}

type queryCodeRecoveryResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	ResetToken string `json:"reset_token,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

func (h *Handler) ConfigureQueryCodeRecovery(service QueryCodeRecoveryService) {
	h.queryCodeRecovery = service
}

func (h *Handler) RequestQueryCodeRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	var request queryCodeRecoveryRequest
	if err := decodeJSON(r, &request); err != nil || querycoderecovery.NormalizeCN(request.CN) == "" {
		writeError(w, http.StatusBadRequest, "请输入 CN")
		return
	}
	started := time.Now()
	defer waitForPublicRecoveryResponse(started)
	if h.queryCodeRecovery != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		_ = h.queryCodeRecovery.Request(ctx, request.CN, clientIP(r))
		cancel()
	}
	writeJSON(w, http.StatusOK, queryCodeRecoveryResponse{Success: true, Message: queryCodeRecoveryPublicMessage})
}

func (h *Handler) VerifyQueryCodeRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	var request queryCodeRecoveryVerifyRequest
	if err := decodeJSON(r, &request); err != nil || querycoderecovery.NormalizeCN(request.CN) == "" || !querycoderecovery.ValidCode(strings.TrimSpace(request.Code)) {
		writeError(w, http.StatusBadRequest, "请输入 CN 和 6 位数字验证码")
		return
	}
	started := time.Now()
	defer waitForPublicRecoveryResponse(started)
	if h.queryCodeRecovery == nil {
		writeError(w, http.StatusServiceUnavailable, "查询码找回服务暂不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	result, err := h.queryCodeRecovery.Verify(ctx, request.CN, strings.TrimSpace(request.Code))
	if err != nil {
		switch {
		case errors.Is(err, querycoderecovery.ErrRejected), errors.Is(err, querycoderecovery.ErrCodeMismatch), errors.Is(err, querycoderecovery.ErrAttemptsExhausted):
			writeError(w, http.StatusBadRequest, queryCodeRecoveryRejectMessage)
		case errors.Is(err, querycoderecovery.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "查询码找回服务暂不可用")
		default:
			log.Printf("verify query code recovery: %v", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
		}
		return
	}
	writeJSON(w, http.StatusOK, queryCodeRecoveryResponse{Success: true, Message: "邮箱验证成功，请设置新的查询码。", ResetToken: result.ResetToken, ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339)})
}

func (h *Handler) ResetRecoveredQueryCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	var request queryCodeRecoveryResetRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	token := strings.TrimSpace(request.ResetToken)
	newQueryCode := strings.TrimSpace(request.NewQueryCode)
	confirmQueryCode := strings.TrimSpace(request.ConfirmQueryCode)
	if !querycoderecovery.ValidToken(token) {
		writeError(w, http.StatusBadRequest, "重置会话无效或已过期，请重新开始找回流程")
		return
	}
	if newQueryCode == "" || newQueryCode != confirmQueryCode {
		writeError(w, http.StatusBadRequest, "两次输入的新查询码不一致")
		return
	}
	if querycode.Validate(newQueryCode) != nil {
		writeError(w, http.StatusBadRequest, "查询码格式不正确")
		return
	}
	if h.queryCodeRecovery == nil {
		writeError(w, http.StatusServiceUnavailable, "查询码找回服务暂不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	err := h.queryCodeRecovery.Reset(ctx, token, newQueryCode, confirmQueryCode)
	if err != nil {
		switch {
		case errors.Is(err, querycoderecovery.ErrSameQueryCode):
			writeError(w, http.StatusBadRequest, "新查询码不能与旧查询码相同")
		case errors.Is(err, querycoderecovery.ErrRejected), errors.Is(err, querycoderecovery.ErrInvalidToken):
			writeError(w, http.StatusBadRequest, "重置会话无效或已过期，请重新开始找回流程")
		case errors.Is(err, querycode.ErrInvalid):
			writeError(w, http.StatusBadRequest, "查询码格式不正确")
		case errors.Is(err, querycoderecovery.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "查询码找回服务暂不可用")
		default:
			log.Printf("reset recovered query code: %v", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
		}
		return
	}
	h.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, queryCodeRecoveryResponse{Success: true, Message: "查询码已重置，请使用新查询码登录。"})
}

func waitForPublicRecoveryResponse(started time.Time) {
	if remaining := queryCodeRecoveryMinimumDelay - time.Since(started); remaining > 0 {
		time.Sleep(remaining)
	}
}
