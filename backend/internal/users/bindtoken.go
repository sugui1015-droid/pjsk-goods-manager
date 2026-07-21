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

// bindTokenTTL is fixed: long enough for an admin to hand the code to a
// verified user across time zones and offline delays, short enough to limit
// exposure. Regenerating invalidates every earlier unused token.
const bindTokenTTL = 24 * time.Hour

// bulkBindTokenTTL is longer than the single-user TTL because a batch is
// handed out through slower channels — a group announcement, a spreadsheet
// passed around — and users trickle in over days. The cost is real: the
// exported file is a working key for every listed account until it expires,
// which is why the batch is capped and gated on a fresh re-authentication.
const bulkBindTokenTTL = 7 * 24 * time.Hour

// maxBulkBindTokens caps one batch. The user list export allows 50k rows, but
// issuing 50k live bind codes into a single spreadsheet by a mis-click is a
// different order of mistake, so a batch that large is refused outright.
const maxBulkBindTokens = 500

// ErrBulkBindTokenTooMany means the filter selected more users than one batch
// may issue codes for. The admin is expected to narrow the filter, not to
// retry.
var ErrBulkBindTokenTooMany = errors.New("bulk bind token batch too large")

// BulkBindToken is one row of a bulk issue. BindToken is plaintext and lives
// only in memory on its way into the download — it is never stored, logged, or
// retrievable from any other endpoint.
type BulkBindToken struct {
	CNCode         string
	DisplayName    string
	BindToken      string
	ExpiresAt      string
	ReplacedUnused bool
}

// BulkBindTokenSkip records one user the batch selected but did not issue to.
// A skip is never silent: the count and every reason travel back with the
// batch so the admin can see that the file has fewer codes than the preview
// promised, and why.
type BulkBindTokenSkip struct {
	CNCode      string
	DisplayName string
	Reason      string
}

// Skip reasons. These describe a state change between the candidate read and
// the locked re-check — the user bound a code, was disabled or merged, or was
// removed while the batch was running.
const (
	SkipReasonNowHasQueryCode = "已在本次生成期间设置查询码"
	SkipReasonNotActive       = "已停用或已合并"
	SkipReasonUserGone        = "用户已不存在"
)

// BulkBindTokenResult is the outcome of one batch. Requested is how many
// candidates the filter selected; len(Issued)+len(Skipped) always equals it,
// so a shortfall cannot go unaccounted for.
type BulkBindTokenResult struct {
	Requested int
	Issued    []BulkBindToken
	Skipped   []BulkBindTokenSkip
}

// BulkBindTokenPreview is the count shown for confirmation before a batch runs.
type BulkBindTokenPreview struct {
	EligibleCount       int `json:"eligible_count"`
	ExistingUnusedCount int `json:"existing_unused_count"`
	MaxBatchSize        int `json:"max_batch_size"`
	ValidDays           int `json:"valid_days"`
}

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

