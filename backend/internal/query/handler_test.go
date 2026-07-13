package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type stubStore struct{}

func (stubStore) FindUserByCN(context.Context, string) (User, error) { return User{}, ErrNotFound }
func (stubStore) CreateSession(context.Context, string, string, time.Time) error {
	return nil
}
func (stubStore) FindUserBySession(context.Context, string) (User, error) {
	return User{}, ErrNotFound
}
func (stubStore) DeleteSession(context.Context, string) error           { return nil }
func (stubStore) ChangeQueryCode(context.Context, string, string) error { return nil }
func (stubStore) BindQueryCode(context.Context, string, string, string) error {
	return ErrBindRejected
}
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

type changeCodeStore struct {
	user        User
	changeErr   error
	changedUser string
	changedHash string
}

func (s *changeCodeStore) FindUserByCN(context.Context, string) (User, error) {
	return User{}, ErrNotFound
}
func (s *changeCodeStore) CreateSession(context.Context, string, string, time.Time) error { return nil }
func (s *changeCodeStore) FindUserBySession(context.Context, string) (User, error) {
	if s.user.ID == "" {
		return User{}, ErrNotFound
	}
	return s.user, nil
}
func (s *changeCodeStore) DeleteSession(context.Context, string) error { return nil }
func (s *changeCodeStore) ChangeQueryCode(_ context.Context, userID string, queryCodeHash string) error {
	if s.changeErr != nil {
		return s.changeErr
	}
	s.changedUser = userID
	s.changedHash = queryCodeHash
	return nil
}
func (s *changeCodeStore) BindQueryCode(context.Context, string, string, string) error {
	return ErrBindRejected
}
func (s *changeCodeStore) ListOrdersForUser(context.Context, string) (OrdersResponse, error) {
	return OrdersResponse{}, nil
}

func TestChangeCodeRequiresQuerySession(t *testing.T) {
	handler := NewHandler(&changeCodeStore{}, time.Hour, false)
	request := httptest.NewRequest(http.MethodPost, "/api/query/change-code", strings.NewReader(`{"old_query_code":"OldCode-123","new_query_code":"NewCode-456","confirm_query_code":"NewCode-456"}`))
	response := httptest.NewRecorder()
	handler.ChangeCode(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestChangeCodeValidationAndSuccess(t *testing.T) {
	oldHashBytes, err := bcrypt.GenerateFromPassword([]byte("OldCode-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash old code: %v", err)
	}
	oldHash := string(oldHashBytes)
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"empty new", `{"old_query_code":"OldCode-123","new_query_code":"   ","confirm_query_code":""}`, http.StatusBadRequest},
		{"mismatch", `{"old_query_code":"OldCode-123","new_query_code":"NewCode-456","confirm_query_code":"OtherCode-456"}`, http.StatusBadRequest},
		{"invalid format", `{"old_query_code":"OldCode-123","new_query_code":"bad space","confirm_query_code":"bad space"}`, http.StatusBadRequest},
		{"wrong old", `{"old_query_code":"WrongCode-123","new_query_code":"NewCode-456","confirm_query_code":"NewCode-456"}`, http.StatusUnauthorized},
		{"same as old", `{"old_query_code":"OldCode-123","new_query_code":"OldCode-123","confirm_query_code":"OldCode-123"}`, http.StatusBadRequest},
		{"success", `{"old_query_code":"OldCode-123","new_query_code":"NewCode-456","confirm_query_code":"NewCode-456"}`, http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &changeCodeStore{user: User{ID: "user-1", CNCode: "CN001", QueryCodeHash: &oldHash, Status: "active"}}
			handler := NewHandler(store, time.Hour, false)
			request := httptest.NewRequest(http.MethodPost, "/api/query/change-code", strings.NewReader(test.body))
			request.RemoteAddr = "10.0.0.2:50000"
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
			response := httptest.NewRecorder()
			handler.ChangeCode(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			body := response.Body.String()
			for _, forbidden := range []string{"OldCode-123", "NewCode-456", "query_code_hash", "user_id", "$2a$", "$2b$"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("response leaks %q: %s", forbidden, body)
				}
			}
			if test.wantStatus == http.StatusOK {
				if store.changedUser != "user-1" || store.changedHash == "" || store.changedHash == oldHash {
					t.Fatalf("query code hash was not changed: user=%q hash=%q", store.changedUser, store.changedHash)
				}
				if err := bcrypt.CompareHashAndPassword([]byte(store.changedHash), []byte("NewCode-456")); err != nil {
					t.Fatalf("changed hash does not match new code: %v", err)
				}
				cleared := false
				for _, cookie := range response.Result().Cookies() {
					if cookie.Name == sessionCookieName && cookie.MaxAge < 0 {
						cleared = true
					}
				}
				if !cleared {
					t.Fatal("session cookie was not cleared")
				}
			}
		})
	}
}

