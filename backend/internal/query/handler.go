package query

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "pjsk_query_session"

var (
	ErrNotFound           = errors.New("query user or session not found")
	dummyQueryCodeHash, _ = bcrypt.GenerateFromPassword([]byte("invalid-query-code-placeholder"), bcrypt.DefaultCost)
)

type Handler struct {
	store        Store
	sessionTTL   time.Duration
	cookieSecure bool
	now          func() time.Time
	random       io.Reader
	limiter      *loginLimiter
}

type Store interface {
	FindUserByCN(context.Context, string) (User, error)
	CreateSession(context.Context, string, string, time.Time) error
	FindUserBySession(context.Context, string) (User, error)
	DeleteSession(context.Context, string) error
	ListOrdersForUser(context.Context, string) (OrdersResponse, error)
}

type User struct {
	ID            string  `json:"id"`
	CNCode        string  `json:"cn_code"`
	DisplayName   *string `json:"display_name,omitempty"`
	QueryCodeHash *string `json:"-"`
	Status        string  `json:"-"`
}

type loginRequest struct {
	CN        string `json:"cn"`
	QueryCode string `json:"query_code"`
}

type loginResponse struct {
	User User `json:"user"`
}

type OrdersResponse struct {
	User            User            `json:"user"`
	Orders          []Order         `json:"orders"`
	Payments        []PaymentRecord `json:"payments"`
	TotalQuantity   float64         `json:"total_quantity"`
	TotalAmount     float64         `json:"total_amount"`
	PaidAmount      float64         `json:"paid_amount"`
	RemainingAmount float64         `json:"remaining_amount"`
}

// PaymentRecord is the user-facing view of a payment. It intentionally
// excludes admin usernames, notes, and void reasons.
type PaymentRecord struct {
	ID              string  `json:"id"`
	PrincipalAmount float64 `json:"principal_amount"`
	FeeAmount       float64 `json:"fee_amount"`
	TotalAmount     float64 `json:"total_amount"`
	PaymentMethod   string  `json:"payment_method,omitempty"`
	Status          string  `json:"status"`
	PaidAt          string  `json:"paid_at"`
	VoidedAt        string  `json:"voided_at,omitempty"`
}

type Order struct {
	ID              string      `json:"id"`
	OrderNo         string      `json:"order_no"`
	Status          string      `json:"status"`
	ProjectName     string      `json:"project_name"`
	TotalQuantity   float64     `json:"total_quantity"`
	TotalAmount     float64     `json:"total_amount"`
	PaidAmount      float64     `json:"paid_amount"`
	RemainingAmount float64     `json:"remaining_amount"`
	CreatedAt       string      `json:"created_at"`
	ImportFilenames []string    `json:"import_filenames"`
	Items           []OrderItem `json:"items"`
}

