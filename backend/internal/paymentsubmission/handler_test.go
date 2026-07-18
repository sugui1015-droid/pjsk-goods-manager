package paymentsubmission

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/payments"
	"pjsk/backend/internal/query"
)

// fakeStore lets handler tests drive each store method independently.
type fakeStore struct {
	create      func(context.Context, CreateInput) (UserSubmission, error)
	listForUser func(context.Context, string) ([]UserSubmission, error)
	userImage   func(context.Context, string, string) (Image, error)
	adminList   func(context.Context, Filters) (AdminListResponse, error)
	facets      func(context.Context, FacetRequest) (FacetResponse, error)
	adminDetail func(context.Context, string) (AdminDetail, error)
	adminImage  func(context.Context, string) (Image, error)
	reject      func(context.Context, string, string, string) (AdminDetail, error)
	approve     func(context.Context, string, string, ApproveInput) (AdminDetail, error)
}

func (s fakeStore) Create(ctx context.Context, in CreateInput) (UserSubmission, error) {
	if s.create == nil {
		return UserSubmission{ID: "sub-1", Status: StatusSubmitted}, nil
	}
	return s.create(ctx, in)
}

func (s fakeStore) ListForUser(ctx context.Context, userID string) ([]UserSubmission, error) {
	if s.listForUser == nil {
		return []UserSubmission{}, nil
	}
	return s.listForUser(ctx, userID)
}

func (s fakeStore) UserImage(ctx context.Context, userID, id string) (Image, error) {
	if s.userImage == nil {
		return Image{}, ErrNotFound
	}
	return s.userImage(ctx, userID, id)
}

func (s fakeStore) AdminList(ctx context.Context, filters Filters) (AdminListResponse, error) {
	if s.adminList == nil {
		return AdminListResponse{Items: []AdminListItem{}}, nil
	}
	return s.adminList(ctx, filters)
}

func (s fakeStore) Facets(ctx context.Context, request FacetRequest) (FacetResponse, error) {
	if s.facets == nil {
		return FacetResponse{Column: request.Column, Values: []FacetValue{}}, nil
	}
	return s.facets(ctx, request)
}

func (s fakeStore) AdminDetail(ctx context.Context, id string) (AdminDetail, error) {
	if s.adminDetail == nil {
		return AdminDetail{}, ErrNotFound
	}
	return s.adminDetail(ctx, id)
}

func (s fakeStore) AdminImage(ctx context.Context, id string) (Image, error) {
	if s.adminImage == nil {
		return Image{}, ErrNotFound
	}
	return s.adminImage(ctx, id)
}

func (s fakeStore) Reject(ctx context.Context, id, adminID, reason string) (AdminDetail, error) {
	if s.reject == nil {
		return AdminDetail{}, nil
	}
	return s.reject(ctx, id, adminID, reason)
}

func (s fakeStore) Approve(ctx context.Context, id, adminID string, in ApproveInput) (AdminDetail, error) {
	if s.approve == nil {
		return AdminDetail{}, nil
	}
	return s.approve(ctx, id, adminID, in)
}

