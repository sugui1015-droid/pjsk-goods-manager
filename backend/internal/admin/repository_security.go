package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNoUsableCode reports that a recovery or verification code did not match
// any live code. It deliberately carries no detail about whether the code was
// wrong, expired, used, revoked, or attempt-limited.
var ErrNoUsableCode = errors.New("no usable code")

// AdminRecoveryEmailRecord is the non-sensitive view of an admin's recovery
// email: ciphertext for the protector plus lifecycle state. The plaintext
// address never appears outside the protector.
type AdminRecoveryEmailRecord struct {
	HasRecoveryEmail bool
	EncryptedEmail   []byte
	Status           string
	VerifiedAt       *time.Time
	UpdatedAt        *time.Time
}

type RecoveryCodeBatchStatus struct {
	RemainingCodes int
	GeneratedAt    *time.Time
}

// AdminAuthAuditSummaryEntry is one row of the self-service audit summary.
// It intentionally exposes no user-agent detail beyond what the audit table
// already stores and never any secret material.
type AdminAuthAuditSummaryEntry struct {
	EventType  string    `json:"event_type"`
	OccurredAt time.Time `json:"occurred_at"`
	ClientIP   string    `json:"client_ip"`
	Result     string    `json:"result"`
	ReasonCode string    `json:"reason_code"`
}

// SecurityStore is the storage surface for owner/security flows. It is a
// separate interface from Store so existing Store fakes keep compiling.
type SecurityStore interface {
	// SessionReauthAt reports the reauth timestamp of a live session, with
	// ok=false when the session has never re-authenticated.
	SessionReauthAt(ctx context.Context, tokenHash string) (time.Time, bool, error)
	SetSessionReauth(ctx context.Context, tokenHash string, at time.Time, event AdminAuthAuditEvent) error

	// UpdatePasswordKeepSession changes the password, deletes every other
	// session of the admin, stamps the surviving session's reauth, and writes
	// the audit event — all in one transaction.
	UpdatePasswordKeepSession(ctx context.Context, adminID string, newHash string, keepTokenHash string, reauthAt time.Time, event AdminAuthAuditEvent) error

	// ReplaceRecoveryCodes revokes every live code of the admin and inserts
	// the new batch in one transaction.
	ReplaceRecoveryCodes(ctx context.Context, adminID string, batchID string, codeHashes [][]byte, event AdminAuthAuditEvent) error
	RecoveryCodeStatus(ctx context.Context, adminID string) (RecoveryCodeBatchStatus, error)

	// ResetPasswordWithRecoveryCode consumes one usable code and resets the
	// password, deleting all sessions of the admin, atomically. It returns
	// ErrNoUsableCode when nothing matched; the audit event is written by the
	// caller in that case so the failure is still recorded.
	ResetPasswordWithRecoveryCode(ctx context.Context, adminID string, codeHash []byte, newHash string, event AdminAuthAuditEvent) error

	RecoveryEmail(ctx context.Context, adminID string) (AdminRecoveryEmailRecord, error)
	UpsertPendingRecoveryEmail(ctx context.Context, adminID string, encrypted []byte, lookupHash string) error
	CreateEmailCode(ctx context.Context, adminID string, purpose string, codeHash string, expiresAt time.Time) error
	// ConsumeEmailCode marks the matching live code consumed; a mismatch
	// increments the live code's attempt counter and returns ErrNoUsableCode.
	ConsumeEmailCode(ctx context.Context, adminID string, purpose string, codeHash string) error
	MarkRecoveryEmailVerified(ctx context.Context, adminID string, event AdminAuthAuditEvent) error
	// ResetPasswordWithEmailCode consumes a live reset code and resets the
	// password, deleting all sessions, atomically.
	ResetPasswordWithEmailCode(ctx context.Context, adminID string, codeHash string, newHash string, event AdminAuthAuditEvent) error

	ListAuthEvents(ctx context.Context, adminID string, limit int) ([]AdminAuthAuditSummaryEntry, error)
}

func (s *PostgresStore) SessionReauthAt(ctx context.Context, tokenHash string) (time.Time, bool, error) {
	var reauthAt *time.Time
	err := s.pool.QueryRow(ctx, `
		select reauth_at from admin_sessions
		where token_hash = $1 and expires_at > now()
	`, tokenHash).Scan(&reauthAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, ErrNotFound
	}
	if err != nil {
		return time.Time{}, false, err
	}
	if reauthAt == nil {
		return time.Time{}, false, nil
	}
	return *reauthAt, true, nil
}

