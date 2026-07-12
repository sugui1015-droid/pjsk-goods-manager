package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/admin"
)

type fakeStore struct {
	getCNPayment       func(context.Context, string) (CNPaymentResponse, error)
	createPayment      func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error)
	listPaymentRecords func(context.Context, PaymentFilters) (PaymentListResponse, error)
	getPaymentDetail   func(context.Context, string) (PaymentDetailResponse, error)
}

func (s fakeStore) GetCNPayment(ctx context.Context, cn string) (CNPaymentResponse, error) {
	if s.getCNPayment == nil {
		return CNPaymentResponse{}, nil
	}
	return s.getCNPayment(ctx, cn)
}

func (s fakeStore) CreatePayment(ctx context.Context, request CreatePaymentRequest, adminID string) (CreatePaymentResponse, error) {
	if s.createPayment == nil {
		return CreatePaymentResponse{}, nil
	}
	return s.createPayment(ctx, request, adminID)
}

func (s fakeStore) ListPaymentRecords(ctx context.Context, filters PaymentFilters) (PaymentListResponse, error) {
	if s.listPaymentRecords == nil {
		return PaymentListResponse{}, nil
	}
	return s.listPaymentRecords(ctx, filters)
}

func (s fakeStore) GetPaymentDetail(ctx context.Context, paymentID string) (PaymentDetailResponse, error) {
	if s.getPaymentDetail == nil {
		return PaymentDetailResponse{}, nil
	}
	return s.getPaymentDetail(ctx, paymentID)
}

