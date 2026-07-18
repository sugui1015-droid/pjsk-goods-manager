package paymentqr

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// orderedMethods is the stable presentation order for admin status.
var orderedMethods = []string{MethodAlipay, MethodWechat}

// PostgresStore implements Store against the payment_qr_codes table (migration
// 0020). Image bytes live in bytea; enabled rows are the current codes and
// disabled rows are retained history.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore builds a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) AdminStatuses(ctx context.Context) ([]AdminStatus, error) {
	rows, err := s.pool.Query(ctx, `
		select
			q.payment_method,
			q.mime_type,
			q.byte_size,
			q.sha256,
			to_char(q.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(a.username, '')
		from payment_qr_codes q
		left join admins a on a.id = q.created_by
		where q.enabled = true
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	configured := map[string]AdminStatus{}
	for rows.Next() {
		var st AdminStatus
		if err := rows.Scan(&st.PaymentMethod, &st.MimeType, &st.ByteSize, &st.SHA256, &st.UpdatedAt, &st.UpdatedBy); err != nil {
			return nil, err
		}
		st.Configured = true
		configured[st.PaymentMethod] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	statuses := make([]AdminStatus, 0, len(orderedMethods))
	for _, method := range orderedMethods {
		if st, ok := configured[method]; ok {
			statuses = append(statuses, st)
		} else {
			statuses = append(statuses, AdminStatus{PaymentMethod: method, Configured: false})
		}
	}
	return statuses, nil
}

func (s *PostgresStore) UserAvailability(ctx context.Context) ([]MethodAvailability, error) {
	rows, err := s.pool.Query(ctx, `
		select payment_method
		from payment_qr_codes
		where enabled = true
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	available := map[string]bool{}
	for rows.Next() {
		var method string
		if err := rows.Scan(&method); err != nil {
			return nil, err
		}
		available[method] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]MethodAvailability, 0, len(orderedMethods))
	for _, method := range orderedMethods {
		items = append(items, MethodAvailability{PaymentMethod: method, Available: available[method]})
	}
	return items, nil
}

func (s *PostgresStore) ActiveImage(ctx context.Context, method string) (Image, error) {
	if !validMethod(method) {
		return Image{}, ErrInvalidMethod
	}
	var img Image
	err := s.pool.QueryRow(ctx, `
		select image_data, mime_type, sha256
		from payment_qr_codes
		where payment_method = $1 and enabled = true
	`, method).Scan(&img.Data, &img.MimeType, &img.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, ErrNotConfigured
	}
	if err != nil {
		return Image{}, err
	}
	return img, nil
}

// Upload disables the current enabled code (if any) and inserts a new enabled
// one in a single transaction, serialized per method by an advisory lock keyed
// only on the method. The partial unique index is the ultimate backstop against
// two concurrent enabled rows.
func (s *PostgresStore) Upload(ctx context.Context, method string, img StoredImage, adminID string) error {
	if !validMethod(method) {
		return ErrInvalidMethod
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, "payment_qr:"+method); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		update payment_qr_codes
		set enabled = false, disabled_at = now(), disabled_by = $2::uuid
		where payment_method = $1 and enabled = true
	`, method, adminID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		insert into payment_qr_codes (payment_method, image_data, mime_type, byte_size, sha256, enabled, created_by)
		values ($1, $2, $3, $4, $5, true, $6::uuid)
	`, method, img.Data, img.MimeType, img.ByteSize, img.SHA256, adminID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// Disable flips the current enabled code to disabled, keeping it as history.
func (s *PostgresStore) Disable(ctx context.Context, method string, adminID string) error {
	if !validMethod(method) {
		return ErrInvalidMethod
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, "payment_qr:"+method); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `
		update payment_qr_codes
		set enabled = false, disabled_at = now(), disabled_by = $2::uuid
		where payment_method = $1 and enabled = true
	`, method, adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotConfigured
	}
	return tx.Commit(ctx)
}