type OrderItem struct {
	ID              string  `json:"id"`
	GoodsName       string  `json:"goods_name"`
	Category        string  `json:"category,omitempty"`
	CharacterName   string  `json:"character_name,omitempty"`
	SeriesCode      string  `json:"series_code,omitempty"`
	DisplayName     string  `json:"display_name"`
	Quantity        float64 `json:"quantity"`
	UnitPrice       float64 `json:"unit_price"`
	Amount          float64 `json:"amount"`
	PaidAmount      float64 `json:"paid_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
	PaymentStatus   string  `json:"payment_status"`
	ImportBatchID   string  `json:"import_batch_id,omitempty"`
	ImportFilename  string  `json:"import_filename,omitempty"`
	SourceSheet     string  `json:"source_sheet,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewHandler(store Store, sessionTTL time.Duration, cookieSecure bool) *Handler {
	return &Handler{
		store:        store,
		sessionTTL:   sessionTTL,
		cookieSecure: cookieSecure,
		now:          time.Now,
		random:       rand.Reader,
		limiter:      newLoginLimiter(),
	}
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	var request loginRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	cn := normalizeCN(request.CN)
	if cn == "" || strings.TrimSpace(request.QueryCode) == "" {
		writeError(w, http.StatusBadRequest, "请输入 CN 和查询码")
		return
	}

	ip := clientIP(r)
	if !h.limiter.allow(ip, cn, h.now()) {
		writeError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	user, err := h.store.FindUserByCN(ctx, cn)
	if err != nil && !errors.Is(err, ErrNotFound) {
		log.Printf("find query user: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	passwordHash := dummyQueryCodeHash
	if err == nil && user.QueryCodeHash != nil && *user.QueryCodeHash != "" {
		passwordHash = []byte(*user.QueryCodeHash)
	} else if err == nil {
		h.limiter.recordFailure(ip, cn, h.now())
		writeError(w, http.StatusUnauthorized, "该 CN 尚未设置查询码，请联系管理员")
		return
	}

	matches := bcrypt.CompareHashAndPassword(passwordHash, []byte(request.QueryCode)) == nil
	if errors.Is(err, ErrNotFound) || user.Status != "active" || !matches {
		h.limiter.recordFailure(ip, cn, h.now())
		writeError(w, http.StatusUnauthorized, "CN 或查询码不正确")
		return
	}

	token, tokenHash, err := newSessionToken(h.random)
	if err != nil {
		log.Printf("generate query session token: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	expiresAt := h.now().Add(h.sessionTTL)
	if err := h.store.CreateSession(ctx, user.ID, tokenHash, expiresAt); err != nil {
		log.Printf("create query session: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	h.limiter.recordSuccess(ip, cn)
	h.setSessionCookie(w, token, expiresAt)
	user.QueryCodeHash = nil
	writeJSON(w, http.StatusOK, loginResponse{User: user})
}

func (h *Handler) Orders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	user, ok := h.userFromRequest(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.ListOrdersForUser(ctx, user.ID)
	if err != nil {
		log.Printf("list query orders: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	response.User = user
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		h.clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.store.DeleteSession(ctx, hashToken(cookie.Value)); err != nil {
		log.Printf("delete query session: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) userFromRequest(w http.ResponseWriter, r *http.Request) (User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "请先输入 CN 和查询码")
		return User{}, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	user, err := h.store.FindUserBySession(ctx, hashToken(cookie.Value))
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			log.Printf("find query session: %v", err)
		}
		h.clearSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "查询登录已过期，请重新输入查询码")
		return User{}, false
	}
	user.QueryCodeHash = nil
	return user, true
}

func (s *PostgresStore) FindUserByCN(ctx context.Context, cn string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		select id::text, cn_code, display_name, query_code_hash, status
		from users
		where lower(regexp_replace(btrim(cn_code), '\s+', ' ', 'g')) = lower($1)
	`, normalizeCN(cn)).Scan(&user.ID, &user.CNCode, &user.DisplayName, &user.QueryCodeHash, &user.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}

func (s *PostgresStore) CreateSession(ctx context.Context, userID string, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		insert into query_sessions (user_id, token_hash, expires_at)
		values ($1::uuid, $2, $3)
	`, userID, tokenHash, expiresAt)
	return err
}

func (s *PostgresStore) FindUserBySession(ctx context.Context, tokenHash string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		with valid_session as (
			update query_sessions
			set last_used_at = now()
			where token_hash = $1 and expires_at > now()
			returning user_id
		)
		select u.id::text, u.cn_code, u.display_name, u.query_code_hash, u.status
		from valid_session s
		join users u on u.id = s.user_id
		where u.status = 'active'
	`, tokenHash).Scan(&user.ID, &user.CNCode, &user.DisplayName, &user.QueryCodeHash, &user.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}

func (s *PostgresStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, "delete from query_sessions where token_hash = $1", tokenHash)
	return err
}

func (s *PostgresStore) ListOrdersForUser(ctx context.Context, userID string) (OrdersResponse, error) {
	rows, err := s.pool.Query(ctx, `
		select
			o.id::text,
			o.order_no,
			o.status,
			p.name,
			coalesce(sum(oi.quantity), 0)::float8,
			coalesce(sum(oi.amount), 0)::float8,
			to_char(o.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(array_agg(distinct ib.original_filename) filter (where ib.original_filename is not null), array[]::text[])
		from orders o
		join projects p on p.id = o.project_id
		left join order_items oi on oi.order_id = o.id and oi.revoked_at is null
		left join import_batches ib on ib.id = oi.import_batch_id
		where o.user_id = $1::uuid
		  and o.status <> 'cancelled'
		group by o.id, o.order_no, o.status, p.name, o.created_at
		having count(oi.id) > 0
		order by o.created_at desc, o.id desc
	`, userID)
	if err != nil {
		return OrdersResponse{}, err
	}
	defer rows.Close()

	response := OrdersResponse{Orders: []Order{}, Payments: []PaymentRecord{}}
	for rows.Next() {
		var order Order
		if err := rows.Scan(
			&order.ID,
			&order.OrderNo,
			&order.Status,
			&order.ProjectName,
			&order.TotalQuantity,
			&order.TotalAmount,
			&order.CreatedAt,
			&order.ImportFilenames,
		); err != nil {
			return OrdersResponse{}, err
		}
		items, err := s.listOrderItems(ctx, order.ID)
		if err != nil {
			return OrdersResponse{}, err
		}
		order.Items = items
		for _, item := range items {
			order.PaidAmount = round2(order.PaidAmount + item.PaidAmount)
			order.RemainingAmount = round2(order.RemainingAmount + item.RemainingAmount)
		}
		response.TotalQuantity += order.TotalQuantity
		response.TotalAmount = round2(response.TotalAmount + order.TotalAmount)
		response.PaidAmount = round2(response.PaidAmount + order.PaidAmount)
		response.RemainingAmount = round2(response.RemainingAmount + order.RemainingAmount)
		response.Orders = append(response.Orders, order)
	}
	if err := rows.Err(); err != nil {
		return OrdersResponse{}, err
	}

	payments, err := s.listPaymentsForUser(ctx, userID)
	if err != nil {
		return OrdersResponse{}, err
	}
	response.Payments = payments
	return response, nil
}

func (s *PostgresStore) listOrderItems(ctx context.Context, orderID string) ([]OrderItem, error) {
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
			coalesce(product.category, ''),
			coalesce(product.character_name, ''),
			coalesce(product.series_code, ''),
			case when coalesce(product.category, '') = '' or product.category = '默认分类' then product.name else product.name || '-' || product.category end,
			oi.quantity::float8,
			oi.unit_price::float8,
			oi.amount::float8,
			least(coalesce(paid.paid_amount, 0), oi.amount)::float8,
			greatest(oi.amount - coalesce(paid.paid_amount, 0), 0)::float8,
			oi.payment_status,
			coalesce(oi.import_batch_id::text, ''),
			coalesce(ib.original_filename, ''),
			coalesce(oi.source_sheet, '')
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

	items := []OrderItem{}
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(
			&item.ID,
			&item.GoodsName,
			&item.Category,
			&item.CharacterName,
			&item.SeriesCode,
			&item.DisplayName,
			&item.Quantity,
			&item.UnitPrice,
			&item.Amount,
			&item.PaidAmount,
			&item.RemainingAmount,
			&item.PaymentStatus,
			&item.ImportBatchID,
			&item.ImportFilename,
			&item.SourceSheet,
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

func (s *PostgresStore) listPaymentsForUser(ctx context.Context, userID string) ([]PaymentRecord, error) {
	rows, err := s.pool.Query(ctx, `
		select
			p.id::text,
			p.submitted_amount::float8,
			p.fee_amount::float8,
			p.payable_amount::float8,
			coalesce(p.payment_method, ''),
			p.status,
			to_char(coalesce(p.paid_at, p.approved_at, p.submitted_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(p.voided_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')
		from payments p
		where p.user_id = $1::uuid
		order by coalesce(p.paid_at, p.approved_at, p.submitted_at) desc, p.created_at desc, p.id desc
		limit 100
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
			&record.PrincipalAmount,
			&record.FeeAmount,
			&record.TotalAmount,
			&record.PaymentMethod,
			&record.Status,
			&record.PaidAt,
			&record.VoidedAt,
		); err != nil {
			return nil, err
		}
		record.PrincipalAmount = round2(record.PrincipalAmount)
		record.FeeAmount = round2(record.FeeAmount)
		record.TotalAmount = round2(record.TotalAmount)
		records = append(records, record)
	}
	return records, rows.Err()
}

func normalizeCN(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func newSessionToken(random io.Reader) (string, string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(value)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(h.sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
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
		log.Printf("encode query JSON response: %v", err)
	}
}

func round2(value float64) float64 {
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(value, 'f', 2, 64), 64)
	return rounded
}
