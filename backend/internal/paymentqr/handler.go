// Package paymentqr manages the static payment collection QR codes (Alipay and
// WeChat). Admins upload / replace / disable the codes; logged-in regular users
// read the currently enabled code for a method. Image bytes are stored in the
// database (see migration 0020) and served only through authenticated endpoints;
// the package never exposes a filesystem path, never trusts a filename or the
// client's declared Content-Type, and never logs image bytes.
package paymentqr

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/logsafe"
)

// Method values are the only accepted payment methods for QR codes this round.
const (
	MethodAlipay = "alipay"
	MethodWechat = "wechat"
)

var (
	// ErrInvalidMethod is returned for any method other than alipay/wechat.
	ErrInvalidMethod = errors.New("付款方式无效")
	// ErrNotConfigured means the method has no currently enabled QR code.
	ErrNotConfigured = errors.New("该付款方式暂未配置收款二维码")
)

// Store is the persistence contract. Implementations must keep at most one
// enabled row per method and preserve disabled rows as history.
type Store interface {
	// AdminStatuses returns the technical status of both methods, in a stable
	// order (alipay, wechat). It must never load image_data.
	AdminStatuses(ctx context.Context) ([]AdminStatus, error)
	// UserAvailability reports which methods currently have an enabled code.
	UserAvailability(ctx context.Context) ([]MethodAvailability, error)
	// ActiveImage returns the enabled image for a method, or ErrNotConfigured.
	ActiveImage(ctx context.Context, method string) (Image, error)
	// Upload disables the current enabled code (if any) and inserts a new
	// enabled one, atomically. It must serialize per method.
	Upload(ctx context.Context, method string, img StoredImage, adminID string) error
	// Disable disables the current enabled code, keeping it as history. It
	// returns ErrNotConfigured when there is nothing enabled to disable.
	Disable(ctx context.Context, method string, adminID string) error
}