func (s *PostgresStore) SetSessionReauth(ctx context.Context, tokenHash string, at time.Time, event AdminAuthAuditEvent) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update admin_sessions set reauth_at = $2
		where token_hash = $1 and expires_at > now()
	`, tokenHash, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) UpdatePasswordKeepSession(
	ctx context.Context,
	adminID string,
	newHash string,
	keepTokenHash string,
	reauthAt time.Time,
	event AdminAuthAuditEvent,
) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update admins set password_hash = $2, updated_at = now()
		where id = $1::uuid
	`, adminID, newHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		delete from admin_sessions where admin_id = $1::uuid and token_hash <> $2
	`, adminID, keepTokenHash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update admin_sessions set reauth_at = $3
		where admin_id = $1::uuid and token_hash = $2
	`, adminID, keepTokenHash, reauthAt); err != nil {
		return err
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ReplaceRecoveryCodes(
	ctx context.Context,
	adminID string,
	batchID string,
	codeHashes [][]byte,
	event AdminAuthAuditEvent,
) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		update admin_recovery_codes set revoked_at = now()
		where admin_id = $1::uuid and revoked_at is null and used_at is null
	`, adminID); err != nil {
		return err
	}
	for _, hash := range codeHashes {
		if _, err := tx.Exec(ctx, `
			insert into admin_recovery_codes (admin_id, code_hash, batch_id)
			values ($1::uuid, $2, $3::uuid)
		`, adminID, hash, batchID); err != nil {
			return err
		}
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) RecoveryCodeStatus(ctx context.Context, adminID string) (RecoveryCodeBatchStatus, error) {
	var status RecoveryCodeBatchStatus
	err := s.pool.QueryRow(ctx, `
		select count(*) filter (where used_at is null and revoked_at is null),
			max(created_at)
		from admin_recovery_codes
		where admin_id = $1::uuid
	`, adminID).Scan(&status.RemainingCodes, &status.GeneratedAt)
	return status, err
}

func (s *PostgresStore) ResetPasswordWithRecoveryCode(
	ctx context.Context,
	adminID string,
	codeHash []byte,
	newHash string,
	event AdminAuthAuditEvent,
) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update admin_recovery_codes set used_at = now()
		where admin_id = $1::uuid and code_hash = $2
			and used_at is null and revoked_at is null
	`, adminID, codeHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoUsableCode
	}
	if err := resetPasswordAndSessionsTx(ctx, tx, adminID, newHash); err != nil {
		return err
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) RecoveryEmail(ctx context.Context, adminID string) (AdminRecoveryEmailRecord, error) {
	var record AdminRecoveryEmailRecord
	err := s.pool.QueryRow(ctx, `
		select email_encrypted, status, verified_at, updated_at
		from admin_recovery_emails
		where admin_id = $1::uuid
	`, adminID).Scan(&record.EncryptedEmail, &record.Status, &record.VerifiedAt, &record.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminRecoveryEmailRecord{}, nil
	}
	if err != nil {
		return AdminRecoveryEmailRecord{}, err
	}
	record.HasRecoveryEmail = true
	return record, nil
}

func (s *PostgresStore) UpsertPendingRecoveryEmail(ctx context.Context, adminID string, encrypted []byte, lookupHash string) error {
	_, err := s.pool.Exec(ctx, `
		insert into admin_recovery_emails (admin_id, email_encrypted, email_hash, status)
		values ($1::uuid, $2, $3, 'pending')
		on conflict (admin_id) do update
			set email_encrypted = excluded.email_encrypted,
				email_hash = excluded.email_hash,
				status = 'pending',
				verified_at = null,
				updated_at = now()
	`, adminID, encrypted, lookupHash)
	return err
}

