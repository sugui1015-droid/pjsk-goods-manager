package users

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/querycode"
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidQueryCode    = errors.New("invalid query code")
	ErrInvalidQueryStatus  = errors.New("invalid query status")
	ErrQueryCodeAlreadySet = errors.New("query code already set")
	ErrQueryCodeNotSet     = errors.New("query code not set")
)

type Handler struct {
	store                  Store
	recoveryEmailStore     RecoveryEmailStore
	recoveryEmailProtector RecoveryEmailProtector
}

type Store interface {
	ListUsers(context.Context, Filters) (ListResponse, error)
	UserFacets(context.Context, FacetRequest) (FacetResponse, error)
	GetUserDetail(context.Context, string) (DetailResponse, error)
	SetQueryCode(context.Context, string, string, bool) (ListItem, error)
	SetQueryAccessStatus(context.Context, string, string) (ListItem, error)
	CreateQueryCodeBindToken(context.Context, string, string) (BindTokenResponse, error)
	PreviewMerge(context.Context, string, string) (MergePreviewResponse, error)
	MergeUsers(context.Context, MergeRequest, string) (MergeResponse, error)
}

// ListItem is one row of the admin user table.
//
// The two account-security columns are booleans by construction: the query code
// is reduced to "is one set" and the recovery email to "is one bound". The hash,
// the ciphertext and the lookup hash are never selected out of the database (see
// baseCTE), so this DTO cannot leak them however it changes.
//
// ID is for keying rows and navigating to the detail page — never a column to
// render.
type ListItem struct {
	ID                 string  `json:"id"`
	CNCode             string  `json:"cn_code"`
	DisplayName        string  `json:"display_name,omitempty"`
	HasQueryCode       bool    `json:"has_query_code"`
	HasRecoveryEmail   bool    `json:"has_recovery_email"`
	Status             string  `json:"status"`
	OrderCount         int     `json:"order_count"`
	TotalAmount        float64 `json:"total_amount"`
	PaidAmount         float64 `json:"paid_amount"`
	RemainingAmount    float64 `json:"remaining_amount"`
	CreatedAt          string  `json:"created_at"`
	QueryCodeUpdatedAt string  `json:"query_code_updated_at,omitempty"`
	LastLoginAt        string  `json:"last_login_at,omitempty"`
}

type queryCodeRequest struct {
	QueryCode string `json:"query_code"`
}

type queryAccessStatusRequest struct {
	Status string `json:"status"`
}
type ListSummary struct {
	UserCount       int     `json:"user_count"`
	UsersWithOrders int     `json:"users_with_orders"`
	TotalAmount     float64 `json:"total_amount"`
	PaidAmount      float64 `json:"paid_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
}

// ListResponse is one page of the filtered result set.
//
// Total counts every user matching the filters, not just this page, and Summary
// aggregates over that same full set — so the tiles and "结果：共 N 位用户" always
// describe the same thing.
type ListResponse struct {
	Items      []ListItem  `json:"items"`
	Summary    ListSummary `json:"summary"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	Total      int         `json:"total"`
	TotalPages int         `json:"total_pages"`
}

type DetailOrder struct {
	ID              string            `json:"id"`
	OrderNo         string            `json:"order_no"`
	Status          string            `json:"status"`
	ProjectName     string            `json:"project_name"`
	ItemCount       int               `json:"item_count"`
	TotalAmount     float64           `json:"total_amount"`
	PaidAmount      float64           `json:"paid_amount"`
	RemainingAmount float64           `json:"remaining_amount"`
	CreatedAt       string            `json:"created_at"`
	Items           []DetailOrderItem `json:"items"`
}

