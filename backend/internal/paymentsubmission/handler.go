package paymentsubmission

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/paymentqr"
	"pjsk/backend/internal/payments"
	"pjsk/backend/internal/query"
)

type errorResponse struct {
	Error string `json:"error"`
}

// Handler serves both the regular-user and the admin payment-proof endpoints.
// Route mounting (and thus authentication) is the router's job: user routes are
// wrapped in the query-session middleware that injects the SessionUser, admin
// routes in the admin auth middleware. The handler assumes the caller is already
// authorized for the surface it is mounted on.
type Handler struct {
	store Store
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// ---- Regular user surface ----------------------------------------------------

// UserCollection handles GET (list own proofs) and POST (submit a new proof) at
// /api/query/payment-submissions. The caller's identity comes only from the
// session; nothing about who the user is is read from the request body.
func (h *Handler) UserCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.userList(w, r)
	case http.MethodPost:
		h.userCreate(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (h *Handler) userList(w http.ResponseWriter, r *http.Request) {
	user, ok := query.CurrentSessionUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := h.store.ListForUser(ctx, user.UserID)
	if err != nil {
		log.Printf("payment-submission user list: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) userCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := query.CurrentSessionUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}

	// Stage timings. Production could only see "status=0 after 188s", which
	// says nothing about WHERE the time went. These four stages separate a slow
	// client uplink (readBody dominates) from slow validation or a slow insert,
	// so the next report is actionable without guessing.
	started := time.Now()
	var readBodyMS, validateMS, hashMS, insertMS int64
	stage := "read_body"

	// Bound the body before buffering anything: reject over 10 MiB up front.
	r.Body = http.MaxBytesReader(w, r.Body, MaxImageBytes+1)
	if err := r.ParseMultipartForm(MaxImageBytes + 1); err != nil {
		// A client that walked away mid-body is not a bad request; classify it
		// so the logs stop showing an unexplained status=0.
		if category := clientAbortCategory(r.Context(), err); category != "" {
			log.Printf("payment-submission upload aborted: stage=%s reason=%s elapsed_ms=%d",
				stage, category, time.Since(started).Milliseconds())
			return
		}
		writeError(w, http.StatusBadRequest, "上传内容无效或超过大小限制")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	method := normalizeMethod(r.FormValue("payment_method"))
	if method != MethodAlipay && method != MethodWechat {
		writeError(w, http.StatusBadRequest, ErrInvalidMethod.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrNoImage.Error())
		return
	}
	defer file.Close()

	data, err := readAllLimited(file, MaxImageBytes+1)
	if err != nil {
		if category := clientAbortCategory(r.Context(), err); category != "" {
			log.Printf("payment-submission upload aborted: stage=%s reason=%s size=%d elapsed_ms=%d",
				stage, category, len(data), time.Since(started).Milliseconds())
			return
		}
		writeError(w, http.StatusBadRequest, "图片读取失败或超过大小限制")
		return
	}
	readBodyMS = time.Since(started).Milliseconds()

	// Validation covers the MIME sniff, the structural decode and the SHA-256;
	// they share one pass, so hash time is reported as part of it.
	stage = "validate"
	validateStarted := time.Now()
	validated, err := paymentqr.ValidateImageWithLimit(data, MaxImageBytes)
	validateMS = time.Since(validateStarted).Milliseconds()
	hashMS = validateMS
	if err != nil {
		// Metadata only — never the bytes, never a filename that could be a path.
		log.Printf("payment-submission upload rejected: method=%s size=%d reason=%s read_body_ms=%d validate_ms=%d",
			method, len(data), err.Error(), readBodyMS, validateMS)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	safeName := safeDisplayName(header.Filename, validated.MimeType)
	requestID := r.FormValue("request_id")

	stage = "insert"
	insertStarted := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	submission, err := h.store.Create(ctx, CreateInput{
		UserID:           user.UserID,
		CNCode:           user.CNCode,
		PaymentMethod:    method,
		ImageData:        data,
		MimeType:         validated.MimeType,
		SHA256:           validated.SHA256,
		ByteSize:         validated.ByteSize,
		OriginalFilename: safeName,
		RequestID:        requestID,
	})
	insertMS = time.Since(insertStarted).Milliseconds()
	if err != nil {
		if errors.Is(err, ErrInvalidMethod) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if category := clientAbortCategory(r.Context(), err); category != "" {
			log.Printf("payment-submission upload aborted: stage=%s reason=%s size=%d read_body_ms=%d validate_ms=%d insert_ms=%d elapsed_ms=%d",
				stage, category, validated.ByteSize, readBodyMS, validateMS, insertMS, time.Since(started).Milliseconds())
			return
		}
		log.Printf("payment-submission create: method=%s size=%d sha256=%s result=error err=%s read_body_ms=%d validate_ms=%d insert_ms=%d",
			method, validated.ByteSize, validated.SHA256, logsafe.Category(err), readBodyMS, validateMS, insertMS)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	// Success audit line: sizes, stage timings, status and the safe hash only —
	// never image bytes, filenames as given, query codes, or session material.
	log.Printf("payment-submission create: method=%s size=%d sha256=%s result=success deduplicated=%t read_body_ms=%d validate_ms=%d hash_ms=%d insert_ms=%d total_ms=%d",
		method, validated.ByteSize, validated.SHA256, submission.Deduplicated,
		readBodyMS, validateMS, hashMS, insertMS, time.Since(started).Milliseconds())

	writeJSON(w, http.StatusCreated, map[string]any{"submission": submission})
}

// UserImage handles GET /api/query/payment-submissions/{id}/image. Only the
// owner may read their own image; any other id (missing or someone else's) is a
// 404 with no ownership leak.
func (h *Handler) UserImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	user, ok := query.CurrentSessionUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}
	id, ok := parseImagePath(r.URL.Path, "/api/query/payment-submissions/")
	if !ok {
		writeError(w, http.StatusNotFound, "未找到资源")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	img, err := h.store.UserImage(ctx, user.UserID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, ErrNotFound.Error())
			return
		}
		log.Printf("payment-submission user image: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	serveImage(w, r, img)
}

// ---- Admin surface -----------------------------------------------------------

// AdminCollection handles GET /api/admin/payment-submissions (WPS list).
func (h *Handler) AdminCollection(w http.ResponseWriter, r *http.Request) {
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
	response, err := h.store.AdminList(ctx, filters)
	if err != nil {
		log.Printf("payment-submission admin list: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// Facets handles GET /api/admin/payment-submissions/facets.
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
	response, err := h.store.Facets(ctx, request)
	if err != nil {
		writeFilterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// AdminItem routes /api/admin/payment-submissions/{id}[/image|/reject|/approve].
func (h *Handler) AdminItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/payment-submissions/"), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "未找到资源")
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]

	switch {
	case len(parts) == 1:
		h.adminDetail(w, r, id)
	case len(parts) == 2 && parts[1] == "image":
		h.adminImage(w, r, id)
	case len(parts) == 2 && parts[1] == "reject":
		h.adminReject(w, r, id)
	case len(parts) == 2 && parts[1] == "approve":
		h.adminApprove(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "未找到资源")
	}
}

func (h *Handler) adminDetail(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	detail, err := h.store.AdminDetail(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, ErrNotFound.Error())
			return
		}
		log.Printf("payment-submission admin detail: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"submission": detail})
}

func (h *Handler) adminImage(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	img, err := h.store.AdminImage(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, ErrNotFound.Error())
			return
		}
		log.Printf("payment-submission admin image: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	serveImage(w, r, img)
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) adminReject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var request rejectRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		writeError(w, http.StatusBadRequest, ErrRejectReasonRequired.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	detail, err := h.store.Reject(ctx, id, account.ID, reason)
	if err != nil {
		writeReviewError(w, err)
		return
	}
	log.Printf("payment-submission reject: id=%s admin=%s result=success", id, account.ID)
	writeJSON(w, http.StatusOK, map[string]any{"submission": detail})
}

type approveRequest struct {
	Items  []approveItemRequest `json:"items"`
	PaidAt string               `json:"paid_at"`
	Note   string               `json:"note"`
}

type approveItemRequest struct {
	OrderItemID string  `json:"order_item_id"`
	Amount      float64 `json:"amount"`
}

func (h *Handler) adminApprove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var request approveRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	items := make([]ApproveItem, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, ApproveItem{OrderItemID: item.OrderItemID, Amount: item.Amount})
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	detail, err := h.store.Approve(ctx, id, account.ID, ApproveInput{
		Items:  items,
		PaidAt: request.PaidAt,
		Note:   request.Note,
	})
	if err != nil {
		writeReviewError(w, err)
		return
	}
	log.Printf("payment-submission approve: id=%s admin=%s payment=%s result=success",
		id, account.ID, detail.LinkedPaymentID)
	writeJSON(w, http.StatusOK, map[string]any{"submission": detail})
}

// ---- shared helpers ----------------------------------------------------------

// serveImage writes a proof image with the correct Content-Type, nosniff, a
// revalidating private Cache-Control and an ETag (the sha256). The sha travels
// only in the ETag header — never in a JSON body.
func serveImage(w http.ResponseWriter, r *http.Request, img Image) {
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

// parseImagePath extracts "{id}" from "{prefix}{id}/image".
func parseImagePath(path, prefix string) (string, bool) {
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "image" || parts[0] == "" {
		return "", false
	}
	return parts[0], true
}

// readAllLimited reads at most limit bytes; reaching limit means the payload
// exceeded the intended ceiling (callers pass MaxImageBytes+1). It never
// allocates more than limit bytes.
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= limit {
		return nil, errors.New("图片超过大小限制")
	}
	return data, nil
}

// safeDisplayName derives a safe, path-free display name from the client's
// filename. The result is stored purely as a label and is never used to build a
// filesystem path, so this defends display, not disk. A name that sanitises to
// nothing falls back to a generated one with the correct extension.
func safeDisplayName(clientName, mime string) string {
	base := filepath.Base(strings.TrimSpace(clientName))
	// filepath.Base on a Windows-style path may still contain a drive/backslash
	// on non-Windows hosts, so strip separators explicitly too.
	base = base[strings.LastIndexAny(base, `/\`)+1:]

	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_' || r == ' ' || r == '(' || r == ')':
			b.WriteRune(r)
		case r >= 0x4E00 && r <= 0x9FFF: // common CJK
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(b.String())
	cleaned = strings.Trim(cleaned, ".")
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	if cleaned == "" {
		return "收肾记录" + extForMime(mime)
	}
	return cleaned
}

func extForMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ""
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

// writeFilterError turns a filter/facet parse rejection into a 400, everything
// else into a 500 with a sanitized log line.
func writeFilterError(w http.ResponseWriter, err error) {
	var badRequestErr *BadRequestError
	if errors.As(err, &badRequestErr) {
		writeError(w, http.StatusBadRequest, badRequestErr.Message)
		return
	}
	log.Printf("payment-submission filter error: %s", logsafe.Category(err))
	writeError(w, http.StatusInternalServerError, "服务器内部错误")
}

// writeReviewError maps reject/approve errors — including the payment-allocation
// errors surfaced from the shared payments core — to safe HTTP responses. It
// never leaks SQL, table names, paths or stack traces.
func writeReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, ErrNotFound.Error())
	case errors.Is(err, ErrNotPending):
		writeError(w, http.StatusConflict, ErrNotPending.Error())
	case errors.Is(err, ErrRejectReasonRequired):
		writeError(w, http.StatusBadRequest, ErrRejectReasonRequired.Error())
	case errors.Is(err, ErrNoItems):
		writeError(w, http.StatusBadRequest, ErrNoItems.Error())
	case errors.Is(err, payments.ErrOverPayment):
		writeError(w, http.StatusBadRequest, "本次分配金额超过该明细的未付余额")
	case errors.Is(err, payments.ErrItemMismatch):
		writeError(w, http.StatusBadRequest, "所选明细不属于该 CN")
	case errors.Is(err, payments.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "分配金额必须大于 0")
	case errors.Is(err, payments.ErrNoPaymentItems):
		writeError(w, http.StatusBadRequest, ErrNoItems.Error())
	case errors.Is(err, payments.ErrPaymentTime):
		writeError(w, http.StatusBadRequest, "付款时间格式无效")
	case errors.Is(err, payments.ErrInvalidPaymentMethod):
		writeError(w, http.StatusBadRequest, "付款方式无效")
	case errors.Is(err, payments.ErrUserNotFound), errors.Is(err, payments.ErrCNRequired):
		writeError(w, http.StatusNotFound, "未找到该 CN 对应的用户")
	default:
		log.Printf("payment-submission review error: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode payment-submission JSON response: %v", err)
	}
}

// clientAbortCategory classifies an error as a client-side disconnect, or
// returns "" when the failure is genuinely ours to report.
//
// Production only ever saw Caddy's status=0 with no explanation. These
// categories distinguish "the phone gave up mid-upload" from "we rejected the
// body" — the two look identical in an access log but mean opposite things.
// Nothing is written to the response for an aborted request: the peer is gone,
// and writing would only log a second, misleading error.
func clientAbortCategory(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "client disconnected"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return "request body read canceled"
	}
	// http.MaxBytesReader's own error is a real 413-shaped rejection, not an
	// abort, and must keep its 400 response.
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return ""
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return "client disconnected"
	}
	if strings.Contains(err.Error(), "client disconnected") ||
		strings.Contains(err.Error(), "connection reset by peer") ||
		strings.Contains(err.Error(), "broken pipe") {
		return "client disconnected"
	}
	return ""
}
