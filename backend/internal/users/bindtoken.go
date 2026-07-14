package users

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/querycode"

	"github.com/jackc/pgx/v5"
)

// bindTokenTTL is fixed for the MVP: long enough for an admin to hand the
// code to a verified user, short enough to limit exposure.
const bindTokenTTL = 30 * time.Minute

var (
	ErrBindTokenUserHasCode  = errors.New("user already has a query code")
	ErrBindTokenUserInactive = errors.New("user is not active")
)

type BindTokenResponse struct {
	BindToken string `json:"bind_token"`
	ExpiresAt string `json:"expires_at"`
	Message   string `json:"message"`
}

// CreateBindToken handles POST /api/admin/users/{id}/query-code-bind-token.
// Admin authentication is enforced by the router middleware; the plaintext
// token appears exactly once, in this response, and is never logged or
// stored.
func (h *Handler) CreateBindToken(w http.ResponseWriter, r *http.Request, userID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.CreateQueryCodeBindToken(ctx, userID, account.ID)
	switch {
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrBindTokenUserHasCode):
		writeError(w, http.StatusConflict, "该用户已设置查询码，不能生成首次绑定码")
	case errors.Is(err, ErrBindTokenUserInactive):
		writeError(w, http.StatusConflict, "已停用或已合并的用户不能生成绑定码")
	case err != nil:
		log.Printf("create query code bind token: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
	default:
		writeJSON(w, http.StatusOK, response)
	}
}

func (s *PostgresStore) CreateQueryCodeBindToken(ctx context.Context, userID string, adminID string) (BindTokenResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BindTokenResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status, currentHash string
	err = tx.QueryRow(ctx, `
		select status, coalesce(query_code_hash, '')
		from users
		where id = $1::uuid
		for update
	`, userID).Scan(&status, &currentHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return BindTokenResponse{}, ErrUserNotFound
	}
	if err != nil {
		return BindTokenResponse{}, err
	}
	if status != "active" {
		return BindTokenResponse{}, ErrBindTokenUserInactive
	}
	if currentHash != "" {
		return BindTokenResponse{}, ErrBindTokenUserHasCode
	}

	// Regenerating always kills every previous unused token for this user.
	if _, err := tx.Exec(ctx, `
		update user_query_code_bind_tokens
		set invalidated_at = now()
		where user_id = $1::uuid and used_at is null and invalidated_at is null
	`, userID); err != nil {
		return BindTokenResponse{}, err
	}

	token, err := querycode.GenerateBindToken()
	if err != nil {
		return BindTokenResponse{}, err
	}
	var expiresAt string
	err = tx.QueryRow(ctx, `
		insert into user_query_code_bind_tokens (user_id, token_hash, expires_at, created_by_admin_id)
		values ($1::uuid, $2, now() + $3::interval, $4::uuid)
		returning to_char(expires_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	`, userID, querycode.HashBindToken(token), bindTokenTTL.String(), adminID).Scan(&expiresAt)
	if err != nil {
		return BindTokenResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BindTokenResponse{}, err
	}
	return BindTokenResponse{
		BindToken: token,
		ExpiresAt: expiresAt,
		Message:   "绑定码仅显示一次，请安全交给用户。",
	}, nil
}
