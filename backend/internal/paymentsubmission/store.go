package paymentsubmission

import (
	"context"
	"errors"
	"fmt"

	"pjsk/backend/internal/payments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AmountSource supplies the canonical outstanding principal for a user, so the
// proof records the exact 本金 the payment center showed without a second money
// query of its own. It is satisfied by *payments.PostgresStore.
type AmountSource interface {
	OutstandingPrincipalCents(ctx context.Context, userID string) (int64, error)
}

// Store is the persistence contract behind the handler.
type Store interface {
	// Create inserts a validated proof and returns the user's view of it. The
	// amounts are derived server-side; a failed insert leaves no row.
	Create(ctx context.Context, in CreateInput) (UserSubmission, error)
	// ListForUser returns a user's own submissions, newest first.
	ListForUser(ctx context.Context, userID string) ([]UserSubmission, error)
	// UserImage returns the image for a submission the user owns, or ErrNotFound
	// when the id is unknown OR belongs to someone else (indistinguishable).
	UserImage(ctx context.Context, userID, submissionID string) (Image, error)

	// AdminList returns one filtered, paginated page for the admin table.
	AdminList(ctx context.Context, filters Filters) (AdminListResponse, error)
	// Facets returns candidate values for one column's popover.
	Facets(ctx context.Context, request FacetRequest) (FacetResponse, error)
	// AdminDetail returns full detail (business + technical) for the admin.
	AdminDetail(ctx context.Context, submissionID string) (AdminDetail, error)
	// AdminImage returns the image bytes for any submission (admin surface).
	AdminImage(ctx context.Context, submissionID string) (Image, error)
	// Reject marks a submitted proof rejected with a required reason.
	Reject(ctx context.Context, submissionID, adminID, reason string) (AdminDetail, error)
	// Approve creates a real approved payment (via the shared payments core) and
	// links the proof to it, atomically.
	Approve(ctx context.Context, submissionID, adminID string, in ApproveInput) (AdminDetail, error)
}

// PostgresStore implements Store against the payment_submissions table.
type PostgresStore struct {
	pool    *pgxpool.Pool
	amounts AmountSource
}

// NewPostgresStore builds a PostgresStore. amounts supplies the canonical
// outstanding principal (pass a *payments.PostgresStore).
func NewPostgresStore(pool *pgxpool.Pool, amounts AmountSource) *PostgresStore {
	return &PostgresStore{pool: pool, amounts: amounts}
}

func (s *PostgresStore) Create(ctx context.Context, in CreateInput) (UserSubmission, error) {
	method := normalizeMethod(in.PaymentMethod)
	if method != MethodAlipay && method != MethodWechat {
		return UserSubmission{}, ErrInvalidMethod
	}

	principalCents, err := s.amounts.OutstandingPrincipalCents(ctx, in.UserID)
	if err != nil {
		return UserSubmission{}, err
	}
	if principalCents < 0 {
		principalCents = 0
	}
	feeCents, payableCents := payments.FeeForPrincipalCents(principalCents, method)

	var id, submittedAt string
	err = s.pool.QueryRow(ctx, `
		insert into payment_submissions (
			user_id, cn_code, payment_method,
			principal_amount, fee_amount, payable_amount,
			image_data, mime_type, byte_size, sha256, original_filename_safe
		)
		values ($1::uuid, $2, $3, $4::numeric(12,2), $5::numeric(12,2), $6::numeric(12,2), $7, $8, $9, $10, $11)
		returning id::text, to_char(submitted_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
	`,
		in.UserID, in.CNCode, method,
		centsToNumeric(principalCents), centsToNumeric(feeCents), centsToNumeric(payableCents),
		in.ImageData, in.MimeType, in.ByteSize, in.SHA256, in.OriginalFilename,
	).Scan(&id, &submittedAt)
	if err != nil {
		return UserSubmission{}, err
	}

	return UserSubmission{
		ID:              id,
		PaymentMethod:   method,
		PrincipalAmount: centsToFloat(principalCents),
		FeeAmount:       centsToFloat(feeCents),
		PayableAmount:   centsToFloat(payableCents),
		Status:          StatusSubmitted,
		SubmittedAt:     submittedAt,
	}, nil
}

func (s *PostgresStore) ListForUser(ctx context.Context, userID string) ([]UserSubmission, error) {
	rows, err := s.pool.Query(ctx, `
		select
			id::text,
			lower(coalesce(payment_method, '')),
			principal_amount::float8,
			fee_amount::float8,
			payable_amount::float8,
			status,
			to_char(submitted_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(reviewed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(reject_reason, '')
		from payment_submissions
		where user_id = $1::uuid
		order by submitted_at desc, id desc
		limit 200
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []UserSubmission{}
	for rows.Next() {
		var item UserSubmission
		if err := rows.Scan(
			&item.ID,
			&item.PaymentMethod,
			&item.PrincipalAmount,
			&item.FeeAmount,
			&item.PayableAmount,
			&item.Status,
			&item.SubmittedAt,
			&item.ReviewedAt,
			&item.RejectReason,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UserImage(ctx context.Context, userID, submissionID string) (Image, error) {
	if !isUUIDLike(submissionID) {
		return Image{}, ErrNotFound
	}
	var img Image
	err := s.pool.QueryRow(ctx, `
		select image_data, mime_type, sha256
		from payment_submissions
		where id = $1::uuid and user_id = $2::uuid
	`, submissionID, userID).Scan(&img.Data, &img.MimeType, &img.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		return Image{}, err
	}
	return img, nil
}

func (s *PostgresStore) AdminList(ctx context.Context, filters Filters) (AdminListResponse, error) {
	response := AdminListResponse{
		Items:    []AdminListItem{},
		Page:     filters.Page,
		PageSize: filters.PageSize,
	}

	countQuery, countArgs := buildCountQuery(filters)
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&response.Total); err != nil {
		return AdminListResponse{}, err
	}
	response.TotalPages = (response.Total + filters.PageSize - 1) / filters.PageSize

	listQuery, listArgs := buildListQuery(filters)
	rows, err := s.pool.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return AdminListResponse{}, err
	}
	defer rows.Close()

	for rows.Next() {
		item, err := scanListItem(rows)
		if err != nil {
			return AdminListResponse{}, err
		}
		response.Items = append(response.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminListResponse{}, err
	}
	return response, nil
}

func scanListItem(rows pgx.Rows) (AdminListItem, error) {
	var item AdminListItem
	if err := rows.Scan(
		&item.ID,
		&item.CNCode,
		&item.DisplayName,
		&item.PaymentMethod,
		&item.PrincipalAmount,
		&item.FeeAmount,
		&item.PayableAmount,
		&item.Status,
		&item.SubmittedAt,
		&item.ReviewedAt,
		&item.ReviewedBy,
		&item.RejectReason,
	); err != nil {
		return AdminListItem{}, err
	}
	return item, nil
}

func (s *PostgresStore) AdminDetail(ctx context.Context, submissionID string) (AdminDetail, error) {
	if !isUUIDLike(submissionID) {
		return AdminDetail{}, ErrNotFound
	}
	args := &argList{}
	idPlaceholder := args.add(submissionID)
	query := baseCTE + `
select` + listColumns + `,
	b.mime_type,
	b.byte_size,
	b.sha256,
	b.original_filename_safe,
	b.user_id::text,
	coalesce(b.linked_payment_id::text, '')
from base b
where b.id = ` + idPlaceholder + `::uuid`

	var detail AdminDetail
	err := s.pool.QueryRow(ctx, query, args.values...).Scan(
		&detail.ID,
		&detail.CNCode,
		&detail.DisplayName,
		&detail.PaymentMethod,
		&detail.PrincipalAmount,
		&detail.FeeAmount,
		&detail.PayableAmount,
		&detail.Status,
		&detail.SubmittedAt,
		&detail.ReviewedAt,
		&detail.ReviewedBy,
		&detail.RejectReason,
		&detail.MimeType,
		&detail.ByteSize,
		&detail.SHA256,
		&detail.OriginalFilename,
		&detail.UserID,
		&detail.LinkedPaymentID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminDetail{}, ErrNotFound
	}
	if err != nil {
		return AdminDetail{}, err
	}
	return detail, nil
}

func (s *PostgresStore) AdminImage(ctx context.Context, submissionID string) (Image, error) {
	if !isUUIDLike(submissionID) {
		return Image{}, ErrNotFound
	}
	var img Image
	err := s.pool.QueryRow(ctx, `
		select image_data, mime_type, sha256
		from payment_submissions
		where id = $1::uuid
	`, submissionID).Scan(&img.Data, &img.MimeType, &img.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	if err != nil {
		return Image{}, err
	}
	return img, nil
}

func (s *PostgresStore) Reject(ctx context.Context, submissionID, adminID, reason string) (AdminDetail, error) {
	if !isUUIDLike(submissionID) {
		return AdminDetail{}, ErrNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AdminDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `
		select status from payment_submissions where id = $1::uuid for update
	`, submissionID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminDetail{}, ErrNotFound
	}
	if err != nil {
		return AdminDetail{}, err
	}
	if status != StatusSubmitted {
		return AdminDetail{}, ErrNotPending
	}

	if _, err := tx.Exec(ctx, `
		update payment_submissions
		set status = 'rejected',
			reject_reason = $2,
			reviewed_by_admin_id = $3::uuid,
			reviewed_at = now(),
			updated_at = now()
		where id = $1::uuid and status = 'submitted'
	`, submissionID, reason, adminID); err != nil {
		return AdminDetail{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return AdminDetail{}, err
	}
	return s.AdminDetail(ctx, submissionID)
}

// Approve creates a real approved payment for the proof's CN with the admin's
// explicit item allocation, and marks the proof approved + linked in the SAME
// transaction. Either both land or neither does. Concurrency and double-clicks
// are guarded twice: the submission row is locked FOR UPDATE and re-checked to
// be still 'submitted', and the payment idempotency key is derived from the
// submission id so a duplicate payment can never be inserted.
func (s *PostgresStore) Approve(ctx context.Context, submissionID, adminID string, in ApproveInput) (AdminDetail, error) {
	if !isUUIDLike(submissionID) {
		return AdminDetail{}, ErrNotFound
	}
	if len(in.Items) == 0 {
		return AdminDetail{}, ErrNoItems
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AdminDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var cnCode, method, status string
	err = tx.QueryRow(ctx, `
		select cn_code, lower(coalesce(payment_method, '')), status
		from payment_submissions
		where id = $1::uuid
		for update
	`, submissionID).Scan(&cnCode, &method, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminDetail{}, ErrNotFound
	}
	if err != nil {
		return AdminDetail{}, err
	}
	if status != StatusSubmitted {
		return AdminDetail{}, ErrNotPending
	}

	items := make([]payments.CreatePaymentItemRequest, 0, len(in.Items))
	for _, item := range in.Items {
		items = append(items, payments.CreatePaymentItemRequest{
			OrderItemID: item.OrderItemID,
			Amount:      item.Amount,
		})
	}

	resp, err := payments.CreatePaymentTx(ctx, tx, payments.CreatePaymentRequest{
		CN:             cnCode,
		PaymentMethod:  method,
		PaidAt:         in.PaidAt,
		Note:           in.Note,
		IdempotencyKey: idempotencyKey(submissionID),
		Items:          items,
	}, adminID)
	if err != nil {
		return AdminDetail{}, err
	}

	tag, err := tx.Exec(ctx, `
		update payment_submissions
		set status = 'approved',
			linked_payment_id = $2::uuid,
			reviewed_by_admin_id = $3::uuid,
			reviewed_at = now(),
			updated_at = now()
		where id = $1::uuid and status = 'submitted'
	`, submissionID, resp.PaymentID, adminID)
	if err != nil {
		return AdminDetail{}, err
	}
	// The FOR UPDATE lock already guaranteed the row was 'submitted'; if the
	// update touched nothing the invariant is broken, so refuse rather than
	// commit a payment with an unlinked proof.
	if tag.RowsAffected() != 1 {
		return AdminDetail{}, ErrNotPending
	}

	if err := tx.Commit(ctx); err != nil {
		return AdminDetail{}, err
	}
	return s.AdminDetail(ctx, submissionID)
}

// idempotencyKey is the stable per-submission key for the approval payment.
func idempotencyKey(submissionID string) string {
	return "payment-submission:" + submissionID
}

// centsToNumeric formats integer cents as a numeric(12,2) string, e.g. 12012 ->
// "120.12". No float arithmetic.
func centsToNumeric(cents int64) string {
	sign := ""
	abs := cents
	if cents < 0 {
		sign = "-"
		abs = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, abs/100, abs%100)
}

// centsToFloat converts integer cents to a two-decimal float for JSON display.
func centsToFloat(cents int64) float64 {
	return float64(cents) / 100
}

func isUUIDLike(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
				return false
			}
		}
	}
	return true
}
