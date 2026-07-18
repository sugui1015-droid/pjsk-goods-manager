package paymentqr

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pjsk/backend/internal/admin"
)

type fakeStore struct {
	statuses     []AdminStatus
	availability []MethodAvailability
	image        Image
	imageErr     error

	uploadCalls  []uploadCall
	uploadErr    error
	disableCalls []disableCall
	disableErr   error
}

type uploadCall struct {
	method  string
	img     StoredImage
	adminID string
}

type disableCall struct {
	method  string
	adminID string
}

func (s *fakeStore) AdminStatuses(context.Context) ([]AdminStatus, error) {
	return s.statuses, nil
}
func (s *fakeStore) UserAvailability(context.Context) ([]MethodAvailability, error) {
	return s.availability, nil
}
func (s *fakeStore) ActiveImage(_ context.Context, _ string) (Image, error) {
	return s.image, s.imageErr
}
func (s *fakeStore) Upload(_ context.Context, method string, img StoredImage, adminID string) error {
	s.uploadCalls = append(s.uploadCalls, uploadCall{method: method, img: img, adminID: adminID})
	return s.uploadErr
}
func (s *fakeStore) Disable(_ context.Context, method string, adminID string) error {
	s.disableCalls = append(s.disableCalls, disableCall{method: method, adminID: adminID})
	return s.disableErr
}

func adminContext() context.Context {
	return admin.ContextWithAdmin(context.Background(), admin.Admin{ID: "11111111-1111-1111-1111-111111111111", Username: "root"})
}

