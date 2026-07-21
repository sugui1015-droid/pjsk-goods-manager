package query

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/querycode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// bindTokenMaxFailures invalidates a bind token after this many wrong
// attempts, independent of the per-IP rate limiter.
const bindTokenMaxFailures = 5

// ErrBindRejected is the single rejection error for the anonymous bind
// endpoint. It deliberately collapses "CN not found", "user disabled or
// merged", "user already has a query code", "no active token", "token
// mismatch", "token expired/used/invalidated", and "token belongs to a
// different user" into one case so the response cannot be used to probe
// account state.
var ErrBindRejected = errors.New("bind code rejected")

type bindCodeRequest struct {
	CN               string `json:"cn"`
	BindToken        string `json:"bind_token"`
	NewQueryCode     string `json:"new_query_code"`
	ConfirmQueryCode string `json:"confirm_query_code"`
}

type bindCodeResponse struct {
	Message string `json:"message"`
}

// BindCode handles POST /api/query/bind-code — the anonymous first-time
// query-code setup flow. Heavily rate limited; never creates a session.
func (h *Handler) BindCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	var request bindCodeRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	cn := normalizeCN(request.CN)
	bindToken := strings.TrimSpace(request.BindToken)
	newQueryCode := querycode.Normalize(request.NewQueryCode)
	confirmQueryCode := querycode.Normalize(request.ConfirmQueryCode)
	if cn == "" || bindToken == "" || newQueryCode == "" || confirmQueryCode == "" {
		writeError(w, http.StatusBadRequest, "请完整输入 CN、绑定码和新查询码")
		return
	}
	if newQueryCode != confirmQueryCode {
		writeError(w, http.StatusBadRequest, "两次输入的新查询码不一致")
		return
	}
	if err := querycode.Validate(newQueryCode); err != nil {
		writeError(w, http.StatusBadRequest, "查询码格式不正确")
		return
	}

	ip := h.resolveClientIP(r)
	limiterKey := "bind:" + cn
	if !h.limiter.allow(ip, limiterKey, h.now()) {
		writeError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newQueryCode), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash bind query code: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	err = h.store.BindQueryCode(ctx, cn, querycode.HashBindToken(bindToken), string(newHash))
	if errors.Is(err, ErrBindRejected) {
		h.limiter.recordFailure(ip, limiterKey, h.now())
		writeError(w, http.StatusUnauthorized, "CN 或绑定码不正确")
		return
	}
	if err != nil {
		log.Printf("bind query code: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	h.limiter.recordSuccess(ip, limiterKey)
	// Only reached once BindQueryCode has committed — a valid one-time bind
	// token is strong proof of identity, so any login block this IP+CN
	// accumulated while guessing the old code is now stale and must go.
	h.limiter.release(ip, limiterCNKey(cn), h.now())
	writeJSON(w, http.StatusOK, bindCodeResponse{Message: "查询码设置成功，请使用新查询码登录。"})
}

// BindQueryCode performs the first-time bind. tokenHash is the SHA-256 of
// the user-supplied token; plaintext never reaches the store layer.
func (s *PostgresStore) BindQueryCode(ctx context.Context, cn string, tokenHash string, newQueryCodeHash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID, status, currentHash string
	err = tx.QueryRow(ctx, `
		select id::text, status, coalesce(query_code_hash, '')
		from users
		where lower(regexp_replace(btrim(cn_code), '\s+', ' ', 'g')) = lower($1)
		for update
	`, cn).Scan(&userID, &status, &currentHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBindRejected
	}
	if err != nil {
		return err
	}
	if status != "active" || currentHash != "" {
		return ErrBindRejected
	}

	var tokenID, storedHash string
	var failedAttempts int
	err = tx.QueryRow(ctx, `
		select id::text, token_hash, failed_attempts
		from user_query_code_bind_tokens
		where user_id = $1::uuid and used_at is null and invalidated_at is null and expires_at > now()
		order by created_at desc
		limit 1
		for update
	`, userID).Scan(&tokenID, &storedHash, &failedAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBindRejected
	}
	if err != nil {
		return err
	}

	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(tokenHash)) != 1 {
		// Record the failed attempt (and invalidate the token at the cap)
		// inside this same transaction, then COMMIT the counter update even
		// though the bind is rejected — otherwise attackers would get
		// unlimited tries.
		if _, err := tx.Exec(ctx, `
			update user_query_code_bind_tokens
			set failed_attempts = failed_attempts + 1,
				invalidated_at = case when failed_attempts + 1 >= $2 then now() else invalidated_at end
			where id = $1::uuid
		`, tokenID, bindTokenMaxFailures); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return ErrBindRejected
	}

	if _, err := tx.Exec(ctx, `
		update users
		set query_code_hash = $2, query_code_updated_at = now(), updated_at = now()
		where id = $1::uuid
	`, userID, newQueryCodeHash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update user_query_code_bind_tokens set used_at = now() where id = $1::uuid
	`, tokenID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update user_query_code_bind_tokens
		set invalidated_at = now()
		where user_id = $1::uuid and id <> $2::uuid and used_at is null and invalidated_at is null
	`, userID, tokenID); err != nil {
		return err
	}
	// A user without a query code normally has no sessions, but clear any
	// abnormal leftovers defensively.
	if _, err := tx.Exec(ctx, `delete from query_sessions where user_id = $1::uuid`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
