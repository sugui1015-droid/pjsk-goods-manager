package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubStore struct{}

func (stubStore) FindUserByCN(context.Context, string) (User, error) { return User{}, ErrNotFound }
func (stubStore) CreateSession(context.Context, string, string, time.Time) error {
	return nil
}
func (stubStore) FindUserBySession(context.Context, string) (User, error) {
	return User{}, ErrNotFound
}
func (stubStore) DeleteSession(context.Context, string) error { return nil }
func (stubStore) ListOrdersForUser(context.Context, string) (OrdersResponse, error) {
	return OrdersResponse{}, nil
}

// TestOrdersResponseNeverExposesInternalIDsOrSourceFiles is a wire-format
// guarantee, not just a frontend hiding concern: regular users must never
// receive order/order-item database ids, import batch ids, source
// filenames, or source sheet names in the JSON response, even if a future
// change accidentally re-adds one of those fields to the struct.
func TestOrdersResponseNeverExposesInternalIDsOrSourceFiles(t *testing.T) {
	response := OrdersResponse{
		User: User{ID: "user-secret-id", CNCode: "CN001"},
		Orders: []Order{
			{
				ID:          "order-secret-id",
				OrderNo:     "ORDER-001",
				Status:      "submitted",
				ProjectName: "26感谢祭单领",
				Items: []OrderItem{
					{GoodsName: "扇子", Category: "周边", CharacterName: "hrk", SeriesCode: "26感谢祭单领", DisplayName: "扇子", PaymentStatus: "unpaid"},
				},
			},
		},
		Payments: []PaymentRecord{
			{ID: "payment-secret-id", PrincipalAmount: 10, TotalAmount: 10, Status: "approved", PaidAt: "2026-07-12T00:00:00Z"},
		},
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := string(body)

	forbidden := []string{
		`"id":`,
		`"import_batch_id"`,
		`"import_filename"`,
		`"import_filenames"`,
		`"source_sheet"`,
		"order-secret-id",
		"user-secret-id",
		"payment-secret-id",
	}
	for _, term := range forbidden {
		if strings.Contains(payload, term) {
			t.Fatalf("orders response leaks internal field %q: %s", term, payload)
		}
	}
}

// TestPaymentItemsExposeOnlyUserFacingFields pins down the exact JSON shape
// of the regular-user payment-associated-items DTO: only the eight business
// fields (plus display_name) may appear, order numbers / project names /
// internal ids / import tracking / admin+audit info must not — while the
// normal order list keeps order_no and project_name for grouping.
func TestPaymentItemsExposeOnlyUserFacingFields(t *testing.T) {
	response := OrdersResponse{
		User: User{ID: "user-secret-id", CNCode: "CN001"},
		Orders: []Order{
			{ID: "order-secret-id", OrderNo: "ORDER-001", ProjectName: "26感谢祭单领", Status: "submitted", Items: []OrderItem{}},
		},
		Payments: []PaymentRecord{
			{
				ID:              "payment-secret-id",
				PrincipalAmount: 36.80,
				FeeAmount:       0.04,
				TotalAmount:     36.84,
				PaymentMethod:   "wechat",
				Status:          "approved",
				PaidAt:          "2026-07-12T15:39:00Z",
				Items: []PaymentItem{
					{DisplayName: "扇子-周边", CharacterName: "hrk", Category: "周边", Quantity: 1, UnitPrice: 36.80, Amount: 36.80, AppliedAmount: 36.80, PaymentStatus: "paid"},
				},
			},
		},
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	payment, ok := payload["payments"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("payments[0] shape: %#v", payload["payments"])
	}
	for _, forbidden := range []string{"id", "order_no", "order_number", "project_name", "import_batch_id", "import_filename", "source_sheet", "created_by", "voided_at", "voided_by", "void_reason", "note"} {
		if _, exists := payment[forbidden]; exists {
			t.Fatalf("payment record leaks %q: %#v", forbidden, payment)
		}
	}

	item, ok := payment["items"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("payment items shape: %#v", payment["items"])
	}
	allowed := map[string]bool{
		"display_name": true, "character_name": true, "category": true,
		"quantity": true, "unit_price": true, "amount": true, "applied_amount": true, "payment_status": true,
	}
	for key := range item {
		if !allowed[key] {
			t.Fatalf("payment item exposes unexpected field %q: %#v", key, item)
		}
	}
	for _, required := range []string{"display_name", "quantity", "unit_price", "amount", "applied_amount", "payment_status"} {
		if _, exists := item[required]; !exists {
			t.Fatalf("payment item is missing %q: %#v", required, item)
		}
	}

	order, ok := payload["orders"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("orders[0] shape: %#v", payload["orders"])
	}
	if order["order_no"] != "ORDER-001" || order["project_name"] != "26感谢祭单领" {
		t.Fatalf("normal order list lost its grouping fields: %#v", order)
	}
}

func TestLoginRateLimitedAfterRepeatedFailures(t *testing.T) {
	handler := NewHandler(stubStore{}, time.Hour, false)

	doLogin := func() int {
		request := httptest.NewRequest(http.MethodPost, "/api/query/login", strings.NewReader(`{"cn":"succ","query_code":"wrong"}`))
		request.RemoteAddr = "10.0.0.1:50000"
		recorder := httptest.NewRecorder()
		handler.Login(recorder, request)
		return recorder.Code
	}

	for i := 0; i < handler.limiter.maxFailures; i++ {
		if code := doLogin(); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, code)
		}
	}
	if code := doLogin(); code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt: status = %d, want 429", code)
	}
}

func TestNormalizeCN(t *testing.T) {
	tests := map[string]string{
		"  Succ  ":     "Succ",
		"a   b\tc":     "a b c",
		"墓靑(ねこ)  neko": "墓靑(ねこ) neko",
	}
	for input, want := range tests {
		if got := normalizeCN(input); got != want {
			t.Fatalf("normalizeCN(%q) = %q, want %q", input, got, want)
		}
	}
}
