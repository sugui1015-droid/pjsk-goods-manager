package users

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"pjsk/backend/internal/admin"

	"github.com/jackc/pgx/v5"
)

var (
	ErrMergeSameUser        = errors.New("source and target must be different users")
	ErrMergeSourceNotActive = errors.New("source user is already merged or missing")
	ErrMergeTargetNotActive = errors.New("target user must be an active, non-merged user")
	ErrMergeReasonRequired  = errors.New("merge reason is required")
)

type MergePreviewResponse struct {
	Source                ListItem `json:"source"`
	Target                ListItem `json:"target"`
	MoveOrderCount        int      `json:"move_order_count"`
	MovePaymentCount      int      `json:"move_payment_count"`
	MoveQuerySessionCount int      `json:"move_query_session_count"`
}

type MergeRequest struct {
	SourceUserID string `json:"source_user_id"`
	TargetUserID string `json:"target_user_id"`
	Reason       string `json:"reason"`
}

type MergeResponse struct {
	SourceUserID      string `json:"source_user_id"`
	TargetUserID      string `json:"target_user_id"`
	MovedOrderCount   int    `json:"moved_order_count"`
	MovedPaymentCount int    `json:"moved_payment_count"`
	MergedAt          string `json:"merged_at"`
}

type MergeLogEntry struct {
	ID        string `json:"id"`
	Direction string `json:"direction"` // "merged_into" (this user was absorbed) or "absorbed" (this user received)
	OtherCN   string `json:"other_cn"`
	Reason    string `json:"reason"`
	MergedBy  string `json:"merged_by,omitempty"`
	MergedAt  string `json:"merged_at"`
}