func (s *PostgresStore) CreateEmailCode(ctx context.Context, adminID string, purpose string, codeHash string, expiresAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A new code supersedes any live code with the same purpose, so exactly
	// one code per purpose can ever be redeemed.
	if _, err := tx.Exec(ctx, `
		update admin_recovery_email_codes set consumed_at = now()
		where admin_id = $1::uuid and purpose = $2 and consumed_at is null
	`, adminID, purpose); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into admin_recovery_email_codes (admin_id, purpose, code_hash, expires_at)
		values ($1::uuid, $2, $3, $4)
	`, adminID, purpose, codeHash, expiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const maxEmailCodeAttempts = 5

func (s *PostgresStore) ConsumeEmailCode(ctx context.Context, adminID string, purpose string, codeHash string) error {
	return consumeEmailCode(ctx, s.pool, adminID, purpose, codeHash)
}

func consumeEmailCode(ctx context.Context, db pgxExecutor, adminID string, purpose string, codeHash string) error {
	tag, err := db.Exec(ctx, `
		update admin_recovery_email_codes set consumed_at = now()
		where id = (
			select id from admin_recovery_email_codes
			where admin_id = $1::uuid and purpose = $2 and code_hash = $3
				and consumed_at is null and expires_at > now()
				and attempt_count < $4
			order by created_at desc
			limit 1
		)
	`, adminID, purpose, codeHash, maxEmailCodeAttempts)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	// Wrong code: burn one attempt on the live code so guessing is bounded
	// even without the IP rate limiter.
	if _, err := db.Exec(ctx, `
		update admin_recovery_email_codes set attempt_count = attempt_count + 1
		where admin_id = $1::uuid and purpose = $2
			and consumed_at is null and expires_at > now()
	`, adminID, purpose); err != nil {
		return err
	}
	return ErrNoUsableCode
}

func (s *PostgresStore) MarkRecoveryEmailVerified(ctx context.Context, adminID string, event AdminAuthAuditEvent) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		update admin_recovery_emails
		set status = 'verified', verified_at = now(), updated_at = now()
		where admin_id = $1::uuid
	`, adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ResetPasswordWithEmailCode(
	ctx context.Context,
	adminID string,
	codeHash string,
	newHash string,
	event AdminAuthAuditEvent,
) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := consumeEmailCode(ctx, tx, adminID, "reset", codeHash); err != nil {
		// The attempt-count bump above must survive even though the reset
		// fails, so commit before surfacing the error.
		if errors.Is(err, ErrNoUsableCode) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return commitErr
			}
		}
		return err
	}
	if err := resetPasswordAndSessionsTx(ctx, tx, adminID, newHash); err != nil {
		return err
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ListAuthEvents(ctx context.Context, adminID string, limit int) ([]AdminAuthAuditSummaryEntry, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		select event_type, occurred_at, client_ip, result, reason_code
		from admin_auth_audit_events
		where admin_id = $1::uuid
		order by occurred_at desc
		limit $2
	`, adminID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]AdminAuthAuditSummaryEntry, 0, limit)
	for rows.Next() {
		var entry AdminAuthAuditSummaryEntry
		if err := rows.Scan(&entry.EventType, &entry.OccurredAt, &entry.ClientIP, &entry.Result, &entry.ReasonCode); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// --- CLI-only storage (owner bootstrap and emergency reset) ---

// SchemaMigrationApplied lets the CLI refuse to run against a database that
// has not applied a required migration, so it can never write owner state
// into a pre-0022 schema (for example the frozen local 19/0019 database).
func (s *PostgresStore) SchemaMigrationApplied(ctx context.Context, version string) (bool, error) {
	var applied bool
	err := s.pool.QueryRow(ctx, `
		select exists (select 1 from schema_migrations where version = $1)
	`, version).Scan(&applied)
	return applied, err
}

func (s *PostgresStore) CountOwners(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `select count(*) from admins where role = 'owner'`).Scan(&count)
	return count, err
}

// FindOwner returns the single owner account, ErrNotFound when none exists.
func (s *PostgresStore) FindOwner(ctx context.Context) (Admin, error) {
	var account Admin
	err := s.pool.QueryRow(ctx, `
		select id::text, username, password_hash, display_name, role, status
		from admins where role = 'owner'
	`).Scan(&account.ID, &account.Username, &account.PasswordHash, &account.DisplayName, &account.Role, &account.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	return account, err
}

// PromoteOwner promotes one active admin to owner, only while zero owners
// exist, with the audit event in the same transaction. The partial unique
// index admins_single_owner_unique independently guarantees at most one
// owner even if two promotions race.
func (s *PostgresStore) PromoteOwner(ctx context.Context, adminID string, event AdminAuthAuditEvent) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var owners int
	if err := tx.QueryRow(ctx, `select count(*) from admins where role = 'owner'`).Scan(&owners); err != nil {
		return err
	}
	if owners != 0 {
		return errors.New("an owner already exists; bootstrap promotion is only allowed when there is no owner")
	}
	tag, err := tx.Exec(ctx, `
		update admins set role = 'owner', updated_at = now()
		where id = $1::uuid and role = 'admin' and status = 'active'
	`, adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResetOwnerPassword is the emergency CLI path: new hash, all sessions of
// the owner revoked, audit event — one transaction, so a failure changes
// nothing.
func (s *PostgresStore) ResetOwnerPassword(ctx context.Context, ownerID string, newHash string, event AdminAuthAuditEvent) error {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var role string
	if err := tx.QueryRow(ctx, `select role from admins where id = $1::uuid`, ownerID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if role != "owner" {
		return errors.New("target account is not the owner")
	}
	if err := resetPasswordAndSessionsTx(ctx, tx, ownerID, newHash); err != nil {
		return err
	}
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// pgxExecutor abstracts pgxpool.Pool and pgx.Tx for shared statements.
type pgxExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// insertAuditEventTx writes one already-validated audit event inside the
// caller's transaction, so state changes and their audit trail commit or
// roll back together.
func insertAuditEventTx(ctx context.Context, tx pgx.Tx, event AdminAuthAuditEvent) error {
	var adminID any
	if event.AdminID != nil {
		adminID = *event.AdminID
	}
	_, err := tx.Exec(ctx, `
		insert into admin_auth_audit_events (
			event_type, occurred_at, admin_id, username_normalized,
			client_ip, result, reason_code, user_agent_summary
		) values ($1, $2, $3::uuid, $4, $5, $6, $7, $8)
	`, event.EventType, event.OccurredAt, adminID, event.UsernameNormalized, event.ClientIP, event.Result, event.ReasonCode, event.UserAgentSummary)
	return err
}

// resetPasswordAndSessionsTx applies a recovery outcome: new password hash,
// every session of the admin revoked (which also clears any reauth state,
// since reauth lives on sessions).
func resetPasswordAndSessionsTx(ctx context.Context, tx pgx.Tx, adminID string, newHash string) error {
	tag, err := tx.Exec(ctx, `
		update admins set password_hash = $2, updated_at = now()
		where id = $1::uuid
	`, adminID, newHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = tx.Exec(ctx, `delete from admin_sessions where admin_id = $1::uuid`, adminID)
	return err
}