type DetailOrderItem struct {
	ID              string  `json:"id"`
	ProductName     string  `json:"product_name"`
	ProductID       string  `json:"product_id,omitempty"`
	CharacterName   string  `json:"character_name,omitempty"`
	Category        string  `json:"category,omitempty"`
	SeriesCode      string  `json:"series_code,omitempty"`
	DisplayName     string  `json:"display_name,omitempty"`
	SKU             string  `json:"sku,omitempty"`
	Quantity        float64 `json:"quantity"`
	UnitPrice       float64 `json:"unit_price"`
	Amount          float64 `json:"amount"`
	PaidAmount      float64 `json:"paid_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
	PaymentStatus   string  `json:"payment_status"`
	ImportFilename  string  `json:"import_filename,omitempty"`
	SourceSheet     string  `json:"source_sheet,omitempty"`
	SourceRowKey    string  `json:"source_row_key,omitempty"`
}

type DetailPayment struct {
	ID              string  `json:"id"`
	PrincipalAmount float64 `json:"principal_amount"`
	FeeAmount       float64 `json:"fee_amount"`
	TotalAmount     float64 `json:"total_amount"`
	PaymentMethod   string  `json:"payment_method,omitempty"`
	Status          string  `json:"status"`
	PaidAt          string  `json:"paid_at"`
	CreatedBy       string  `json:"created_by,omitempty"`
	VoidedAt        string  `json:"voided_at,omitempty"`
	VoidedBy        string  `json:"voided_by,omitempty"`
	VoidReason      string  `json:"void_reason,omitempty"`
}

type DetailResponse struct {
	User            ListItem        `json:"user"`
	Orders          []DetailOrder   `json:"orders"`
	Payments        []DetailPayment `json:"payments"`
	ImportFilenames []string        `json:"import_filenames"`
	Merges          []MergeLogEntry `json:"merges"`
	// Bind-token status only — the token itself and its hash are never
	// exposed on the detail response.
	HasActiveBindToken bool   `json:"has_active_bind_token"`
	BindTokenExpiresAt string `json:"bind_token_expires_at,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	filters, err := FiltersFromQuery(r.URL.Query())
	if err != nil {
		writeFilterError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	response, err := h.store.ListUsers(ctx, filters)
	if err != nil {
		log.Printf("list users: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// Facets serves the candidate values for one column's filter popover.
func (h *Handler) Facets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	request, err := FacetRequestFromQuery(r.URL.Query())
	if err != nil {
		writeFilterError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	response, err := h.store.UserFacets(ctx, request)
	if err != nil {
		writeFilterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// writeFilterError reports a rejected parameter. Only BadRequestError messages
// reach the client; anything else is treated as internal so no database detail
// leaks through the filter surface.
func writeFilterError(w http.ResponseWriter, err error) {
	var badRequestErr *BadRequestError
	if errors.As(err, &badRequestErr) {
		writeError(w, http.StatusBadRequest, badRequestErr.Message)
		return
	}
	log.Printf("admin users filters: %s", logsafe.Category(err))
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func (h *Handler) Detail(w http.ResponseWriter, r *http.Request) {
	id, action := userIDAndAction(r.URL.Path)
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !isUUIDLike(id) {
		writeError(w, http.StatusBadRequest, "user id is invalid")
		return
	}

	if action != "" {
		switch action {
		case "query-code":
			h.UpdateQueryCode(w, r, id)
		case "status":
			h.UpdateQueryAccessStatus(w, r, id)
		case "query-code-bind-token":
			h.CreateBindToken(w, r, id)
		case "recovery-email":
			h.RecoveryEmail(w, r, id)
		default:
			writeError(w, http.StatusNotFound, "user not found")
		}
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.GetUserDetail(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		log.Printf("user detail: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) UpdateQueryCode(w http.ResponseWriter, r *http.Request, userID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	var request queryCodeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	queryCode := strings.TrimSpace(request.QueryCode)
	if err := validateQueryCode(queryCode); err != nil {
		writeError(w, http.StatusBadRequest, "查询码格式不正确")
		return
	}
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(queryCode), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash user query code: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	current, err := h.store.GetUserDetail(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		log.Printf("load user before query code update: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	user, err := h.store.SetQueryCode(ctx, userID, string(hashBytes), current.User.HasQueryCode)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if errors.Is(err, ErrQueryCodeAlreadySet) || errors.Is(err, ErrQueryCodeNotSet) {
		writeError(w, http.StatusConflict, "查询码状态已变化，请刷新后重试")
		return
	}
	if err != nil {
		log.Printf("update user query code: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]ListItem{"user": user})
}

func (h *Handler) UpdateQueryAccessStatus(w http.ResponseWriter, r *http.Request, userID string) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	var request queryAccessStatusRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	status := strings.TrimSpace(request.Status)
	if status != "active" && status != "disabled" {
		writeError(w, http.StatusBadRequest, "用户状态不正确")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	user, err := h.store.SetQueryAccessStatus(ctx, userID, status)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if errors.Is(err, ErrInvalidQueryStatus) {
		writeError(w, http.StatusBadRequest, "用户状态不正确")
		return
	}
	if err != nil {
		log.Printf("update user query status: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]ListItem{"user": user})
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const paidByItemCTE = `
	paid_by_item as (
		select
			pi.order_item_id,
			coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
		from payment_items pi
		join payments p on p.id = pi.payment_id
		group by pi.order_item_id
	),
	user_totals as (
		select
			o.user_id,
			count(distinct o.id)::int as order_count,
			coalesce(sum(oi.amount), 0)::float8 as total_amount,
			coalesce(sum(least(coalesce(paid.paid_amount, 0), oi.amount)), 0)::float8 as paid_amount
		from orders o
		join order_items oi on oi.order_id = o.id and oi.revoked_at is null
		left join paid_by_item paid on paid.order_item_id = oi.id
		where o.status <> 'cancelled'
		group by o.user_id
	)
`

// scanListItems reads the shared list column set (see listColumns).
func scanListItems(rows pgx.Rows) ([]ListItem, error) {
	items := []ListItem{}
	for rows.Next() {
		var item ListItem
		if err := rows.Scan(
			&item.ID,
			&item.CNCode,
			&item.DisplayName,
			&item.HasQueryCode,
			&item.HasRecoveryEmail,
			&item.Status,
			&item.OrderCount,
			&item.TotalAmount,
			&item.PaidAmount,
			&item.RemainingAmount,
			&item.CreatedAt,
			&item.QueryCodeUpdatedAt,
			&item.LastLoginAt,
		); err != nil {
			return nil, err
		}
		item.TotalAmount = round2(item.TotalAmount)
		item.PaidAmount = round2(item.PaidAmount)
		item.RemainingAmount = round2(item.RemainingAmount)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) ListUsers(ctx context.Context, filters Filters) (ListResponse, error) {
	response := ListResponse{
		Items:    []ListItem{},
		Page:     filters.Page,
		PageSize: filters.PageSize,
	}

	countQuery, countArgs := buildCountQuery(filters)
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&response.Total); err != nil {
		return ListResponse{}, err
	}
	response.TotalPages = (response.Total + filters.PageSize - 1) / filters.PageSize

	// The summary is aggregated in SQL over the whole filtered set rather than
	// summed from the scanned page: a page of 50 out of 300 users must not make
	// the tiles disagree with the reported total.
	summaryQuery, summaryArgs := buildSummaryQuery(filters)
	if err := s.pool.QueryRow(ctx, summaryQuery, summaryArgs...).Scan(
		&response.Summary.UserCount,
		&response.Summary.UsersWithOrders,
		&response.Summary.TotalAmount,
		&response.Summary.PaidAmount,
		&response.Summary.RemainingAmount,
	); err != nil {
		return ListResponse{}, err
	}
	response.Summary.TotalAmount = round2(response.Summary.TotalAmount)
	response.Summary.PaidAmount = round2(response.Summary.PaidAmount)
	response.Summary.RemainingAmount = round2(response.Summary.RemainingAmount)

	listQuery, listArgs := buildListQuery(filters)
	rows, err := s.pool.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return ListResponse{}, err
	}
	defer rows.Close()
	items, err := scanListItems(rows)
	if err != nil {
		return ListResponse{}, err
	}
	response.Items = items
	return response, nil
}

// ExportUsers returns every user matching the filters, capped at maxRows and
// ignoring list pagination.
func (s *PostgresStore) ExportUsers(ctx context.Context, filters Filters, maxRows int) ([]ListItem, error) {
	query, args := BuildExportQuery(filters, maxRows)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanListItems(rows)
}
func (s *PostgresStore) GetUserDetail(ctx context.Context, userID string) (DetailResponse, error) {
	var detail DetailResponse
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
			to_char(u.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(u.query_code_updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(to_char(u.last_query_login_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		from users u
		left join user_totals t on t.user_id = u.id
		where u.id = $1::uuid
	`, userID).Scan(
		&detail.User.ID,
		&detail.User.CNCode,
		&detail.User.DisplayName,
		&detail.User.HasQueryCode,
		&detail.User.Status,
		&detail.User.OrderCount,
		&detail.User.TotalAmount,
		&detail.User.PaidAmount,
		&detail.User.RemainingAmount,
		&detail.User.CreatedAt,
		&detail.User.QueryCodeUpdatedAt,
		&detail.User.LastLoginAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DetailResponse{}, ErrUserNotFound
	}
	if err != nil {
		return DetailResponse{}, err
	}
	detail.User.TotalAmount = round2(detail.User.TotalAmount)
	detail.User.PaidAmount = round2(detail.User.PaidAmount)
	detail.User.RemainingAmount = round2(detail.User.RemainingAmount)

	orders, err := s.listOrdersForUser(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.Orders = orders

	payments, err := s.listPaymentsForUser(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.Payments = payments

	filenames, err := s.listImportFilenames(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.ImportFilenames = filenames

	merges, err := s.listMergeLogs(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.Merges = merges

	if err := s.pool.QueryRow(ctx, `
		select
			exists(select 1 from user_query_code_bind_tokens
				where user_id = $1::uuid and used_at is null and invalidated_at is null and expires_at > now()),
			coalesce((select to_char(max(expires_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
				from user_query_code_bind_tokens
				where user_id = $1::uuid and used_at is null and invalidated_at is null and expires_at > now()), '')
	`, userID).Scan(&detail.HasActiveBindToken, &detail.BindTokenExpiresAt); err != nil {
		return DetailResponse{}, err
	}
	return detail, nil
}

func (s *PostgresStore) SetQueryCode(ctx context.Context, userID string, queryCodeHash string, reset bool) (ListItem, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ListItem{}, err
	}
	defer tx.Rollback(ctx)

	var hasQueryCode bool
	err = tx.QueryRow(ctx, `
		select (coalesce(query_code_hash, '') <> '')
		from users
		where id = $1::uuid
		for update
	`, userID).Scan(&hasQueryCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return ListItem{}, ErrUserNotFound
	}
	if err != nil {
		return ListItem{}, err
	}
	if reset && !hasQueryCode {
		return ListItem{}, ErrQueryCodeNotSet
	}
	if !reset && hasQueryCode {
		return ListItem{}, ErrQueryCodeAlreadySet
	}
	if _, err := tx.Exec(ctx, `
		update users
		set query_code_hash = $2,
			query_code_updated_at = now(),
			updated_at = now()
		where id = $1::uuid
	`, userID, queryCodeHash); err != nil {
		return ListItem{}, err
	}
	if _, err := tx.Exec(ctx, `delete from query_sessions where user_id = $1::uuid`, userID); err != nil {
		return ListItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ListItem{}, err
	}
	return s.getListItem(ctx, userID)
}

func (s *PostgresStore) SetQueryAccessStatus(ctx context.Context, userID string, status string) (ListItem, error) {
	if status != "active" && status != "disabled" {
		return ListItem{}, ErrInvalidQueryStatus
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ListItem{}, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		update users
		set status = $2,
			updated_at = now()
		where id = $1::uuid
		  and status <> 'merged'
	`, userID, status)
	if err != nil {
		return ListItem{}, err
	}
	if tag.RowsAffected() == 0 {
		return ListItem{}, ErrUserNotFound
	}
	if status == "disabled" {
		if _, err := tx.Exec(ctx, `delete from query_sessions where user_id = $1::uuid`, userID); err != nil {
			return ListItem{}, err
		}
		if _, err := tx.Exec(ctx, `
			update query_code_recovery_codes
			set status='invalidated', invalidated_at=coalesce(invalidated_at,now()), updated_at=now()
			where user_id=$1::uuid and purpose='query_code_recovery'
			  and status in ('sending','active') and invalidated_at is null
		`, userID); err != nil {
			return ListItem{}, err
		}
		if _, err := tx.Exec(ctx, `
			update query_code_recovery_sessions
			set status='invalidated', invalidated_at=coalesce(invalidated_at,now()), updated_at=now()
			where user_id=$1::uuid and purpose='query_code_recovery'
			  and status='active' and invalidated_at is null
		`, userID); err != nil {
			return ListItem{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ListItem{}, err
	}
	return s.getListItem(ctx, userID)
}

func (s *PostgresStore) getListItem(ctx context.Context, userID string) (ListItem, error) {
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
			to_char(u.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(u.query_code_updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(to_char(u.last_query_login_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		from users u
		left join user_totals t on t.user_id = u.id
		where u.id = $1::uuid
	`, userID).Scan(
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
		&item.QueryCodeUpdatedAt,
		&item.LastLoginAt,
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
func (s *PostgresStore) listOrdersForUser(ctx context.Context, userID string) ([]DetailOrder, error) {
	rows, err := s.pool.Query(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments p on p.id = pi.payment_id
			group by pi.order_item_id
		)
		select
			o.id::text,
			o.order_no,
			o.status,
			p.name,
			count(oi.id)::int,
			coalesce(sum(oi.amount), 0)::float8,
			coalesce(sum(least(coalesce(paid.paid_amount, 0), oi.amount)), 0)::float8,
			to_char(o.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from orders o
		join projects p on p.id = o.project_id
		left join order_items oi on oi.order_id = o.id and oi.revoked_at is null
		left join paid_by_item paid on paid.order_item_id = oi.id
		where o.user_id = $1::uuid
		  and o.status <> 'cancelled'
		group by o.id, o.order_no, o.status, p.name, o.created_at
		order by o.created_at desc, o.id desc
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []DetailOrder{}
	for rows.Next() {
		var order DetailOrder
		if err := rows.Scan(
			&order.ID,
			&order.OrderNo,
			&order.Status,
			&order.ProjectName,
			&order.ItemCount,
			&order.TotalAmount,
			&order.PaidAmount,
			&order.CreatedAt,
		); err != nil {
			return nil, err
		}
		order.TotalAmount = round2(order.TotalAmount)
		order.PaidAmount = round2(order.PaidAmount)
		order.RemainingAmount = round2(order.TotalAmount - order.PaidAmount)
		if order.RemainingAmount < 0 {
			order.RemainingAmount = 0
		}
		items, err := s.listOrderItemsForUserDetail(ctx, order.ID)
		if err != nil {
			return nil, err
		}
		order.Items = items
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func (s *PostgresStore) listOrderItemsForUserDetail(ctx context.Context, orderID string) ([]DetailOrderItem, error) {
	rows, err := s.pool.Query(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments p on p.id = pi.payment_id
			group by pi.order_item_id
		)
		select
			oi.id::text,
			product.name,
			product.id::text,
			coalesce(product.character_name, ''),
			coalesce(product.category, ''),
			coalesce(product.series_code, ''),
			product.name,
			coalesce(product.sku, ''),
			oi.quantity::float8,
			oi.unit_price::float8,
			oi.amount::float8,
			least(coalesce(paid.paid_amount, 0), oi.amount)::float8,
			greatest(oi.amount - coalesce(paid.paid_amount, 0), 0)::float8,
			case
				when coalesce(paid.paid_amount, 0) <= 0 then 'unpaid'
				when coalesce(paid.paid_amount, 0) + 0.004 >= oi.amount then 'paid'
				else 'partial'
			end,
			coalesce(ib.original_filename, ''),
			coalesce(oi.source_sheet, ''),
			coalesce(oi.source_row_key, '')
		from order_items oi
		join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		left join paid_by_item paid on paid.order_item_id = oi.id
		where oi.order_id = $1::uuid
		  and oi.revoked_at is null
		order by product.sort_order, product.name, product.character_name, oi.created_at, oi.id
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []DetailOrderItem{}
	for rows.Next() {
		var item DetailOrderItem
		if err := rows.Scan(
			&item.ID,
			&item.ProductName,
			&item.ProductID,
			&item.CharacterName,
			&item.Category,
			&item.SeriesCode,
			&item.DisplayName,
			&item.SKU,
			&item.Quantity,
			&item.UnitPrice,
			&item.Amount,
			&item.PaidAmount,
			&item.RemainingAmount,
			&item.PaymentStatus,
			&item.ImportFilename,
			&item.SourceSheet,
			&item.SourceRowKey,
		); err != nil {
			return nil, err
		}
		item.Amount = round2(item.Amount)
		item.PaidAmount = round2(item.PaidAmount)
		item.RemainingAmount = round2(item.RemainingAmount)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) listPaymentsForUser(ctx context.Context, userID string) ([]DetailPayment, error) {
	rows, err := s.pool.Query(ctx, `
		select
			p.id::text,
			p.submitted_amount::float8,
			p.fee_amount::float8,
			p.payable_amount::float8,
			coalesce(p.payment_method, ''),
			p.status,
			to_char(coalesce(p.paid_at, p.approved_at, p.submitted_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(a.username, ''),
			coalesce(to_char(p.voided_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(voider.username, ''),
			coalesce(p.void_reason, '')
		from payments p
		left join admins a on a.id = coalesce(p.created_by, p.approved_by)
		left join admins voider on voider.id = p.voided_by_admin_id
		where p.user_id = $1::uuid
		order by coalesce(p.paid_at, p.approved_at, p.submitted_at) desc, p.created_at desc, p.id desc
		limit 200
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := []DetailPayment{}
	for rows.Next() {
		var payment DetailPayment
		if err := rows.Scan(
			&payment.ID,
			&payment.PrincipalAmount,
			&payment.FeeAmount,
			&payment.TotalAmount,
			&payment.PaymentMethod,
			&payment.Status,
			&payment.PaidAt,
			&payment.CreatedBy,
			&payment.VoidedAt,
			&payment.VoidedBy,
			&payment.VoidReason,
		); err != nil {
			return nil, err
		}
		payment.PrincipalAmount = round2(payment.PrincipalAmount)
		payment.FeeAmount = round2(payment.FeeAmount)
		payment.TotalAmount = round2(payment.TotalAmount)
		payments = append(payments, payment)
	}
	return payments, rows.Err()
}

func (s *PostgresStore) listImportFilenames(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		select distinct ib.original_filename
		from order_items oi
		join orders o on o.id = oi.order_id
		join import_batches ib on ib.id = oi.import_batch_id
		where o.user_id = $1::uuid
		  and oi.revoked_at is null
		  and ib.original_filename is not null
		order by ib.original_filename
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	filenames := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		filenames = append(filenames, name)
	}
	return filenames, rows.Err()
}

func userIDAndAction(path string) (string, string) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/admin/users/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], strings.Join(parts[1:], "/")
}

func validateQueryCode(value string) error {
	if err := querycode.Validate(value); err != nil {
		return ErrInvalidQueryCode
	}
	return nil
}
func isUUIDLike(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
				return false
			}
		}
	}
	return true
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode users JSON response: %v", err)
	}
}