func TestCNRequiresCN(t *testing.T) {
	handler := NewHandler(fakeStore{
		getCNPayment: func(_ context.Context, cn string) (CNPaymentResponse, error) {
			if cn != "" {
				t.Fatalf("cn = %q, want empty", cn)
			}
			return CNPaymentResponse{}, ErrCNRequired
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/payments/cn", nil)
	response := httptest.NewRecorder()

	handler.CN(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestCreateRequiresAdmin(t *testing.T) {
	handler := NewHandler(fakeStore{})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/payments", bytes.NewBufferString(`{}`))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestCreatePassesAdminAndReturnsDuplicate(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(_ context.Context, request CreatePaymentRequest, adminID string) (CreatePaymentResponse, error) {
			if adminID != "admin-1" {
				t.Fatalf("adminID = %q, want admin-1", adminID)
			}
			if request.CN != "CN001" || request.IdempotencyKey != "key-1" {
				t.Fatalf("unexpected request: %#v", request)
			}
			return CreatePaymentResponse{
				PaymentID: "payment-1",
				Status:    "approved",
				Duplicate: true,
			}, nil
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","payment_method":"bank","paid_at":"2026-07-12T12:30:00Z","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload CreatePaymentResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Duplicate || payload.PaymentID != "payment-1" {
		t.Fatalf("payload = %#v, want duplicate payment-1", payload)
	}
}

func TestCreateReturnsDuplicatePayment(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(_ context.Context, request CreatePaymentRequest, adminID string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{
				PaymentID: "payment-dup-1",
				Status:    "approved",
				Duplicate: true,
				Summary: PaymentSummary{
					TotalAmount:     100,
					PaidAmount:      50,
					RemainingAmount: 50,
					ItemCount:       2,
					UnpaidCount:     1,
					PartialCount:    1,
					PaidCount:       0,
				},
				Items: []PaymentItemRow{
					{ID: "item-1", OrderID: "order-1", Amount: 50, PaidAmount: 50, RemainingAmount: 0, PaymentStatus: "paid"},
					{ID: "item-2", OrderID: "order-1", Amount: 50, PaidAmount: 0, RemainingAmount: 50, PaymentStatus: "unpaid"},
				},
			}, nil
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","payment_method":"bank","paid_at":"2026-07-12T12:30:00Z","idempotency_key":"key-dup","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload CreatePaymentResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Duplicate {
		t.Fatalf("payload.Duplicate = false, want true")
	}
	if payload.PaymentID != "payment-dup-1" {
		t.Fatalf("payload.PaymentID = %q, want payment-dup-1", payload.PaymentID)
	}
	if payload.Status != "approved" {
		t.Fatalf("payload.Status = %q, want approved", payload.Status)
	}
	if payload.Summary.TotalAmount != 100 {
		t.Fatalf("payload.Summary.TotalAmount = %f, want 100", payload.Summary.TotalAmount)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("len(payload.Items) = %d, want 2", len(payload.Items))
	}
}

func TestCreateRejectsOverPayment(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrOverPayment
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","idempotency_key":"key-overpay","items":[{"order_item_id":"item-1","amount":999}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != ErrOverPayment.Error() {
		t.Fatalf("error = %q, want %q", payload.Error, ErrOverPayment.Error())
	}
}

func TestCreateMapsValidationErrors(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrOverPayment
		},
	})
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", bytes.NewBufferString(`{"cn":"CN001","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":99}]}`))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestCreateRejectsEmptyIdempotencyKey(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrIdempotencyKey
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","idempotency_key":"","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != ErrIdempotencyKey.Error() {
		t.Fatalf("error = %q, want %q", payload.Error, ErrIdempotencyKey.Error())
	}
}

func TestCreateRejectsInvalidPaidAt(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrPaymentTime
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","paid_at":"not-a-valid-time","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != ErrPaymentTime.Error() {
		t.Fatalf("error = %q, want %q", payload.Error, ErrPaymentTime.Error())
	}
}

func TestCreateRejectsUnknownCN(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrUserNotFound
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN999","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != ErrUserNotFound.Error() {
		t.Fatalf("error = %q, want %q", payload.Error, ErrUserNotFound.Error())
	}
}

func TestCreateMapsUnknownErrors(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, errors.New("database down")
		},
	})
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", bytes.NewBufferString(`{"cn":"CN001","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":1}]}`))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestListPassesFilters(t *testing.T) {
	handler := NewHandler(fakeStore{
		listPaymentRecords: func(_ context.Context, filters PaymentFilters) (PaymentListResponse, error) {
			if filters.CN != "CN001" || filters.PaymentMethod != "Alipay" || filters.Status != "approved" || filters.PaidFrom != "2026-07-12T10:00" || filters.PaidTo != "2026-07-13T10:00" {
				t.Fatalf("filters = %#v", filters)
			}
			if filters.Limit != 100 {
				t.Fatalf("limit = %d, want 100", filters.Limit)
			}
			return PaymentListResponse{Items: []PaymentListItem{{ID: "payment-1", CNCode: "CN001", Amount: 10, PaymentItemCount: 2}}}, nil
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments?cn=CN001&payment_method=Alipay&status=approved&paid_from=2026-07-12T10:00&paid_to=2026-07-13T10:00", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.List).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload PaymentListResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "payment-1" {
		t.Fatalf("payload = %#v, want payment-1", payload)
	}
}

func TestListRejectsInvalidTime(t *testing.T) {
	handler := NewHandler(fakeStore{
		listPaymentRecords: func(context.Context, PaymentFilters) (PaymentListResponse, error) {
			t.Fatal("store must not be called for invalid time")
			return PaymentListResponse{}, nil
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments?paid_from=not-a-time", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.List).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestDetailRejectsInvalidID(t *testing.T) {
	handler := NewHandler(fakeStore{
		getPaymentDetail: func(context.Context, string) (PaymentDetailResponse, error) {
			t.Fatal("store must not be called for invalid payment id")
			return PaymentDetailResponse{}, nil
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments/not-a-uuid", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Detail).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestDetailMapsNotFound(t *testing.T) {
	handler := NewHandler(fakeStore{
		getPaymentDetail: func(_ context.Context, paymentID string) (PaymentDetailResponse, error) {
			if paymentID != "00000000-0000-0000-0000-000000000000" {
				t.Fatalf("paymentID = %q, want 00000000-0000-0000-0000-000000000000", paymentID)
			}
			return PaymentDetailResponse{}, ErrPaymentNotFound
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments/00000000-0000-0000-0000-000000000000", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Detail).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestDetailReturnsFullItems(t *testing.T) {
	handler := NewHandler(fakeStore{
		getPaymentDetail: func(_ context.Context, paymentID string) (PaymentDetailResponse, error) {
			if paymentID != "11111111-1111-1111-1111-111111111111" {
				t.Fatalf("paymentID = %q, want 11111111-1111-1111-1111-111111111111", paymentID)
			}
			return PaymentDetailResponse{
				Payment: PaymentDetail{
					PaymentListItem: PaymentListItem{
						ID:               "11111111-1111-1111-1111-111111111111",
						CNCode:           "CN001",
						DisplayName:      "Test User",
						Amount:           150,
						PaymentMethod:    "bank",
						Status:           "approved",
						PaidAt:           "2026-07-12T12:30:00Z",
						CreatedBy:        "admin-1",
						Note:             "test payment",
						PaymentItemCount: 2,
						CreatedAt:        "2026-07-12T12:30:00Z",
					},
					UserID: "user-1",
					Items: []PaymentDetailItem{
						{
							ID:            "pi-1",
							OrderItemID:   "oi-1",
							OrderID:       "order-1",
							OrderNo:       "ORD-001",
							ProjectName:   "Project A",
							ProductName:   "Product X",
							CharacterName: "Character 1",
							Category:      "figure",
							SeriesCode:    "SERIES-01",
							DisplayName:   "Product X-figure",
							SKU:           "SKU-001",
							AppliedAmount: 100,
							PaymentStatus: "paid",
						},
						{
							ID:            "pi-2",
							OrderItemID:   "oi-2",
							OrderID:       "order-1",
							OrderNo:       "ORD-001",
							ProjectName:   "Project A",
							ProductName:   "Product Y",
							AppliedAmount: 50,
							PaymentStatus: "partial",
						},
					},
				},
			}, nil
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments/11111111-1111-1111-1111-111111111111", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Detail).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload PaymentDetailResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify payment summary fields
	if payload.Payment.ID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("payload.Payment.ID = %q, want 11111111-1111-1111-1111-111111111111", payload.Payment.ID)
	}
	if payload.Payment.CNCode != "CN001" {
		t.Fatalf("payload.Payment.CNCode = %q, want CN001", payload.Payment.CNCode)
	}
	if payload.Payment.Amount != 150 {
		t.Fatalf("payload.Payment.Amount = %f, want 150", payload.Payment.Amount)
	}
	if payload.Payment.Status != "approved" {
		t.Fatalf("payload.Payment.Status = %q, want approved", payload.Payment.Status)
	}
	if payload.Payment.PaymentItemCount != 2 {
		t.Fatalf("payload.Payment.PaymentItemCount = %d, want 2", payload.Payment.PaymentItemCount)
	}

	// Verify items count
	if len(payload.Payment.Items) != 2 {
		t.Fatalf("len(payload.Payment.Items) = %d, want 2", len(payload.Payment.Items))
	}

	// Verify key fields on first item
	item := payload.Payment.Items[0]
	if item.OrderItemID != "oi-1" {
		t.Fatalf("item.OrderItemID = %q, want oi-1", item.OrderItemID)
	}
	if item.AppliedAmount != 100 {
		t.Fatalf("item.AppliedAmount = %f, want 100", item.AppliedAmount)
	}
	if item.OrderNo != "ORD-001" {
		t.Fatalf("item.OrderNo = %q, want ORD-001", item.OrderNo)
	}
	if item.ProductName != "Product X" {
		t.Fatalf("item.ProductName = %q, want Product X", item.ProductName)
	}
	if item.PaymentStatus != "paid" {
		t.Fatalf("item.PaymentStatus = %q, want paid", item.PaymentStatus)
	}

	// Verify key fields on second item
	item2 := payload.Payment.Items[1]
	if item2.OrderItemID != "oi-2" {
		t.Fatalf("item2.OrderItemID = %q, want oi-2", item2.OrderItemID)
	}
	if item2.AppliedAmount != 50 {
		t.Fatalf("item2.AppliedAmount = %f, want 50", item2.AppliedAmount)
	}
	if item2.PaymentStatus != "partial" {
		t.Fatalf("item2.PaymentStatus = %q, want partial", item2.PaymentStatus)
	}
}

func authenticatedHandler(next http.HandlerFunc) http.Handler {
	adminHandler := admin.NewHandler(authStore{}, time.Hour, false)
	return adminHandler.RequireAuthentication(http.HandlerFunc(next))
}

func authenticatedRequest(method string, path string, body *bytes.Buffer) *http.Request {
	request := httptest.NewRequest(method, path, body)
	request.AddCookie(&http.Cookie{Name: "pjsk_admin_session", Value: "session-token"})
	return request
}

type authStore struct{}

func (authStore) FindByUsername(context.Context, string) (admin.Admin, error) {
	return admin.Admin{}, admin.ErrNotFound
}

func (authStore) CreateSession(context.Context, string, string, time.Time) error {
	return nil
}

func (authStore) FindBySession(context.Context, string) (admin.Admin, error) {
	return admin.Admin{ID: "admin-1", Username: "admin", Status: "active"}, nil
}

func (authStore) DeleteSession(context.Context, string) error {
	return nil
}