func TestChangeCodeStoreFailureDoesNotClearSessionCookie(t *testing.T) {
	oldHashBytes, err := bcrypt.GenerateFromPassword([]byte("OldCode-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash old code: %v", err)
	}
	oldHash := string(oldHashBytes)
	store := &changeCodeStore{
		user:      User{ID: "user-1", CNCode: "CN001", QueryCodeHash: &oldHash, Status: "active"},
		changeErr: errors.New("store failure"),
	}
	handler := NewHandler(store, time.Hour, false)
	request := httptest.NewRequest(http.MethodPost, "/api/query/change-code", strings.NewReader(`{"old_query_code":"OldCode-123","new_query_code":"NewCode-456","confirm_query_code":"NewCode-456"}`))
	request.RemoteAddr = "10.0.0.4:50000"
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	response := httptest.NewRecorder()

	handler.ChangeCode(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", response.Code, response.Body.String())
	}
	if store.changedHash != "" {
		t.Fatalf("store recorded changed hash despite failure")
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.MaxAge < 0 {
			t.Fatalf("session cookie should not be cleared when transaction fails")
		}
	}
	body := response.Body.String()
	for _, forbidden := range []string{"OldCode-123", "NewCode-456", "query_code_hash", "user_id", "$2a$", "$2b$"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaks %q: %s", forbidden, body)
		}
	}
}

func TestChangeCodeRateLimitedAfterWrongOldCode(t *testing.T) {
	oldHashBytes, err := bcrypt.GenerateFromPassword([]byte("OldCode-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash old code: %v", err)
	}
	oldHash := string(oldHashBytes)
	store := &changeCodeStore{user: User{ID: "user-1", CNCode: "CN001", QueryCodeHash: &oldHash, Status: "active"}}
	handler := NewHandler(store, time.Hour, false)
	doChange := func() int {
		request := httptest.NewRequest(http.MethodPost, "/api/query/change-code", strings.NewReader(`{"old_query_code":"WrongCode-123","new_query_code":"NewCode-456","confirm_query_code":"NewCode-456"}`))
		request.RemoteAddr = "10.0.0.3:50000"
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
		response := httptest.NewRecorder()
		handler.ChangeCode(response, request)
		return response.Code
	}
	for i := 0; i < handler.limiter.maxFailures; i++ {
		if code := doChange(); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, code)
		}
	}
	if code := doChange(); code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt status = %d, want 429", code)
	}
}

func TestBindCodeValidationAndUnifiedRejection(t *testing.T) {
	handler := NewHandler(stubStore{}, time.Hour, false)

	doBind := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/query/bind-code", strings.NewReader(body))
		request.RemoteAddr = "10.0.9.1:50000"
		recorder := httptest.NewRecorder()
		handler.BindCode(recorder, request)
		return recorder
	}

	// Missing fields.
	if got := doBind(`{"cn":"","bind_token":"","new_query_code":"","confirm_query_code":""}`); got.Code != http.StatusBadRequest {
		t.Fatalf("empty fields: status = %d, want 400", got.Code)
	}
	// Mismatched confirmation.
	if got := doBind(`{"cn":"succ","bind_token":"ABCDEFG234","new_query_code":"NewCode123","confirm_query_code":"Other12345"}`); got.Code != http.StatusBadRequest {
		t.Fatalf("mismatch: status = %d, want 400", got.Code)
	}
	// Invalid format (too short).
	if got := doBind(`{"cn":"succ","bind_token":"ABCDEFG234","new_query_code":"abc","confirm_query_code":"abc"}`); got.Code != http.StatusBadRequest {
		t.Fatalf("format: status = %d, want 400", got.Code)
	}
	// Store rejection collapses to one unified Chinese message.
	got := doBind(`{"cn":"succ","bind_token":"ABCDEFG234","new_query_code":"NewCode123","confirm_query_code":"NewCode123"}`)
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("rejected: status = %d, want 401", got.Code)
	}
	if !strings.Contains(got.Body.String(), "CN 或绑定码不正确") {
		t.Fatalf("rejected body = %s, want unified message", got.Body.String())
	}
}

func TestBindCodeRateLimitedAfterRepeatedRejections(t *testing.T) {
	handler := NewHandler(stubStore{}, time.Hour, false)

	doBind := func() int {
		request := httptest.NewRequest(http.MethodPost, "/api/query/bind-code", strings.NewReader(`{"cn":"succ","bind_token":"WRONGTOKEN","new_query_code":"NewCode123","confirm_query_code":"NewCode123"}`))
		request.RemoteAddr = "10.0.9.2:50000"
		recorder := httptest.NewRecorder()
		handler.BindCode(recorder, request)
		return recorder.Code
	}

	for i := 0; i < handler.limiter.maxFailures; i++ {
		if code := doBind(); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, code)
		}
	}
	if code := doBind(); code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt: status = %d, want 429", code)
	}
}
