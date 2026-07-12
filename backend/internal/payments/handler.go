package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pjsk/backend/internal/admin"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCNRequired           = errors.New("cn is required")
	ErrUserNotFound         = errors.New("cn not found")
	ErrNoPaymentItems       = errors.New("payment items are required")
	ErrInvalidAmount        = errors.New("payment amount must be greater than 0")
	ErrOverPayment          = errors.New("payment amount exceeds remaining amount")
	ErrItemMismatch         = errors.New("order item does not belong to this cn")
	ErrIdempotencyKey       = errors.New("idempotency key is required")
	ErrPaymentTime          = errors.New("payment time is invalid")
	ErrPaymentNotFound      = errors.New("payment not found")
	ErrVoidReasonRequired   = errors.New("void reason is required")
	ErrPaymentAlreadyVoid   = errors.New("payment is already voided")
	ErrPaymentNotApproved   = errors.New("only approved payments can be voided")
	ErrInvalidPaymentMethod = errors.New("payment_method must be 'alipay' or 'wechat'")
)

type Handler struct {
	store Store
}

type Store interface {
	GetCNPayment(context.Context, string) (CNPaymentResponse, error)
	GetCNUnpaidPayment(context.Context, string) (CNPaymentResponse, error)
	CreatePayment(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error)
	ListPaymentRecords(context.Context, PaymentFilters) (PaymentListResponse, error)
	GetPaymentDetail(context.Context, string) (PaymentDetailResponse, error)
	VoidPayment(context.Context, VoidPaymentRequest, string) (PaymentDetailResponse, error)
}

type CNPaymentResponse struct {
	User     PaymentUser      `json:"user"`
	Summary  PaymentSummary   `json:"summary"`
	Items    []PaymentItemRow `json:"items"`
	Payments []PaymentRecord  `json:"payments"`
}

type PaymentUser struct {
	ID          string  `json:"id"`
	CNCode      string  `json:"cn_code"`
	DisplayName *string `json:"display_name,omitempty"`
}