func (h *Handler) MergePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	sourceID := strings.TrimSpace(r.URL.Query().Get("source_id"))
	targetCN := strings.TrimSpace(r.URL.Query().Get("target_cn"))
	if !isUUIDLike(sourceID) {
		writeError(w, http.StatusBadRequest, "source_id is invalid")
		return
	}
	if targetCN == "" {
		writeError(w, http.StatusBadRequest, "target_cn is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.PreviewMerge(ctx, sourceID, targetCN)
	if err != nil {
		writeMergeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Merge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request MergeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !isUUIDLike(request.SourceUserID) || !isUUIDLike(request.TargetUserID) {
		writeError(w, http.StatusBadRequest, "user ids are invalid")
		return
	}
	if strings.TrimSpace(request.Reason) == "" {
		writeError(w, http.StatusBadRequest, ErrMergeReasonRequired.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	response, err := h.store.MergeUsers(ctx, request, account.ID)
	if err != nil {
		writeMergeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func writeMergeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrMergeSameUser),
		errors.Is(err, ErrMergeSourceNotActive),
		errors.Is(err, ErrMergeTargetNotActive),
		errors.Is(err, ErrMergeReasonRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("cn merge error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (s *PostgresStore) PreviewMerge(ctx context.Context, sourceID string, targetCN string) (MergePreviewResponse, error) {
	source, err := s.userSummaryByID(ctx, sourceID)
	if err != nil {
		return MergePreviewResponse{}, err
	}
	target, err := s.userSummaryByCN(ctx, targetCN)
	if err != nil {
		return MergePreviewResponse{}, err
	}
	if source.ID == target.ID {
		return MergePreviewResponse{}, ErrMergeSameUser
	}
	if source.Status == "merged" {
		return MergePreviewResponse{}, ErrMergeSourceNotActive
	}
	if target.Status != "active" {
		return MergePreviewResponse{}, ErrMergeTargetNotActive
	}

	var orderCount, paymentCount, querySessionCount int
	if err := s.pool.QueryRow(ctx, `
		select
			(select count(*) from orders where user_id = $1::uuid),
			(select count(*) from payments where user_id = $1::uuid),
			(select count(*) from query_sessions where user_id = $1::uuid)
	`, source.ID).Scan(&orderCount, &paymentCount, &querySessionCount); err != nil {
		return MergePreviewResponse{}, err
	}

	return MergePreviewResponse{
		Source:                source,
		Target:                target,
		MoveOrderCount:        orderCount,
		MovePaymentCount:      paymentCount,
		MoveQuerySessionCount: querySessionCount,
	}, nil
}

func (s *PostgresStore) MergeUsers(ctx context.Context, request MergeRequest, adminID string) (MergeResponse, error) {
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		return MergeResponse{}, ErrMergeReasonRequired
	}
	if request.SourceUserID == request.TargetUserID {
		return MergeResponse{}, ErrMergeSameUser
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MergeResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock both user rows in a stable order to avoid deadlocks between
	// concurrent merges.
	firstID, secondID := request.SourceUserID, request.TargetUserID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}
	type lockedUser struct {
		id     string
		cn     string
		status string
	}
	locked := map[string]lockedUser{}
	rows, err := tx.Query(ctx, `
		select id::text, cn_code, status
		from users
		where id in ($1::uuid, $2::uuid)
		order by id
		for update
	`, firstID, secondID)
	if err != nil {
		return MergeResponse{}, err
	}
	for rows.Next() {
		var u lockedUser
		if err := rows.Scan(&u.id, &u.cn, &u.status); err != nil {
			rows.Close()
			return MergeResponse{}, err
		}
		locked[u.id] = u
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return MergeResponse{}, err
	}

	source, sourceOK := locked[request.SourceUserID]
	target, targetOK := locked[request.TargetUserID]
	if !sourceOK || !targetOK {
		return MergeResponse{}, ErrUserNotFound
	}
	if source.status == "merged" {
		return MergeResponse{}, ErrMergeSourceNotActive
	}
	if target.status != "active" {
		return MergeResponse{}, ErrMergeTargetNotActive
	}

	orderTag, err := tx.Exec(ctx, `update orders set user_id = $2::uuid, updated_at = now() where user_id = $1::uuid`, source.id, target.id)
	if err != nil {
		return MergeResponse{}, err
	}
	paymentTag, err := tx.Exec(ctx, `update payments set user_id = $2::uuid, updated_at = now() where user_id = $1::uuid`, source.id, target.id)
	if err != nil {
		return MergeResponse{}, err
	}
	if _, err := tx.Exec(ctx, `update query_sessions set user_id = $2::uuid where user_id = $1::uuid`, source.id, target.id); err != nil {
		return MergeResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status='invalidated', invalidated_at=coalesce(invalidated_at,now()), updated_at=now()
		where user_id=$1::uuid and purpose='query_code_recovery'
		  and status in ('sending','active') and invalidated_at is null
	`, source.id); err != nil {
		return MergeResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_sessions
		set status='invalidated', invalidated_at=coalesce(invalidated_at,now()), updated_at=now()
		where user_id=$1::uuid and purpose='query_code_recovery'
		  and status='active' and invalidated_at is null
	`, source.id); err != nil {
		return MergeResponse{}, err
	}
	if _, err := tx.Exec(ctx, `update users set status = 'merged', query_code_hash = null, updated_at = now() where id = $1::uuid`, source.id); err != nil {
		return MergeResponse{}, err
	}

	var mergedAt string
	err = tx.QueryRow(ctx, `
		insert into cn_merge_logs (source_user_id, target_user_id, source_cn, target_cn, moved_order_count, moved_payment_count, reason, merged_by)
		values ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8::uuid)
		returning to_char(merged_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	`, source.id, target.id, source.cn, target.cn, orderTag.RowsAffected(), paymentTag.RowsAffected(), reason, adminID).Scan(&mergedAt)
	if err != nil {
		return MergeResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return MergeResponse{}, err
	}
	return MergeResponse{
		SourceUserID:      source.id,
		TargetUserID:      target.id,
		MovedOrderCount:   int(orderTag.RowsAffected()),
		MovedPaymentCount: int(paymentTag.RowsAffected()),
		MergedAt:          mergedAt,
	}, nil
}

func (s *PostgresStore) userSummaryByID(ctx context.Context, id string) (ListItem, error) {
	return s.userSummary(ctx, "u.id = $1::uuid", id)
}

func (s *PostgresStore) userSummaryByCN(ctx context.Context, cn string) (ListItem, error) {
	return s.userSummary(ctx, `lower(regexp_replace(btrim(u.cn_code), '\s+', ' ', 'g')) = lower($1)`, normalizeCN(cn))
}

func (s *PostgresStore) userSummary(ctx context.Context, condition string, arg any) (ListItem, error) {
	var item ListItem
	err := s.pool.QueryRow(ctx, `
		with `+paidByItemCTE+`
		select
			u.id::text,
			u.cn_code,
			coalesce(u.display_name, ''),
			(coalesce(u.query_code_hash, '') <> ''),
			u.status,
			coalesce(t.order_count, 0),
			coalesce(t.total_amount, 0),
			coalesce(t.paid_amount, 0),
			greatest(coalesce(t.total_amount, 0) - coalesce(t.paid_amount, 0), 0),
			to_char(u.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from users u
		left join user_totals t on t.user_id = u.id
		where `+condition, arg).Scan(
		&item.ID,
		&item.CNCode,
		&item.DisplayName,
		&item.HasQueryCode,
		&item.Status,
		&item.OrderCount,
		&item.TotalAmount,
		&item.PaidAmount,
		&item.RemainingAmount,
		&item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ListItem{}, ErrUserNotFound
	}
	if err != nil {
		return ListItem{}, err
	}
	item.TotalAmount = round2(item.TotalAmount)
	item.PaidAmount = round2(item.PaidAmount)
	item.RemainingAmount = round2(item.RemainingAmount)
	return item, nil
}

func (s *PostgresStore) listMergeLogs(ctx context.Context, userID string) ([]MergeLogEntry, error) {
	rows, err := s.pool.Query(ctx, `
		select
			m.id::text,
			case when m.source_user_id = $1::uuid then 'merged_into' else 'absorbed' end,
			case when m.source_user_id = $1::uuid then m.target_cn else m.source_cn end,
			m.reason,
			coalesce(a.username, ''),
			to_char(m.merged_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from cn_merge_logs m
		left join admins a on a.id = m.merged_by
		where m.source_user_id = $1::uuid or m.target_user_id = $1::uuid
		order by m.merged_at desc
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []MergeLogEntry{}
	for rows.Next() {
		var entry MergeLogEntry
		if err := rows.Scan(&entry.ID, &entry.Direction, &entry.OtherCN, &entry.Reason, &entry.MergedBy, &entry.MergedAt); err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

func normalizeCN(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
