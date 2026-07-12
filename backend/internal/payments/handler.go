package payments

import (
	"context"
	"encoding/json"
	"errors"
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
	ErrCNRequired     = errors.New("cn is required")
	ErrUserNotFound   = errors.New("cn not found")
	ErrNoPaymentItems = errors.New("payment items are required")
	ErrInvalidAmount  = errors.New("payment amount must be greater than 0")
	ErrOverPayment    = errors.New("payment amount exceeds remaining amount")
	ErrItemMismatch   = errors.New("order item does not belong to this cn")
	ErrIdempotencyKey = errors.New("idempotency key is required")
	ErrPaymentTime    = errors.New("payment time is invalid")
)

type Handler struct {
	store Store
}

type Store interface {
	GetCNPayment(context.Context, string) (CNPaymentResponse, error)
	CreatePayment(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error)
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
	PaymentMethod string  `json:"payment_method,omitempty"`
	Note          string  `json:"note,omitempty"`
	Status        string  `json:"status"`
	PaidAt        string  `json:"paid_at"`
	CreatedBy     string  `json:"created_by,omitempty"`
	CreatedAt     string  `json:"created_at"`
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

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
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
		writePaymentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
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

	var paymentID string
	err = tx.QueryRow(ctx, `
		insert into payments (
			user_id,
			submitted_amount,
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
		values ($1::uuid, $2, $3, $4, 'approved', now(), now(), $5::uuid, $6, $5::uuid, $7)
		returning id::text
	`, user.ID, total, strings.TrimSpace(request.PaymentMethod), strings.TrimSpace(request.Note), adminID, paidAt, request.IdempotencyKey).Scan(&paymentID)
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

func listItemsForUserTx(ctx context.Context, q queryer, userID string) ([]PaymentItemRow, PaymentSummary, error) {
	rows, err := q.Query(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where p.status in ('submitted', 'approved')), 0) as paid_amount
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
			coalesce(p.payment_method, ''),
			coalesce(p.note, ''),
			p.status,
			to_char(coalesce(p.paid_at, p.approved_at, p.submitted_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(a.username, ''),
			to_char(p.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from payments p
		left join admins a on a.id = coalesce(p.created_by, p.approved_by)
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
			&record.PaymentMethod,
			&record.Note,
			&record.Status,
			&record.PaidAt,
			&record.CreatedBy,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		record.Amount = round2(record.Amount)
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
				coalesce(sum(pi.applied_amount) filter (where p.status in ('submitted', 'approved')), 0) as paid_amount
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
				coalesce(sum(pi.applied_amount) filter (where p.status in ('submitted', 'approved')), 0) as paid_amount
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
		errors.Is(err, ErrPaymentTime):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrUserNotFound):
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