type PaymentSummary struct {
	TotalAmount     float64 `json:"total_amount"`
	PaidAmount      float64 `json:"paid_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
	ItemCount       int     `json:"item_count"`
	UnpaidCount     int     `json:"unpaid_count"`
	PartialCount    int     `json:"partial_count"`
	PaidCount       int     `json:"paid_count"`
}

type PaymentItemRow struct {
	ID              string  `json:"id"`
	OrderItemID     string  `json:"order_item_id"`
	OrderID         string  `json:"order_id"`
	OrderNo         string  `json:"order_no"`
	ProjectName     string  `json:"project_name"`
	ProductName     string  `json:"product_name"`
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

type PaymentRecord struct {
	ID            string  `json:"id"`
	Amount        float64 `json:"amount"`
	FeeAmount     float64 `json:"fee_amount"`
	PayableAmount float64 `json:"payable_amount"`
	PaymentMethod string  `json:"payment_method,omitempty"`
	Note          string  `json:"note,omitempty"`
	Status        string  `json:"status"`
	PaidAt        string  `json:"paid_at"`
	CreatedBy     string  `json:"created_by,omitempty"`
	CreatedAt     string  `json:"created_at"`
	VoidedAt      string  `json:"voided_at,omitempty"`
	VoidedBy      string  `json:"voided_by,omitempty"`
	VoidReason    string  `json:"void_reason,omitempty"`
}

type CreatePaymentRequest struct {
	CN             string                     `json:"cn"`
	PaymentMethod  string                     `json:"payment_method"`
	PaidAt         string                     `json:"paid_at"`
	Note           string                     `json:"note"`
	IdempotencyKey string                     `json:"idempotency_key"`
	Items          []CreatePaymentItemRequest `json:"items"`
}

type CreatePaymentItemRequest struct {
	OrderItemID string  `json:"order_item_id"`
	Amount      float64 `json:"amount"`
}

type CreatePaymentResponse struct {
	PaymentID string           `json:"payment_id"`
	Status    string           `json:"status"`
	Duplicate bool             `json:"duplicate"`
	Summary   PaymentSummary   `json:"summary"`
	Items     []PaymentItemRow `json:"items"`
}

type VoidPaymentRequest struct {
	PaymentID string `json:"-"`
	Reason    string `json:"reason"`
}

type PaymentFilters struct {
	CN            string
	PaymentMethod string
	Status        string
	PaidFrom      string
	PaidTo        string
	Limit         int
}

type PaymentListItem struct {
	ID               string  `json:"id"`
	CNCode           string  `json:"cn_code"`
	DisplayName      string  `json:"display_name,omitempty"`
	Amount           float64 `json:"amount"`
	FeeAmount        float64 `json:"fee_amount"`
	PayableAmount    float64 `json:"payable_amount"`
	PaymentMethod    string  `json:"payment_method,omitempty"`
	Status           string  `json:"status"`
	PaidAt           string  `json:"paid_at"`
	CreatedBy        string  `json:"created_by,omitempty"`
	Note             string  `json:"note,omitempty"`
	PaymentItemCount int     `json:"payment_item_count"`
	CreatedAt        string  `json:"created_at"`
	VoidedAt         string  `json:"voided_at,omitempty"`
	VoidedBy         string  `json:"voided_by,omitempty"`
	VoidReason       string  `json:"void_reason,omitempty"`
}

type PaymentListResponse struct {
	Items []PaymentListItem `json:"items"`
}

type PaymentDetailResponse struct {
	Payment PaymentDetail `json:"payment"`
}

type PaymentDetail struct {
	PaymentListItem
	UserID string              `json:"user_id"`
	Items  []PaymentDetailItem `json:"items"`
}

type PaymentDetailItem struct {
	ID             string  `json:"id"`
	OrderItemID    string  `json:"order_item_id"`
	OrderID        string  `json:"order_id"`
	OrderNo        string  `json:"order_no"`
	ProjectName    string  `json:"project_name"`
	ProductName    string  `json:"product_name"`
	CharacterName  string  `json:"character_name,omitempty"`
	Category       string  `json:"category,omitempty"`
	SeriesCode     string  `json:"series_code,omitempty"`
	DisplayName    string  `json:"display_name,omitempty"`
	SKU            string  `json:"sku,omitempty"`
	AppliedAmount  float64 `json:"applied_amount"`
	PaymentStatus  string  `json:"payment_status"`
	ImportFilename string  `json:"import_filename,omitempty"`
	SourceSheet    string  `json:"source_sheet,omitempty"`
	SourceRowKey   string  `json:"source_row_key,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.List(w, r)
	case http.MethodPost:
		h.Create(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	filters, err := paymentFiltersFromRequest(r)
	if err != nil {
		writePaymentError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.ListPaymentRecords(ctx, filters)
	if err != nil {
		log.Printf("list payments: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Detail(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/payments/"), "/")
	if path == "" {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}
	if strings.HasSuffix(path, "/void") {
		paymentID := strings.Trim(strings.TrimSuffix(path, "/void"), "/")
		h.Void(w, r, paymentID)
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if strings.Contains(path, "/") {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}
	if !isUUIDLike(path) {
		writeError(w, http.StatusBadRequest, "payment id is invalid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.GetPaymentDetail(ctx, path)
	if err != nil {
		writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Void(w http.ResponseWriter, r *http.Request, paymentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if paymentID == "" || strings.Contains(paymentID, "/") {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}
	if !isUUIDLike(paymentID) {
		writeError(w, http.StatusBadRequest, "payment id is invalid")
		return
	}

	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request VoidPaymentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	request.PaymentID = paymentID

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.VoidPayment(ctx, request, account.ID)
	if err != nil {
		writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) CN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	cn := strings.TrimSpace(r.URL.Query().Get("cn"))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.GetCNPayment(ctx, cn)
	if err != nil {
		writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Unpaid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	cn := strings.TrimSpace(r.URL.Query().Get("cn"))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.GetCNUnpaidPayment(ctx, cn)
	if err != nil {
		writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request CreatePaymentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.CreatePayment(ctx, request, account.ID)
	if err != nil {
		log.Printf("CreatePayment failed: cn=%q method=%q ik=%q item_count=%d err=%v",
			request.CN, request.PaymentMethod, request.IdempotencyKey, len(request.Items), err)
		writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func paymentFiltersFromRequest(r *http.Request) (PaymentFilters, error) {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(strings.TrimSpace(query.Get("limit")))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	paidFrom := strings.TrimSpace(query.Get("paid_from"))
	paidTo := strings.TrimSpace(query.Get("paid_to"))
	if err := validateOptionalPaymentTime(paidFrom); err != nil {
		return PaymentFilters{}, err
	}
	if err := validateOptionalPaymentTime(paidTo); err != nil {
		return PaymentFilters{}, err
	}
	return PaymentFilters{
		CN:            strings.TrimSpace(query.Get("cn")),
		PaymentMethod: strings.TrimSpace(query.Get("payment_method")),
		Status:        strings.TrimSpace(query.Get("status")),
		PaidFrom:      paidFrom,
		PaidTo:        paidTo,
		Limit:         limit,
	}, nil
}

func validateOptionalPaymentTime(value string) error {
	if value == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if _, err := time.Parse(layout, value); err == nil {
			return nil
		}
	}
	return ErrPaymentTime
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

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) GetCNPayment(ctx context.Context, cn string) (CNPaymentResponse, error) {
	cn = normalizeCN(cn)
	if cn == "" {
		return CNPaymentResponse{}, ErrCNRequired
	}

	user, err := s.findUser(ctx, cn)
	if err != nil {
		return CNPaymentResponse{}, err
	}
	items, summary, err := s.listItemsForUser(ctx, user.ID)
	if err != nil {
		return CNPaymentResponse{}, err
	}
	records, err := s.listPaymentsForUser(ctx, user.ID)
	if err != nil {
		return CNPaymentResponse{}, err
	}
	return CNPaymentResponse{User: user, Summary: summary, Items: items, Payments: records}, nil
}

func (s *PostgresStore) GetCNUnpaidPayment(ctx context.Context, cn string) (CNPaymentResponse, error) {
	cn = normalizeCN(cn)
	if cn == "" {
		return CNPaymentResponse{}, ErrCNRequired
	}

	user, err := s.findUser(ctx, cn)
	if err != nil {
		return CNPaymentResponse{}, err
	}
	items, err := s.listUnpaidItemsForUser(ctx, user.ID)
	if err != nil {
		return CNPaymentResponse{}, err
	}
	records, err := s.listPaymentsForUser(ctx, user.ID)
	if err != nil {
		return CNPaymentResponse{}, err
	}
	return CNPaymentResponse{User: user, Summary: summarizePaymentItems(items), Items: items, Payments: records}, nil
}

func (s *PostgresStore) CreatePayment(ctx context.Context, request CreatePaymentRequest, adminID string) (CreatePaymentResponse, error) {
	cn := normalizeCN(request.CN)
	if cn == "" {
		return CreatePaymentResponse{}, ErrCNRequired
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		return CreatePaymentResponse{}, ErrIdempotencyKey
	}
	if len(request.Items) == 0 {
		return CreatePaymentResponse{}, ErrNoPaymentItems
	}
	paidAt, err := parsePaymentTime(request.PaidAt)
	if err != nil {
		return CreatePaymentResponse{}, ErrPaymentTime
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreatePaymentResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "select pg_advisory_xact_lock(hashtext($1))", request.IdempotencyKey); err != nil {
		return CreatePaymentResponse{}, err
	}

	var existingPaymentID string
	err = tx.QueryRow(ctx, `
		select id::text
		from payments
		where idempotency_key = $1
	`, request.IdempotencyKey).Scan(&existingPaymentID)
	if err == nil {
		user, err := findUserTx(ctx, tx, cn)
		if err != nil {
			return CreatePaymentResponse{}, err
		}
		items, summary, err := listItemsForUserTx(ctx, tx, user.ID)
		if err != nil {
			return CreatePaymentResponse{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CreatePaymentResponse{}, err
		}
		return CreatePaymentResponse{PaymentID: existingPaymentID, Status: "approved", Duplicate: true, Summary: summary, Items: items}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return CreatePaymentResponse{}, err
	}

	user, err := findUserTx(ctx, tx, cn)
	if err != nil {
		return CreatePaymentResponse{}, err
	}

	requestItems := mergeRequestItems(request.Items)
	ids := make([]string, 0, len(requestItems))
	for id, amount := range requestItems {
		if strings.TrimSpace(id) == "" || amount <= 0 {
			return CreatePaymentResponse{}, ErrInvalidAmount
		}
		ids = append(ids, id)
	}

	itemRows, err := lockPaymentItems(ctx, tx, user.ID, ids)
	if err != nil {
		return CreatePaymentResponse{}, err
	}
	if len(itemRows) != len(ids) {
		return CreatePaymentResponse{}, ErrItemMismatch
	}

	total := 0.0
	for _, item := range itemRows {
		amount := round2(requestItems[item.ID])
		if amount <= 0 {
			return CreatePaymentResponse{}, ErrInvalidAmount
		}
		if amount-item.RemainingAmount > 0.005 {
			return CreatePaymentResponse{}, ErrOverPayment
		}
		total = round2(total + amount)
	}
	if total <= 0 {
		return CreatePaymentResponse{}, ErrInvalidAmount
	}

	paymentMethod := strings.TrimSpace(request.PaymentMethod)
	if paymentMethod != "alipay" && paymentMethod != "wechat" {
		return CreatePaymentResponse{}, ErrInvalidPaymentMethod
	}

	// Convert to integer cents for fee calculation — no float64 arithmetic on fees.
	baseCents := safeCentsFromFloat64(total)
	feeCents, payableCents := calculateFee(baseCents, paymentMethod)

	// Format as strings for numeric(12,2) columns to avoid float64 precision loss.
	submittedAmountStr := centsToNumeric(baseCents)
	feeAmountStr := centsToNumeric(feeCents)
	payableAmountStr := centsToNumeric(payableCents)

	var paymentID string
	err = tx.QueryRow(ctx, `
		insert into payments (
			user_id,
			submitted_amount,
			fee_amount,
			payable_amount,
			payment_method,
			note,
			status,
			submitted_at,
			approved_at,
			approved_by,
			paid_at,
			created_by,
			idempotency_key
		)
		values ($1::uuid, $2::numeric(12,2), $3::numeric(12,2), $4::numeric(12,2), $5, $6, 'approved', now(), now(), $7::uuid, $8, $7::uuid, $9)
		returning id::text
	`, user.ID, submittedAmountStr, feeAmountStr, payableAmountStr, paymentMethod, strings.TrimSpace(request.Note), adminID, paidAt, request.IdempotencyKey).Scan(&paymentID)
	if err != nil {
		return CreatePaymentResponse{}, err
	}

	for _, item := range itemRows {
		if _, err := tx.Exec(ctx, `
			insert into payment_items (payment_id, order_item_id, applied_amount)
			values ($1::uuid, $2::uuid, $3)
		`, paymentID, item.ID, round2(requestItems[item.ID])); err != nil {
			return CreatePaymentResponse{}, err
		}
	}

	if err := recalculateUserPaymentStatus(ctx, tx, user.ID); err != nil {
		return CreatePaymentResponse{}, err
	}

	items, summary, err := listItemsForUserTx(ctx, tx, user.ID)
	if err != nil {
		return CreatePaymentResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return CreatePaymentResponse{}, err
	}
	return CreatePaymentResponse{PaymentID: paymentID, Status: "approved", Summary: summary, Items: items}, nil
}

func (s *PostgresStore) ListPaymentRecords(ctx context.Context, filters PaymentFilters) (PaymentListResponse, error) {
	conditions := []string{"1 = 1"}
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	if filters.CN != "" {
		placeholder := addArg("%" + filters.CN + "%")
		conditions = append(conditions, "(u.cn_code ilike "+placeholder+" or coalesce(u.display_name, '') ilike "+placeholder+")")
	}
	if filters.PaymentMethod != "" {
		placeholder := addArg(filters.PaymentMethod)
		conditions = append(conditions, "lower(coalesce(p.payment_method, '')) = lower("+placeholder+")")
	}
	if filters.Status != "" {
		placeholder := addArg(filters.Status)
		conditions = append(conditions, "p.status = "+placeholder)
	}
	if filters.PaidFrom != "" {
		placeholder := addArg(filters.PaidFrom)
		conditions = append(conditions, "coalesce(p.paid_at, p.approved_at, p.submitted_at) >= "+placeholder+"::timestamptz")
	}
	if filters.PaidTo != "" {
		placeholder := addArg(filters.PaidTo)
		conditions = append(conditions, "coalesce(p.paid_at, p.approved_at, p.submitted_at) < "+placeholder+"::timestamptz")
	}
	limitPlaceholder := addArg(filters.Limit)

	rows, err := s.pool.Query(ctx, `
		select
			p.id::text,
			u.cn_code,
			coalesce(u.display_name, ''),
			p.submitted_amount::float8,
			p.fee_amount::float8,
			p.payable_amount::float8,
			coalesce(p.payment_method, ''),
			p.status,
			to_char(coalesce(p.paid_at, p.approved_at, p.submitted_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(a.username, ''),
			coalesce(p.note, ''),
			count(pi.id)::int,
			to_char(p.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(p.voided_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(voider.username, ''),
			coalesce(p.void_reason, '')
		from payments p
		join users u on u.id = p.user_id
		left join admins a on a.id = coalesce(p.created_by, p.approved_by)
		left join admins voider on voider.id = p.voided_by_admin_id
		left join payment_items pi on pi.payment_id = p.id
		where `+strings.Join(conditions, " and ")+`
		group by p.id, u.cn_code, u.display_name, p.submitted_amount, p.fee_amount, p.payable_amount, p.payment_method, p.status, p.paid_at, p.approved_at, p.submitted_at, a.username, p.note, p.created_at, p.voided_at, voider.username, p.void_reason
		order by coalesce(p.paid_at, p.approved_at, p.submitted_at) desc, p.created_at desc, p.id desc
		limit `+limitPlaceholder, args...)
	if err != nil {
		return PaymentListResponse{}, err
	}
	defer rows.Close()

	response := PaymentListResponse{Items: []PaymentListItem{}}
	for rows.Next() {
		var item PaymentListItem
		if err := rows.Scan(&item.ID, &item.CNCode, &item.DisplayName, &item.Amount, &item.FeeAmount, &item.PayableAmount, &item.PaymentMethod, &item.Status, &item.PaidAt, &item.CreatedBy, &item.Note, &item.PaymentItemCount, &item.CreatedAt, &item.VoidedAt, &item.VoidedBy, &item.VoidReason); err != nil {
			return PaymentListResponse{}, err
		}
		item.Amount = round2(item.Amount)
		item.FeeAmount = round2(item.FeeAmount)
		item.PayableAmount = round2(item.PayableAmount)
		response.Items = append(response.Items, item)
	}
	if err := rows.Err(); err != nil {
		return PaymentListResponse{}, err
	}
	return response, nil
}

func (s *PostgresStore) GetPaymentDetail(ctx context.Context, paymentID string) (PaymentDetailResponse, error) {
	var detail PaymentDetail
	err := s.pool.QueryRow(ctx, `
		select
			p.id::text,
			p.user_id::text,
			u.cn_code,
			coalesce(u.display_name, ''),
			p.submitted_amount::float8,
			p.fee_amount::float8,
			p.payable_amount::float8,
			coalesce(p.payment_method, ''),
			p.status,
			to_char(coalesce(p.paid_at, p.approved_at, p.submitted_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(a.username, ''),
			coalesce(p.note, ''),
			(select count(*)::int from payment_items pi_count where pi_count.payment_id = p.id),
			to_char(p.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(p.voided_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(voider.username, ''),
			coalesce(p.void_reason, '')
		from payments p
		join users u on u.id = p.user_id
		left join admins a on a.id = coalesce(p.created_by, p.approved_by)
		left join admins voider on voider.id = p.voided_by_admin_id
		where p.id = $1::uuid
	`, paymentID).Scan(
		&detail.ID,
		&detail.UserID,
		&detail.CNCode,
		&detail.DisplayName,
		&detail.Amount,
		&detail.FeeAmount,
		&detail.PayableAmount,
		&detail.PaymentMethod,
		&detail.Status,
		&detail.PaidAt,
		&detail.CreatedBy,
		&detail.Note,
		&detail.PaymentItemCount,
		&detail.CreatedAt,
		&detail.VoidedAt,
		&detail.VoidedBy,
		&detail.VoidReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentDetailResponse{}, ErrPaymentNotFound
	}
	if err != nil {
		return PaymentDetailResponse{}, err
	}
	detail.Amount = round2(detail.Amount)
	detail.FeeAmount = round2(detail.FeeAmount)
	detail.PayableAmount = round2(detail.PayableAmount)

	rows, err := s.pool.Query(ctx, `
		select
			pi.id::text,
			oi.id::text,
			o.id::text,
			o.order_no,
			project.name,
			product.name,
			coalesce(product.character_name, ''),
			coalesce(product.category, ''),
			coalesce(product.series_code, ''),
			case when coalesce(product.category, '') = '' then product.name else product.name || '-' || product.category end,
			coalesce(product.sku, ''),
			pi.applied_amount::float8,
			oi.payment_status,
			coalesce(ib.original_filename, ''),
			coalesce(oi.source_sheet, ''),
			coalesce(oi.source_row_key, '')
		from payment_items pi
		join order_items oi on oi.id = pi.order_item_id
		join orders o on o.id = oi.order_id
		join projects project on project.id = o.project_id
		join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		where pi.payment_id = $1::uuid
		order by o.order_no, product.sort_order, product.name, oi.created_at, pi.id
	`, paymentID)
	if err != nil {
		return PaymentDetailResponse{}, err
	}
	defer rows.Close()

	detail.Items = []PaymentDetailItem{}
	for rows.Next() {
		var item PaymentDetailItem
		if err := rows.Scan(&item.ID, &item.OrderItemID, &item.OrderID, &item.OrderNo, &item.ProjectName, &item.ProductName, &item.CharacterName, &item.Category, &item.SeriesCode, &item.DisplayName, &item.SKU, &item.AppliedAmount, &item.PaymentStatus, &item.ImportFilename, &item.SourceSheet, &item.SourceRowKey); err != nil {
			return PaymentDetailResponse{}, err
		}
		item.AppliedAmount = round2(item.AppliedAmount)
		detail.Items = append(detail.Items, item)
	}
	if err := rows.Err(); err != nil {
		return PaymentDetailResponse{}, err
	}
	return PaymentDetailResponse{Payment: detail}, nil
}

func (s *PostgresStore) VoidPayment(ctx context.Context, request VoidPaymentRequest, adminID string) (PaymentDetailResponse, error) {
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		return PaymentDetailResponse{}, ErrVoidReasonRequired
	}
	if !isUUIDLike(request.PaymentID) {
		return PaymentDetailResponse{}, ErrPaymentNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentDetailResponse{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	var status string
	err = tx.QueryRow(ctx, `
		select user_id::text, status
		from payments
		where id = $1::uuid
		for update
	`, request.PaymentID).Scan(&userID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentDetailResponse{}, ErrPaymentNotFound
	}
	if err != nil {
		return PaymentDetailResponse{}, err
	}
	if status == "voided" {
		return PaymentDetailResponse{}, ErrPaymentAlreadyVoid
	}
	if status != "approved" {
		return PaymentDetailResponse{}, ErrPaymentNotApproved
	}

	if _, err := tx.Exec(ctx, `
		select oi.id
		from payment_items pi
		join order_items oi on oi.id = pi.order_item_id
		where pi.payment_id = $1::uuid
		for update of oi
	`, request.PaymentID); err != nil {
		return PaymentDetailResponse{}, err
	}

	if _, err := tx.Exec(ctx, `
		update payments
		set status = 'voided',
			voided_at = now(),
			voided_by_admin_id = $2::uuid,
			void_reason = $3,
			updated_at = now()
		where id = $1::uuid
		  and status = 'approved'
	`, request.PaymentID, adminID, reason); err != nil {
		return PaymentDetailResponse{}, err
	}

	if err := recalculateUserPaymentStatus(ctx, tx, userID); err != nil {
		return PaymentDetailResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PaymentDetailResponse{}, err
	}
	return s.GetPaymentDetail(ctx, request.PaymentID)
}
func (s *PostgresStore) findUser(ctx context.Context, cn string) (PaymentUser, error) {
	return findUserQuery(ctx, s.pool, cn)
}

func findUserTx(ctx context.Context, tx pgx.Tx, cn string) (PaymentUser, error) {
	return findUserQuery(ctx, tx, cn)
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findUserQuery(ctx context.Context, q queryer, cn string) (PaymentUser, error) {
	var user PaymentUser
	err := q.QueryRow(ctx, `
		select id::text, cn_code, display_name
		from users
		where lower(regexp_replace(btrim(cn_code), '\s+', ' ', 'g')) = lower($1)
		  and status = 'active'
	`, normalizeCN(cn)).Scan(&user.ID, &user.CNCode, &user.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentUser{}, ErrUserNotFound
	}
	return user, err
}

func (s *PostgresStore) listItemsForUser(ctx context.Context, userID string) ([]PaymentItemRow, PaymentSummary, error) {
	return listItemsForUserTx(ctx, s.pool, userID)
}

func (s *PostgresStore) listUnpaidItemsForUser(ctx context.Context, userID string) ([]PaymentItemRow, error) {
	items, _, err := listItemsForUserTx(ctx, s.pool, userID)
	if err != nil {
		return nil, err
	}
	return filterPayableItems(items), nil
}

func filterPayableItems(items []PaymentItemRow) []PaymentItemRow {
	payable := make([]PaymentItemRow, 0, len(items))
	for _, item := range items {
		if item.RemainingAmount > 0.005 {
			payable = append(payable, item)
		}
	}
	return payable
}

func summarizePaymentItems(items []PaymentItemRow) PaymentSummary {
	summary := PaymentSummary{}
	for _, item := range items {
		summary.TotalAmount = round2(summary.TotalAmount + item.Amount)
		summary.PaidAmount = round2(summary.PaidAmount + item.PaidAmount)
		summary.RemainingAmount = round2(summary.RemainingAmount + item.RemainingAmount)
		summary.ItemCount++
		switch item.PaymentStatus {
		case "paid":
			summary.PaidCount++
		case "partial":
			summary.PartialCount++
		default:
			summary.UnpaidCount++
		}
	}
	return summary
}

func listItemsForUserTx(ctx context.Context, q queryer, userID string) ([]PaymentItemRow, PaymentSummary, error) {
	rows, err := q.Query(ctx, `
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
			o.id::text,
			o.order_no,
			p.name,
			product.name,
			coalesce(product.character_name, ''),
			coalesce(product.category, ''),
			coalesce(product.series_code, ''),
			case when coalesce(product.category, '') = '' or product.category = '默认分类' then product.name else product.name || '-' || product.category end,
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
		join orders o on o.id = oi.order_id
		join projects p on p.id = o.project_id
		join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		left join paid_by_item paid on paid.order_item_id = oi.id
		where o.user_id = $1::uuid
		  and o.status <> 'cancelled'
		  and oi.revoked_at is null
		order by o.created_at desc, o.order_no, product.sort_order, product.name, oi.created_at, oi.id
	`, userID)
	if err != nil {
		return nil, PaymentSummary{}, err
	}
	defer rows.Close()

	items := []PaymentItemRow{}
	summary := PaymentSummary{}
	for rows.Next() {
		var item PaymentItemRow
		if err := rows.Scan(
			&item.ID,
			&item.OrderID,
			&item.OrderNo,
			&item.ProjectName,
			&item.ProductName,
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
			return nil, PaymentSummary{}, err
		}
		item.OrderItemID = item.ID
		item.Amount = round2(item.Amount)
		item.PaidAmount = round2(item.PaidAmount)
		item.RemainingAmount = round2(item.RemainingAmount)
		summary.TotalAmount = round2(summary.TotalAmount + item.Amount)
		summary.PaidAmount = round2(summary.PaidAmount + item.PaidAmount)
		summary.RemainingAmount = round2(summary.RemainingAmount + item.RemainingAmount)
		summary.ItemCount++
		switch item.PaymentStatus {
		case "paid":
			summary.PaidCount++
		case "partial":
			summary.PartialCount++
		default:
			summary.UnpaidCount++
		}
		items = append(items, item)
	}
	return items, summary, rows.Err()
}

func (s *PostgresStore) listPaymentsForUser(ctx context.Context, userID string) ([]PaymentRecord, error) {
	rows, err := s.pool.Query(ctx, `
		select
			p.id::text,
			p.submitted_amount::float8,
			p.fee_amount::float8,
			p.payable_amount::float8,
			coalesce(p.payment_method, ''),
			coalesce(p.note, ''),
			p.status,
			to_char(coalesce(p.paid_at, p.approved_at, p.submitted_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(a.username, ''),
			to_char(p.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(p.voided_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(voider.username, ''),
			coalesce(p.void_reason, '')
		from payments p
		left join admins a on a.id = coalesce(p.created_by, p.approved_by)
		left join admins voider on voider.id = p.voided_by_admin_id
		where p.user_id = $1::uuid
		order by p.created_at desc, p.id desc
		limit 50
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []PaymentRecord{}
	for rows.Next() {
		var record PaymentRecord
		if err := rows.Scan(
			&record.ID,
			&record.Amount,
			&record.FeeAmount,
			&record.PayableAmount,
			&record.PaymentMethod,
			&record.Note,
			&record.Status,
			&record.PaidAt,
			&record.CreatedBy,
			&record.CreatedAt,
			&record.VoidedAt,
			&record.VoidedBy,
			&record.VoidReason,
		); err != nil {
			return nil, err
		}
		record.Amount = round2(record.Amount)
		record.FeeAmount = round2(record.FeeAmount)
		record.PayableAmount = round2(record.PayableAmount)
		records = append(records, record)
	}
	return records, rows.Err()
}

func lockPaymentItems(ctx context.Context, tx pgx.Tx, userID string, ids []string) ([]PaymentItemRow, error) {
	placeholders := make([]string, 0, len(ids))
	args := []any{userID}
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, "$"+strconv.Itoa(len(args))+"::uuid")
	}
	rows, err := tx.Query(ctx, `
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
			oi.amount::float8,
			least(coalesce(paid.paid_amount, 0), oi.amount)::float8,
			greatest(oi.amount - coalesce(paid.paid_amount, 0), 0)::float8
		from order_items oi
		join orders o on o.id = oi.order_id
		left join paid_by_item paid on paid.order_item_id = oi.id
		where o.user_id = $1::uuid
		  and o.status <> 'cancelled'
		  and oi.revoked_at is null
		  and oi.id in (`+strings.Join(placeholders, ",")+`)
		for update of oi
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []PaymentItemRow{}
	for rows.Next() {
		var item PaymentItemRow
		if err := rows.Scan(&item.ID, &item.Amount, &item.PaidAmount, &item.RemainingAmount); err != nil {
			return nil, err
		}
		item.Amount = round2(item.Amount)
		item.PaidAmount = round2(item.PaidAmount)
		item.RemainingAmount = round2(item.RemainingAmount)
		items = append(items, item)
	}
	return items, rows.Err()
}

func recalculateUserPaymentStatus(ctx context.Context, tx pgx.Tx, userID string) error {
	if _, err := tx.Exec(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments p on p.id = pi.payment_id
			group by pi.order_item_id
		),
		item_status as (
			select
				oi.id,
				case
					when coalesce(paid.paid_amount, 0) <= 0 then 'unpaid'
					when coalesce(paid.paid_amount, 0) + 0.004 >= oi.amount then 'paid'
					else 'partial'
				end as payment_status
			from order_items oi
			join orders o on o.id = oi.order_id
			left join paid_by_item paid on paid.order_item_id = oi.id
			where o.user_id = $1::uuid
			  and oi.revoked_at is null
		)
		update order_items oi
		set payment_status = item_status.payment_status,
			updated_at = now()
		from item_status
		where item_status.id = oi.id
	`, userID); err != nil {
		return err
	}

	_, err := tx.Exec(ctx, `
		with order_status as (
			select
				o.id,
				count(oi.id) as item_count,
				count(oi.id) filter (where oi.payment_status = 'paid') as paid_count,
				count(oi.id) filter (where oi.payment_status = 'partial') as partial_count
			from orders o
			left join order_items oi on oi.order_id = o.id and oi.revoked_at is null
			where o.user_id = $1::uuid
			  and o.status <> 'cancelled'
			group by o.id
		)
		update orders o
		set status = case
				when os.item_count = 0 then o.status
				when os.paid_count = os.item_count then 'paid'
				when os.paid_count > 0 or os.partial_count > 0 then 'partially_paid'
				else 'submitted'
			end,
			updated_at = now()
		from order_status os
		where os.id = o.id
	`, userID)
	return err
}

func mergeRequestItems(items []CreatePaymentItemRequest) map[string]float64 {
	merged := map[string]float64{}
	for _, item := range items {
		id := strings.TrimSpace(item.OrderItemID)
		if id == "" {
			merged[id] += item.Amount
			continue
		}
		merged[id] = round2(merged[id] + item.Amount)
	}
	return merged
}

func parsePaymentTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC(), nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	var lastErr error
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return parsed.UTC(), nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func normalizeCN(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

// safeCentsFromFloat64 converts a round2'd float64 amount to integer cents.
// Input must already be rounded to 2 decimal places via round2().
func safeCentsFromFloat64(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// calculateFee returns (feeCents, payableCents) given baseCents and paymentMethod.
// All arithmetic is in integer cents — no float64 involved in fee calculation.
func calculateFee(baseCents int64, paymentMethod string) (int64, int64) {
	switch paymentMethod {
	case "alipay":
		return 0, baseCents
	case "wechat":
		// fee_cents = (base_cents + 999) / 1000  (ceiling division by 1000, i.e. 0.1% rounded up)
		feeCents := (baseCents + 999) / 1000
		return feeCents, baseCents + feeCents
	default:
		return 0, baseCents
	}
}

// centsToNumeric formats integer cents as a string for numeric(12,2), e.g. 3680 -> "36.80".
func centsToNumeric(cents int64) string {
	sign := ""
	abs := cents
	if cents < 0 {
		sign = "-"
		abs = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, abs/100, abs%100)
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

func writePaymentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCNRequired),
		errors.Is(err, ErrNoPaymentItems),
		errors.Is(err, ErrInvalidAmount),
		errors.Is(err, ErrOverPayment),
		errors.Is(err, ErrItemMismatch),
		errors.Is(err, ErrIdempotencyKey),
		errors.Is(err, ErrPaymentTime),
		errors.Is(err, ErrVoidReasonRequired),
		errors.Is(err, ErrPaymentNotApproved),
		errors.Is(err, ErrInvalidPaymentMethod):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrPaymentAlreadyVoid):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrUserNotFound), errors.Is(err, ErrPaymentNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		log.Printf("payment handler error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode payments JSON response: %v", err)
	}
}