// AdminStatus is the admin-only view: full technical detail for one method.
type AdminStatus struct {
	PaymentMethod string `json:"payment_method"`
	Configured    bool   `json:"configured"`
	MimeType      string `json:"mime_type,omitempty"`
	ByteSize      int    `json:"byte_size,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	UpdatedBy     string `json:"updated_by,omitempty"`
}

// MethodAvailability is the regular-user view: only whether a method is usable.
// It intentionally carries no mime type, size, hash, admin, or timestamp.
type MethodAvailability struct {
	PaymentMethod string `json:"payment_method"`
	Available     bool   `json:"available"`
}

// Image is a QR image loaded for serving.
type Image struct {
	Data     []byte
	MimeType string
	SHA256   string
}

// StoredImage is a validated image about to be persisted.
type StoredImage struct {
	Data     []byte
	MimeType string
	SHA256   string
	ByteSize int
}

type errorResponse struct {
	Error string `json:"error"`
}

// Handler serves both the admin and the regular-user QR endpoints. Route
// mounting (and therefore authentication) is done by the router: admin routes
// are wrapped in the admin auth middleware, user routes in the query-session
// middleware. The handler itself assumes the caller is already authorized for
// the surface it is mounted on.
type Handler struct {
	store Store
}

// NewHandler builds a Handler over the given store.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// AdminCollection handles GET /api/admin/payment-qr (status of both methods).
func (h *Handler) AdminCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	statuses, err := h.store.AdminStatuses(ctx)
	if err != nil {
		log.Printf("payment-qr admin statuses: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": statuses})
}

// AdminItem routes /api/admin/payment-qr/{method}[/image|/disable].
func (h *Handler) AdminItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/payment-qr/"), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "未找到资源")
		return
	}
	parts := strings.Split(rest, "/")
	method := parts[0]
	if !validMethod(method) {
		// Unknown method is a 404 for image/GET semantics and 400 for actions;
		// use 404 here since the resource does not exist.
		writeError(w, http.StatusNotFound, ErrInvalidMethod.Error())
		return
	}

	switch {
	case len(parts) == 1:
		h.adminUpload(w, r, method)
	case len(parts) == 2 && parts[1] == "image":
		h.adminImage(w, r, method)
	case len(parts) == 2 && parts[1] == "disable":
		h.adminDisable(w, r, method)
	default:
		writeError(w, http.StatusNotFound, "未找到资源")
	}
}

func (h *Handler) adminUpload(w http.ResponseWriter, r *http.Request, method string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Bound the body first: reject anything over 5 MiB before buffering it.
	r.Body = http.MaxBytesReader(w, r.Body, MaxImageBytes+1)
	if err := r.ParseMultipartForm(MaxImageBytes + 1); err != nil {
		writeError(w, http.StatusBadRequest, "上传内容无效或超过大小限制")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择要上传的二维码图片（字段名 file）")
		return
	}
	defer file.Close()

	data, err := readAllLimited(file, MaxImageBytes+1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "图片读取失败或超过大小限制")
		return
	}

	validated, err := ValidateImage(data)
	if err != nil {
		// Log only non-recoverable, non-content metadata.
		log.Printf("payment-qr upload rejected: method=%s size=%d reason=%s",
			method, len(data), err.Error())
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := h.store.Upload(ctx, method, StoredImage{
		Data:     data,
		MimeType: validated.MimeType,
		SHA256:   validated.SHA256,
		ByteSize: validated.ByteSize,
	}, account.ID); err != nil {
		log.Printf("payment-qr upload store: method=%s mime=%s size=%d sha256=%s admin=%s result=error err=%s",
			method, validated.MimeType, validated.ByteSize, validated.SHA256, account.ID, logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	// Success audit line: metadata only, never image bytes.
	log.Printf("payment-qr upload store: method=%s mime=%s size=%d sha256=%s admin=%s result=success",
		method, validated.MimeType, validated.ByteSize, validated.SHA256, account.ID)

	statuses, err := h.store.AdminStatuses(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []AdminStatus{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": statuses})
}

func (h *Handler) adminDisable(w http.ResponseWriter, r *http.Request, method string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := h.store.Disable(ctx, method, account.ID); err != nil {
		if errors.Is(err, ErrNotConfigured) {
			writeError(w, http.StatusNotFound, ErrNotConfigured.Error())
			return
		}
		log.Printf("payment-qr disable: method=%s admin=%s err=%s", method, account.ID, logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	log.Printf("payment-qr disable: method=%s admin=%s result=success", method, account.ID)
	statuses, err := h.store.AdminStatuses(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []AdminStatus{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": statuses})
}

func (h *Handler) adminImage(w http.ResponseWriter, r *http.Request, method string) {
	h.serveImage(w, r, method)
}

// UserAvailability handles GET /api/query/payment-qr (which methods are usable).
func (h *Handler) UserAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := h.store.UserAvailability(ctx)
	if err != nil {
		log.Printf("payment-qr user availability: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// UserImage handles GET /api/query/payment-qr/{method}/image.
func (h *Handler) UserImage(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/query/payment-qr/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "image" {
		writeError(w, http.StatusNotFound, "未找到资源")
		return
	}
	method := parts[0]
	if !validMethod(method) {
		writeError(w, http.StatusNotFound, ErrInvalidMethod.Error())
		return
	}
	h.serveImage(w, r, method)
}

// serveImage writes the currently enabled image for method with correct
// Content-Type, nosniff, a revalidating Cache-Control, and an ETag (the sha256).
// It honors If-None-Match with a 304. The sha256 travels only in the ETag header
// here — regular-user JSON responses never carry it.
func (h *Handler) serveImage(w http.ResponseWriter, r *http.Request, method string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	img, err := h.store.ActiveImage(ctx, method)
	if err != nil {
		if errors.Is(err, ErrNotConfigured) {
			writeError(w, http.StatusNotFound, ErrNotConfigured.Error())
			return
		}
		log.Printf("payment-qr serve image: method=%s err=%s", method, logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	etag := `"` + img.SHA256 + `"`
	w.Header().Set("Content-Type", img.MimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", etag)

	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(img.Data)
}

func validMethod(method string) bool {
	return method == MethodAlipay || method == MethodWechat
}

// etagMatches reports whether the If-None-Match header (which may be a
// comma-separated list, and may use a weak "W/" prefix) contains etag.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode payment-qr JSON response: %v", err)
	}
}