// BulkBindTokenPreview handles GET /api/admin/users/bind-token-batch-preview.
// It is a pure read: it tells the admin how large the batch is and how many
// users would lose a still-valid older code, so the confirmation shown before
// the batch is not produced by the call that performs it.
func (h *Handler) BulkBindTokenPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	filters, err := FiltersFromQuery(r.URL.Query())
	if err != nil {
		writeFilterError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	preview, err := h.store.PreviewBulkQueryCodeBindTokens(ctx, filters)
	if err != nil {
		log.Printf("preview bulk bind tokens: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// PreviewBulkQueryCodeBindTokens counts what a batch would affect without
// issuing anything. It is deliberately a separate read: the confirmation the
// admin sees must not be produced by the same call that mutates.
func (s *PostgresStore) PreviewBulkQueryCodeBindTokens(ctx context.Context, filters Filters) (BulkBindTokenPreview, error) {
	query, args := BuildBulkBindTokenPreviewQuery(filters, maxBulkBindTokens+1)
	preview := BulkBindTokenPreview{
		MaxBatchSize: maxBulkBindTokens,
		ValidDays:    int(bulkBindTokenTTL / (24 * time.Hour)),
	}
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&preview.EligibleCount, &preview.ExistingUnusedCount); err != nil {
		return BulkBindTokenPreview{}, err
	}
	return preview, nil
}

// BulkCreateQueryCodeBindTokens issues one fresh bind code per eligible user in
// the filter result, in a single transaction: either the whole batch is issued
// or none of it is, so a partial failure can never leave codes live that the
// admin never received in the download.
//
// Every user's earlier unused codes are invalidated. That is unavoidable rather
// than incidental — the old plaintext is unrecoverable by design, so a user
// left holding an old code could not appear in the spreadsheet at all. The
// preview reports how many users this affects.
func (s *PostgresStore) BulkCreateQueryCodeBindTokens(ctx context.Context, filters Filters, adminID string) (BulkBindTokenResult, error) {
	query, args := BuildBulkBindTokenQuery(filters, maxBulkBindTokens+1)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return BulkBindTokenResult{}, err
	}
	type candidate struct{ id, cn, displayName string }
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.cn, &item.displayName); err != nil {
			rows.Close()
			return BulkBindTokenResult{}, err
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return BulkBindTokenResult{}, err
	}
	if len(candidates) > maxBulkBindTokens {
		return BulkBindTokenResult{}, ErrBulkBindTokenTooMany
	}
	result := BulkBindTokenResult{
		Requested: len(candidates),
		Issued:    []BulkBindToken{},
		Skipped:   []BulkBindTokenSkip{},
	}
	if len(candidates) == 0 {
		return result, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BulkBindTokenResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	skip := func(item candidate, reason string) {
		result.Skipped = append(result.Skipped, BulkBindTokenSkip{
			CNCode:      item.cn,
			DisplayName: item.displayName,
			Reason:      reason,
		})
	}

	for _, item := range candidates {
		// Re-check eligibility under a row lock. The candidate list was read
		// outside the transaction, so a user may have bound a query code or
		// been disabled in between. Such a user is skipped rather than issued a
		// code that could never work — and the skip is recorded, never silent.
		var status, currentHash string
		err := tx.QueryRow(ctx, `
			select status, coalesce(query_code_hash, '')
			from users
			where id = $1::uuid
			for update
		`, item.id).Scan(&status, &currentHash)
		if errors.Is(err, pgx.ErrNoRows) {
			skip(item, SkipReasonUserGone)
			continue
		}
		if err != nil {
			return BulkBindTokenResult{}, err
		}
		if status != "active" {
			skip(item, SkipReasonNotActive)
			continue
		}
		if currentHash != "" {
			skip(item, SkipReasonNowHasQueryCode)
			continue
		}

		tag, err := tx.Exec(ctx, `
			update user_query_code_bind_tokens
			set invalidated_at = now()
			where user_id = $1::uuid
				and used_at is null
				and invalidated_at is null
				and expires_at > now()
		`, item.id)
		if err != nil {
			return BulkBindTokenResult{}, err
		}

		token, err := querycode.GenerateBindToken()
		if err != nil {
			return BulkBindTokenResult{}, err
		}
		var expiresAt string
		if err := tx.QueryRow(ctx, `
			insert into user_query_code_bind_tokens (user_id, token_hash, expires_at, created_by_admin_id)
			values ($1::uuid, $2, now() + $3::interval, $4::uuid)
			returning to_char(expires_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		`, item.id, querycode.HashBindToken(token), bulkBindTokenTTL.String(), adminID).Scan(&expiresAt); err != nil {
			return BulkBindTokenResult{}, err
		}

		result.Issued = append(result.Issued, BulkBindToken{
			CNCode:         item.cn,
			DisplayName:    item.displayName,
			BindToken:      token,
			ExpiresAt:      expiresAt,
			ReplacedUnused: tag.RowsAffected() > 0,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return BulkBindTokenResult{}, err
	}
	return result, nil
}
