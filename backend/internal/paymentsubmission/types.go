// Package paymentsubmission implements the user-submitted payment proof
// ("收肾记录") feature. A logged-in regular user uploads a screenshot of a
// completed payment; the submission is EVIDENCE ONLY and never, by itself,
// changes any paid amount. Paid totals are still computed exclusively from
// approved `payments` rows. Only an admin review that creates a real approved
// payment — through the shared payments.CreatePaymentTx transaction core —
// increases the paid total; the submission then links to that payment.
//
// Image bytes live in the database as bytea (migration 0021), reusing the same
// content validation as paymentqr (magic-number sniff + structural PNG/JPEG/WebP
// decode + decompression-bomb ceiling + SHA-256). The package never writes a
// file to disk, never trusts a client filename or Content-Type, and never logs
// image bytes, query codes, cookies, tokens, or connection strings.
package paymentsubmission

import "errors"

// Submission status values. "已撤销" is a lifecycle of the real payment, not of
// a proof, and is intentionally not represented here.
const (
	StatusSubmitted = "submitted"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
)

// Accepted payment methods for a proof, matching the payment center and the QR
// codes. Bank/cash/other are not user-selectable here.
const (
	MethodAlipay = "alipay"
	MethodWechat = "wechat"
)

// MaxImageBytes bounds a proof image at 10 MiB (larger than a QR image, which is
// capped at 5 MiB). The same paymentqr validator is reused with this ceiling.
const MaxImageBytes = 10 << 20

var (
	// ErrInvalidMethod is returned when the payment method is not alipay/wechat.
	ErrInvalidMethod = errors.New("付款方式无效")
	// ErrNoImage is returned when no image file was provided.
	ErrNoImage = errors.New("请先选择付款截图")
	// ErrNotFound is returned for a missing submission, or when a user asks for
	// a submission that is not theirs — the two are indistinguishable to the
	// caller so ownership never leaks.
	ErrNotFound = errors.New("未找到该收肾记录")
	// ErrRejectReasonRequired is returned when a rejection carries no reason.
	ErrRejectReasonRequired = errors.New("驳回原因不能为空")
	// ErrNotPending is returned when reviewing a submission that is not in the
	// submitted state (already approved or rejected).
	ErrNotPending = errors.New("该收肾记录已被核对，无法重复处理")
	// ErrNoItems is returned when an approval carries no item allocation.
	ErrNoItems = errors.New("请先选择本次付款对应的未付明细")
)

// Image is a proof image loaded for serving. The sha256 travels only in the
// ETag header, never in a JSON body.
type Image struct {
	Data     []byte
	MimeType string
	SHA256   string
}

// UserSubmission is the regular-user view of one of their own proofs. It carries
// only what the user needs to see their own history and fetch their own image;
// it never exposes image bytes, the sha, the internal user id, the linked
// payment id, an admin identity, or any other technical secret.
type UserSubmission struct {
	ID              string  `json:"id"`
	PaymentMethod   string  `json:"payment_method"`
	PrincipalAmount float64 `json:"principal_amount"`
	FeeAmount       float64 `json:"fee_amount"`
	PayableAmount   float64 `json:"payable_amount"`
	Status          string  `json:"status"`
	SubmittedAt     string  `json:"submitted_at"`
	ReviewedAt      string  `json:"reviewed_at,omitempty"`
	RejectReason    string  `json:"reject_reason,omitempty"`
	// Deduplicated reports that this response replays an existing submission
	// because the request carried an already-seen idempotency key. It lets the
	// client tell "your retry landed" apart from "a second proof was filed",
	// and is never persisted.
	Deduplicated bool `json:"deduplicated,omitempty"`
}

// AdminListItem is one row of the admin WPS list. It shows only business fields;
// the sha, byte size, mime type, internal user id and linked payment id belong
// to the detail view's collapsed "技术标识" section, never the main table.
type AdminListItem struct {
	ID              string  `json:"id"`
	CNCode          string  `json:"cn_code"`
	DisplayName     string  `json:"display_name,omitempty"`
	PaymentMethod   string  `json:"payment_method"`
	PrincipalAmount float64 `json:"principal_amount"`
	FeeAmount       float64 `json:"fee_amount"`
	PayableAmount   float64 `json:"payable_amount"`
	Status          string  `json:"status"`
	SubmittedAt     string  `json:"submitted_at"`
	ReviewedAt      string  `json:"reviewed_at,omitempty"`
	ReviewedBy      string  `json:"reviewed_by,omitempty"`
	RejectReason    string  `json:"reject_reason,omitempty"`
}

// AdminListResponse is one page of the filtered result set. Total counts every
// submission matching the filters, not just this page.
type AdminListResponse struct {
	Items      []AdminListItem `json:"items"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Total      int             `json:"total"`
	TotalPages int             `json:"total_pages"`
}

// AdminDetail is the admin detail view: the business fields plus the technical
// identifiers the UI keeps in a collapsed section.
type AdminDetail struct {
	AdminListItem
	MimeType         string `json:"mime_type"`
	ByteSize         int    `json:"byte_size"`
	SHA256           string `json:"sha256"`
	OriginalFilename string `json:"original_filename_safe,omitempty"`
	UserID           string `json:"user_id"`
	LinkedPaymentID  string `json:"linked_payment_id,omitempty"`
}

// CreateInput carries an already-validated proof image plus the trusted user
// identity (taken from the session, never from the multipart body).
type CreateInput struct {
	UserID           string
	CNCode           string
	PaymentMethod    string
	ImageData        []byte
	MimeType         string
	SHA256           string
	ByteSize         int
	OriginalFilename string
	// RequestID is the client-generated idempotency key for this submission,
	// reused across retries of the same upload. Empty means "no key" and
	// disables deduplication for that request.
	RequestID string
}

// ApproveInput is the admin's explicit allocation of the proof to unpaid order
// items. The CN, payment method and idempotency key are NOT taken from here —
// they are derived server-side from the submission being approved.
type ApproveInput struct {
	Items  []ApproveItem
	PaidAt string
	Note   string
}

// ApproveItem allocates an amount to one order item, exactly like the existing
// record-payment flow.
type ApproveItem struct {
	OrderItemID string
	Amount      float64
}