// validPNG returns bytes that pass the shared image validator.
func validPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// multipartBody builds a multipart form with the given fields plus, optionally,
// a "file" part carrying fileBytes with fileName.
func multipartBody(t *testing.T, fields map[string]string, fileName string, fileBytes []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if fileName != "" {
		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(fileBytes); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func withSessionUser(r *http.Request, user query.SessionUser) *http.Request {
	return r.WithContext(query.ContextWithSessionUser(r.Context(), user))
}

func withAdmin(r *http.Request) *http.Request {
	return r.WithContext(admin.ContextWithAdmin(r.Context(), admin.Admin{ID: "admin-1", Username: "qa_admin", Status: "active"}))
}

func decodeError(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return payload.Error
}

// ---- user create -------------------------------------------------------------

func TestUserCreateRequiresSession(t *testing.T) {
	handler := NewHandler(fakeStore{})
	body, contentType := multipartBody(t, map[string]string{"payment_method": "alipay"}, "proof.png", validPNG(t))
	request := httptest.NewRequest(http.MethodPost, "/api/query/payment-submissions", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	handler.UserCollection(response, request) // no session in context

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestUserCreateSucceedsAndTakesIdentityFromSessionNotBody(t *testing.T) {
	var captured CreateInput
	handler := NewHandler(fakeStore{
		create: func(_ context.Context, in CreateInput) (UserSubmission, error) {
			captured = in
			return UserSubmission{ID: "sub-1", PaymentMethod: in.PaymentMethod, Status: StatusSubmitted, SubmittedAt: "2026-07-18T00:00:00Z"}, nil
		},
	})
	// A hostile client also sends cn/user_id/principal_amount fields; they must be
	// ignored — identity and amounts come from the session and the server.
	body, contentType := multipartBody(t, map[string]string{
		"payment_method":   "wechat",
		"cn":               "SOMEONE_ELSE",
		"user_id":          "attacker",
		"principal_amount": "999999",
	}, "我的付款.png", validPNG(t))
	request := httptest.NewRequest(http.MethodPost, "/api/query/payment-submissions", body)
	request.Header.Set("Content-Type", contentType)
	request = withSessionUser(request, query.SessionUser{UserID: "user-1", CNCode: "测试CN01"})
	response := httptest.NewRecorder()

	handler.UserCollection(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body.String())
	}
	if captured.UserID != "user-1" || captured.CNCode != "测试CN01" {
		t.Fatalf("identity = %q/%q, want user-1/测试CN01 (must come from session, not body)", captured.UserID, captured.CNCode)
	}
	if captured.PaymentMethod != "wechat" {
		t.Fatalf("method = %q, want wechat", captured.PaymentMethod)
	}
	if captured.MimeType != "image/png" || captured.ByteSize == 0 || len(captured.SHA256) != 64 {
		t.Fatalf("validated image not passed through: %#v", captured)
	}
	if strings.ContainsAny(captured.OriginalFilename, `/\`) {
		t.Fatalf("filename %q must not contain a path separator", captured.OriginalFilename)
	}
}

func TestUserCreateRejectsMissingImage(t *testing.T) {
	handler := NewHandler(fakeStore{
		create: func(context.Context, CreateInput) (UserSubmission, error) {
			t.Fatal("store.Create must not run when no image was provided")
			return UserSubmission{}, nil
		},
	})
	body, contentType := multipartBody(t, map[string]string{"payment_method": "alipay"}, "", nil)
	request := httptest.NewRequest(http.MethodPost, "/api/query/payment-submissions", body)
	request.Header.Set("Content-Type", contentType)
	request = withSessionUser(request, query.SessionUser{UserID: "user-1", CNCode: "CN"})
	response := httptest.NewRecorder()

	handler.UserCollection(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestUserCreateRejectsInvalidMethod(t *testing.T) {
	handler := NewHandler(fakeStore{
		create: func(context.Context, CreateInput) (UserSubmission, error) {
			t.Fatal("store.Create must not run for an invalid method")
			return UserSubmission{}, nil
		},
	})
	body, contentType := multipartBody(t, map[string]string{"payment_method": "bank"}, "proof.png", validPNG(t))
	request := httptest.NewRequest(http.MethodPost, "/api/query/payment-submissions", body)
	request.Header.Set("Content-Type", contentType)
	request = withSessionUser(request, query.SessionUser{UserID: "user-1", CNCode: "CN"})
	response := httptest.NewRecorder()

	handler.UserCollection(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestUserCreateRejectsDisguisedFile(t *testing.T) {
	handler := NewHandler(fakeStore{
		create: func(context.Context, CreateInput) (UserSubmission, error) {
			t.Fatal("store.Create must not run for a non-image file")
			return UserSubmission{}, nil
		},
	})
	// An HTML/text payload named .png must be rejected by content validation.
	body, contentType := multipartBody(t, map[string]string{"payment_method": "alipay"}, "proof.png", []byte("<html><body>not an image</body></html>"))
	request := httptest.NewRequest(http.MethodPost, "/api/query/payment-submissions", body)
	request.Header.Set("Content-Type", contentType)
	request = withSessionUser(request, query.SessionUser{UserID: "user-1", CNCode: "CN"})
	response := httptest.NewRecorder()

	handler.UserCollection(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

// ---- user list & image -------------------------------------------------------

func TestUserListReturnsOwnSubmissions(t *testing.T) {
	handler := NewHandler(fakeStore{
		listForUser: func(_ context.Context, userID string) ([]UserSubmission, error) {
			if userID != "user-1" {
				t.Fatalf("userID = %q, want user-1", userID)
			}
			return []UserSubmission{{ID: "sub-1", Status: StatusRejected, RejectReason: "图片模糊"}}, nil
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/query/payment-submissions", nil)
	request = withSessionUser(request, query.SessionUser{UserID: "user-1", CNCode: "CN"})
	response := httptest.NewRecorder()

	handler.UserCollection(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), "图片模糊") {
		t.Fatalf("body missing reject reason: %s", response.Body.String())
	}
}

func TestUserImageRejectsForeignSubmission(t *testing.T) {
	handler := NewHandler(fakeStore{
		userImage: func(_ context.Context, userID, id string) (Image, error) {
			// The store scopes by user_id, so a foreign id returns ErrNotFound.
			return Image{}, ErrNotFound
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/query/payment-submissions/11111111-1111-1111-1111-111111111111/image", nil)
	request = withSessionUser(request, query.SessionUser{UserID: "user-1", CNCode: "CN"})
	response := httptest.NewRecorder()

	handler.UserImage(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestUserImageSetsNosniffHeaders(t *testing.T) {
	handler := NewHandler(fakeStore{
		userImage: func(context.Context, string, string) (Image, error) {
			return Image{Data: validPNG(t), MimeType: "image/png", SHA256: strings.Repeat("a", 64)}, nil
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/query/payment-submissions/11111111-1111-1111-1111-111111111111/image", nil)
	request = withSessionUser(request, query.SessionUser{UserID: "user-1", CNCode: "CN"})
	response := httptest.NewRecorder()

	handler.UserImage(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q, want nosniff", got)
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if !strings.HasPrefix(response.Header().Get("Cache-Control"), "private") {
		t.Fatalf("cache-control = %q, want private", response.Header().Get("Cache-Control"))
	}
}

// ---- admin list & facets -----------------------------------------------------

func TestAdminListPassesFilters(t *testing.T) {
	handler := NewHandler(fakeStore{
		adminList: func(_ context.Context, filters Filters) (AdminListResponse, error) {
			if len(filters.Status) != 1 || filters.Status[0] != StatusSubmitted {
				t.Fatalf("status = %#v", filters.Status)
			}
			if len(filters.PaymentMethod) != 1 || filters.PaymentMethod[0] != MethodWechat {
				t.Fatalf("method = %#v (aliases must normalise)", filters.PaymentMethod)
			}
			return AdminListResponse{Items: []AdminListItem{{ID: "sub-1", CNCode: "CN"}}, Total: 1, Page: 1, PageSize: DefaultPageSize, TotalPages: 1}, nil
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/payment-submissions?status=submitted&payment_method=微信", nil)
	request = withAdmin(request)
	response := httptest.NewRecorder()

	handler.AdminCollection(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
}

func TestAdminListRejectsInvalidStatus(t *testing.T) {
	handler := NewHandler(fakeStore{
		adminList: func(context.Context, Filters) (AdminListResponse, error) {
			t.Fatal("store must not be called for an invalid status")
			return AdminListResponse{}, nil
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/payment-submissions?status=voided", nil)
	request = withAdmin(request)
	response := httptest.NewRecorder()

	handler.AdminCollection(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestFacetsRejectsUnknownColumn(t *testing.T) {
	handler := NewHandler(fakeStore{})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/payment-submissions/facets?column=sha256", nil)
	request = withAdmin(request)
	response := httptest.NewRecorder()

	handler.Facets(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

// ---- admin reject ------------------------------------------------------------

func TestAdminRejectRequiresAdmin(t *testing.T) {
	handler := NewHandler(fakeStore{})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/reject", bytes.NewBufferString(`{"reason":"blurry"}`))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request) // no admin in context

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestAdminRejectRequiresReason(t *testing.T) {
	handler := NewHandler(fakeStore{
		reject: func(context.Context, string, string, string) (AdminDetail, error) {
			t.Fatal("store.Reject must not run without a reason")
			return AdminDetail{}, nil
		},
	})
	request := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/reject", bytes.NewBufferString(`{"reason":"   "}`)))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if got := decodeError(t, response); got != ErrRejectReasonRequired.Error() {
		t.Fatalf("error = %q, want %q", got, ErrRejectReasonRequired.Error())
	}
}

func TestAdminRejectPassesReasonAndMapsNotPending(t *testing.T) {
	handler := NewHandler(fakeStore{
		reject: func(_ context.Context, id, adminID, reason string) (AdminDetail, error) {
			if id != "11111111-1111-1111-1111-111111111111" || adminID != "admin-1" || reason != "图片不清晰" {
				t.Fatalf("reject args = %q/%q/%q", id, adminID, reason)
			}
			return AdminDetail{}, ErrNotPending
		},
	})
	request := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/reject", bytes.NewBufferString(`{"reason":"  图片不清晰  "}`)))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
}

// ---- admin approve -----------------------------------------------------------

func TestAdminApproveRequiresAdmin(t *testing.T) {
	handler := NewHandler(fakeStore{})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/approve", bytes.NewBufferString(`{"items":[{"order_item_id":"oi-1","amount":10}]}`))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestAdminApprovePassesAllocationAndMapsOverPayment(t *testing.T) {
	handler := NewHandler(fakeStore{
		approve: func(_ context.Context, id, adminID string, in ApproveInput) (AdminDetail, error) {
			if id != "11111111-1111-1111-1111-111111111111" || adminID != "admin-1" {
				t.Fatalf("approve args = %q/%q", id, adminID)
			}
			if len(in.Items) != 1 || in.Items[0].OrderItemID != "oi-1" || in.Items[0].Amount != 120 {
				t.Fatalf("items = %#v", in.Items)
			}
			return AdminDetail{}, payments.ErrOverPayment
		},
	})
	request := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/approve", bytes.NewBufferString(`{"items":[{"order_item_id":"oi-1","amount":120}]}`)))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (over-payment)", response.Code)
	}
}

func TestAdminApproveMapsNoItems(t *testing.T) {
	handler := NewHandler(fakeStore{
		approve: func(context.Context, string, string, ApproveInput) (AdminDetail, error) {
			return AdminDetail{}, ErrNoItems
		},
	})
	request := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/approve", bytes.NewBufferString(`{"items":[]}`)))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestAdminApproveSuccessReturnsLinkedPayment(t *testing.T) {
	handler := NewHandler(fakeStore{
		approve: func(context.Context, string, string, ApproveInput) (AdminDetail, error) {
			return AdminDetail{
				AdminListItem:   AdminListItem{ID: "sub-1", Status: StatusApproved},
				LinkedPaymentID: "pay-1",
			}, nil
		},
	})
	request := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/approve", bytes.NewBufferString(`{"items":[{"order_item_id":"oi-1","amount":10}]}`)))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Submission AdminDetail `json:"submission"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Submission.Status != StatusApproved || payload.Submission.LinkedPaymentID != "pay-1" {
		t.Fatalf("submission = %#v", payload.Submission)
	}
}

func TestAdminImageSetsNosniff(t *testing.T) {
	handler := NewHandler(fakeStore{
		adminImage: func(context.Context, string) (Image, error) {
			return Image{Data: validPNG(t), MimeType: "image/png", SHA256: strings.Repeat("b", 64)}, nil
		},
	})
	request := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/image", nil))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q, want nosniff", got)
	}
}

func TestAdminDetailMapsNotFound(t *testing.T) {
	handler := NewHandler(fakeStore{
		adminDetail: func(context.Context, string) (AdminDetail, error) {
			return AdminDetail{}, ErrNotFound
		},
	})
	request := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111", nil))
	response := httptest.NewRecorder()

	handler.AdminItem(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestSafeDisplayNameStripsPaths(t *testing.T) {
	cases := map[string]string{
		`../../etc/passwd`:     "passwd",
		`C:\Users\x\proof.png`: "proof.png",
		`/tmp/a/b/收据.jpg`:      "收据.jpg",
		`...`:                  "收肾记录.png",
		``:                     "收肾记录.png",
		`normal (1).png`:       "normal (1).png",
	}
	for input, want := range cases {
		got := safeDisplayName(input, "image/png")
		if strings.ContainsAny(got, `/\`) {
			t.Fatalf("safeDisplayName(%q) = %q still contains a separator", input, got)
		}
		if got != want {
			t.Fatalf("safeDisplayName(%q) = %q, want %q", input, got, want)
		}
	}
}