func multipartUpload(t *testing.T, fieldName, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func TestAdminUploadRejectsUnauthenticated(t *testing.T) {
	store := &fakeStore{}
	handler := NewHandler(store)
	body, ct := multipartUpload(t, "file", "qr.png", makePNG(t, 32, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/alipay", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req) // no admin in context
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(store.uploadCalls) != 0 {
		t.Fatalf("upload should not be called on unauthenticated request")
	}
}

func TestAdminUploadInvalidMethod(t *testing.T) {
	handler := NewHandler(&fakeStore{})
	body, ct := multipartUpload(t, "file", "qr.png", makePNG(t, 32, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/bank", body).WithContext(adminContext())
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAdminUploadEmptyFileRejected(t *testing.T) {
	store := &fakeStore{}
	handler := NewHandler(store)
	body, ct := multipartUpload(t, "file", "qr.png", []byte{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/alipay", body).WithContext(adminContext())
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(store.uploadCalls) != 0 {
		t.Fatalf("upload should not run for empty file")
	}
}

func TestAdminUploadDisguisedFileRejected(t *testing.T) {
	store := &fakeStore{}
	handler := NewHandler(store)
	// .png filename but HTML content.
	body, ct := multipartUpload(t, "file", "qr.png", []byte("<html><body>nope</body></html>"))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/wechat", body).WithContext(adminContext())
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(store.uploadCalls) != 0 {
		t.Fatalf("upload should not run for disguised file")
	}
}

func TestAdminUploadOversizeRejected(t *testing.T) {
	store := &fakeStore{}
	handler := NewHandler(store)
	big := make([]byte, MaxImageBytes+1024)
	body, ct := multipartUpload(t, "file", "qr.png", big)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/alipay", body).WithContext(adminContext())
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(store.uploadCalls) != 0 {
		t.Fatalf("upload should not run for oversize file")
	}
}

func TestAdminUploadValidPNGAccepted(t *testing.T) {
	store := &fakeStore{statuses: []AdminStatus{{PaymentMethod: "alipay", Configured: true}}}
	handler := NewHandler(store)
	png := makePNG(t, 48, 48)
	body, ct := multipartUpload(t, "file", "whatever.bin", png)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/alipay", body).WithContext(adminContext())
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(store.uploadCalls) != 1 {
		t.Fatalf("upload calls = %d, want 1", len(store.uploadCalls))
	}
	call := store.uploadCalls[0]
	if call.method != "alipay" {
		t.Fatalf("method = %q, want alipay", call.method)
	}
	if call.img.MimeType != "image/png" {
		t.Fatalf("mime = %q, want image/png", call.img.MimeType)
	}
	if call.adminID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("adminID = %q", call.adminID)
	}
	if len(call.img.SHA256) != 64 || call.img.ByteSize != len(png) {
		t.Fatalf("sha/size not populated: sha=%d size=%d", len(call.img.SHA256), call.img.ByteSize)
	}
}

func TestAdminDisable(t *testing.T) {
	store := &fakeStore{statuses: []AdminStatus{{PaymentMethod: "wechat", Configured: false}}}
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/wechat/disable", nil).WithContext(adminContext())
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(store.disableCalls) != 1 || store.disableCalls[0].method != "wechat" {
		t.Fatalf("disable not called correctly: %+v", store.disableCalls)
	}
}

func TestAdminDisableNotConfigured(t *testing.T) {
	store := &fakeStore{disableErr: ErrNotConfigured}
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/payment-qr/alipay/disable", nil).WithContext(adminContext())
	rec := httptest.NewRecorder()
	handler.AdminItem(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAdminStatusesResponse(t *testing.T) {
	store := &fakeStore{statuses: []AdminStatus{
		{PaymentMethod: "alipay", Configured: true, MimeType: "image/png", ByteSize: 100, SHA256: strings.Repeat("a", 64), UpdatedBy: "root"},
		{PaymentMethod: "wechat", Configured: false},
	}}
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/payment-qr", nil).WithContext(adminContext())
	rec := httptest.NewRecorder()
	handler.AdminCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"sha256\"") {
		t.Fatalf("admin status should expose sha256: %s", rec.Body.String())
	}
}

func TestUserAvailabilityHidesTechnicalFields(t *testing.T) {
	store := &fakeStore{availability: []MethodAvailability{
		{PaymentMethod: "alipay", Available: true},
		{PaymentMethod: "wechat", Available: false},
	}}
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/query/payment-qr", nil)
	rec := httptest.NewRecorder()
	handler.UserAvailability(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"sha256", "byte_size", "mime_type", "updated_by", "updated_at"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("user availability leaked %q: %s", forbidden, body)
		}
	}
	// Confirm it parses to the expected shape.
	var parsed struct {
		Items []MethodAvailability `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(parsed.Items))
	}
}

func TestUserImageServesWithSecurityHeaders(t *testing.T) {
	png := makePNG(t, 32, 32)
	store := &fakeStore{image: Image{Data: png, MimeType: "image/png", SHA256: strings.Repeat("b", 64)}}
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/query/payment-qr/alipay/image", nil)
	rec := httptest.NewRecorder()
	handler.UserImage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q", got)
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Fatalf("cache-control missing")
	}
	if got := rec.Header().Get("ETag"); got != `"`+strings.Repeat("b", 64)+`"` {
		t.Fatalf("etag = %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), png) {
		t.Fatalf("body bytes mismatch")
	}
}

func TestUserImageNotModified(t *testing.T) {
	etag := `"` + strings.Repeat("c", 64) + `"`
	store := &fakeStore{image: Image{Data: makePNG(t, 16, 16), MimeType: "image/png", SHA256: strings.Repeat("c", 64)}}
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/query/payment-qr/wechat/image", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	handler.UserImage(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 body should be empty, got %d bytes", rec.Body.Len())
	}
}

func TestUserImageNotConfigured(t *testing.T) {
	store := &fakeStore{imageErr: ErrNotConfigured}
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/query/payment-qr/alipay/image", nil)
	rec := httptest.NewRecorder()
	handler.UserImage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUserImageInvalidMethod(t *testing.T) {
	handler := NewHandler(&fakeStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/query/payment-qr/paypal/image", nil)
	rec := httptest.NewRecorder()
	handler.UserImage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
