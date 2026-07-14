package recoveryemailverification

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUserInactive      = errors.New("user is not active")
	ErrNoRecoveryEmail   = errors.New("recovery email is not registered")
	ErrEmailDisabled     = errors.New("recovery email is disabled")
	ErrAlreadyVerified   = errors.New("recovery email is already verified")
	ErrNoActiveCode      = errors.New("no active recovery email verification code")
	ErrCodeExpired       = errors.New("recovery email verification code expired")
	ErrCodeUsed          = errors.New("recovery email verification code used")
	ErrCodeInvalidated   = errors.New("recovery email verification code invalidated")
	ErrCodeMismatch      = errors.New("recovery email verification code mismatch")
	ErrAttemptsExhausted = errors.New("recovery email verification attempts exhausted")
	ErrDeliveryFailed    = errors.New("recovery email verification delivery failed")
)

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "recovery email verification rate limited" }

func RetryAfterSeconds(err error) int {
	var rateError *RateLimitError
	if !errors.As(err, &rateError) {
		return 0
	}
	seconds := int(math.Ceil(rateError.RetryAfter.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

type PreparedSend struct {
	ID              string
	UserID          string
	RecoveryEmailID string
	EncryptedEmail  []byte
	Code            string
	ExpiresAt       time.Time
}

type VerifiedState struct {
	EncryptedEmail []byte
	VerifiedAt     time.Time
}

type Store interface {
	PrepareSend(context.Context, string) (PreparedSend, error)
	ConfirmSent(context.Context, PreparedSend, string) error
	MarkDeliveryFailed(context.Context, string, string) error
	Verify(context.Context, string, string) (VerifiedState, error)
}

type PostgresStore struct {
	pool    *pgxpool.Pool
	manager *Manager
	policy  Policy
}

func NewPostgresStore(pool *pgxpool.Pool, manager *Manager) *PostgresStore {
	return &PostgresStore{pool: pool, manager: manager, policy: manager.Policy()}
}

func (s *PostgresStore) PrepareSend(ctx context.Context, userID string) (PreparedSend, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PreparedSend{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userStatus string
	if err := tx.QueryRow(ctx, `select status from users where id = $1::uuid for update`, userID).Scan(&userStatus); errors.Is(err, pgx.ErrNoRows) {
		return PreparedSend{}, ErrUserInactive
	} else if err != nil {
		return PreparedSend{}, err
	}
	if userStatus != "active" {
		return PreparedSend{}, ErrUserInactive
	}

	prepared := PreparedSend{UserID: userID}
	var emailStatus string
	err = tx.QueryRow(ctx, `
		select id::text, encrypted_email, status
		from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
		for update
	`, userID).Scan(&prepared.RecoveryEmailID, &prepared.EncryptedEmail, &emailStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return PreparedSend{}, ErrNoRecoveryEmail
	}
	if err != nil {
		return PreparedSend{}, err
	}
	switch emailStatus {
	case "verified":
		return PreparedSend{}, ErrAlreadyVerified
	case "pending":
		// Continue.
	default:
		return PreparedSend{}, ErrEmailDisabled
	}

	if _, err := tx.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'expired', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and status = 'active' and invalidated_at is null and expires_at <= now()
	`, userID); err != nil {
		return PreparedSend{}, err
	}
	if _, err := tx.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'delivery_failed', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and status = 'sending' and invalidated_at is null
		  and created_at <= now() - interval '2 minutes'
	`, userID); err != nil {
		return PreparedSend{}, err
	}

	var databaseNow time.Time
	var latestCreated, oldestInWindow *time.Time
	var windowCount int
	if err := tx.QueryRow(ctx, `
		select now(), max(created_at),
			min(created_at) filter (where created_at > now() - $2::interval),
			count(*) filter (where created_at > now() - $2::interval)::int
		from recovery_email_verification_codes
		where user_id = $1::uuid
	`, userID, s.policy.Window.String()).Scan(&databaseNow, &latestCreated, &oldestInWindow, &windowCount); err != nil {
		return PreparedSend{}, err
	}
	if latestCreated != nil {
		availableAt := latestCreated.Add(s.policy.Cooldown)
		if databaseNow.Before(availableAt) {
			return PreparedSend{}, &RateLimitError{RetryAfter: availableAt.Sub(databaseNow)}
		}
	}
	if windowCount >= s.policy.WindowLimit && oldestInWindow != nil {
		availableAt := oldestInWindow.Add(s.policy.Window)
		return PreparedSend{}, &RateLimitError{RetryAfter: availableAt.Sub(databaseNow)}
	}

	if _, err := tx.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and status in ('sending', 'active') and invalidated_at is null
	`, userID); err != nil {
		return PreparedSend{}, err
	}
	prepared.Code, err = s.manager.Generate()
	if err != nil {
		return PreparedSend{}, err
	}
	codeHash, err := s.manager.Hash(prepared.RecoveryEmailID, prepared.Code)
	if err != nil {
		return PreparedSend{}, err
	}
	err = tx.QueryRow(ctx, `
		insert into recovery_email_verification_codes (
			user_id, recovery_email_id, code_hash, status, expires_at, max_attempts
		) values ($1::uuid, $2::uuid, $3, 'sending', now() + $4::interval, $5)
		returning id::text, expires_at
	`, userID, prepared.RecoveryEmailID, codeHash, s.policy.TTL.String(), s.policy.MaxAttempts).Scan(&prepared.ID, &prepared.ExpiresAt)
	if err != nil {
		return PreparedSend{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PreparedSend{}, err
	}
	return prepared, nil
}

func (s *PostgresStore) ConfirmSent(ctx context.Context, prepared PreparedSend, maskedEmail string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userStatus string
	if err := tx.QueryRow(ctx, `select status from users where id = $1::uuid for update`, prepared.UserID).Scan(&userStatus); err != nil {
		return err
	}
	if userStatus != "active" {
		return ErrUserInactive
	}
	var emailStatus string
	if err := tx.QueryRow(ctx, `
		select status from user_recovery_emails
		where id = $1::uuid and user_id = $2::uuid and invalidated_at is null
		for update
	`, prepared.RecoveryEmailID, prepared.UserID).Scan(&emailStatus); errors.Is(err, pgx.ErrNoRows) {
		return ErrCodeInvalidated
	} else if err != nil {
		return err
	}
	if emailStatus != "pending" {
		return ErrCodeInvalidated
	}
	var codeStatus string
	if err := tx.QueryRow(ctx, `
		select status from recovery_email_verification_codes
		where id = $1::uuid and user_id = $2::uuid and recovery_email_id = $3::uuid
		for update
	`, prepared.ID, prepared.UserID, prepared.RecoveryEmailID).Scan(&codeStatus); err != nil {
		return err
	}
	if codeStatus != "sending" {
		return ErrCodeInvalidated
	}
	if _, err := tx.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'active', sent_at = now(), updated_at = now()
		where id = $1::uuid
	`, prepared.ID); err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"masked_email": maskedEmail,
		"operation":    "recovery_email_verification_sent",
		"status":       "active",
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into account_security_audit_logs (actor_type, target_user_id, action, result, metadata)
		values ('user', $1::uuid, 'recovery_email_verification_sent', 'success', $2::jsonb)
	`, prepared.UserID, metadata); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) MarkDeliveryFailed(ctx context.Context, codeID string, userID string) error {
	_, err := s.pool.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'delivery_failed', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where id = $1::uuid and user_id = $2::uuid and status = 'sending'
	`, codeID, userID)
	return err
}

func (s *PostgresStore) Verify(ctx context.Context, userID string, code string) (VerifiedState, error) {
	if !ValidCode(code) {
		return VerifiedState{}, ErrInvalidCode
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VerifiedState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userStatus string
	if err := tx.QueryRow(ctx, `select status from users where id = $1::uuid for update`, userID).Scan(&userStatus); errors.Is(err, pgx.ErrNoRows) {
		return VerifiedState{}, ErrUserInactive
	} else if err != nil {
		return VerifiedState{}, err
	}
	if userStatus != "active" {
		return VerifiedState{}, ErrUserInactive
	}

	var recoveryEmailID, emailStatus string
	var encryptedEmail []byte
	err = tx.QueryRow(ctx, `
		select id::text, encrypted_email, status
		from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
		for update
	`, userID).Scan(&recoveryEmailID, &encryptedEmail, &emailStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifiedState{}, ErrNoRecoveryEmail
	}
	if err != nil {
		return VerifiedState{}, err
	}
	if emailStatus == "verified" {
		return VerifiedState{}, ErrAlreadyVerified
	}
	if emailStatus != "pending" {
		return VerifiedState{}, ErrEmailDisabled
	}

	var codeID, codeHash, codeStatus string
	var expiresAt time.Time
	var attempts, maxAttempts int
	err = tx.QueryRow(ctx, `
		select id::text, code_hash, status, expires_at, attempt_count, max_attempts
		from recovery_email_verification_codes
		where user_id = $1::uuid and recovery_email_id = $2::uuid
		order by created_at desc
		limit 1
		for update
	`, userID, recoveryEmailID).Scan(&codeID, &codeHash, &codeStatus, &expiresAt, &attempts, &maxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifiedState{}, ErrNoActiveCode
	}
	if err != nil {
		return VerifiedState{}, err
	}
	switch codeStatus {
	case "used":
		return VerifiedState{}, ErrCodeUsed
	case "locked":
		return VerifiedState{}, ErrAttemptsExhausted
	case "expired":
		return VerifiedState{}, ErrCodeExpired
	case "active":
		// Continue.
	default:
		return VerifiedState{}, ErrCodeInvalidated
	}
	if !expiresAt.After(time.Now()) {
		if _, err := tx.Exec(ctx, `
			update recovery_email_verification_codes
			set status = 'expired', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
			where id = $1::uuid
		`, codeID); err != nil {
			return VerifiedState{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VerifiedState{}, err
		}
		return VerifiedState{}, ErrCodeExpired
	}

	if !s.manager.Matches(recoveryEmailID, code, strings.TrimSpace(codeHash)) {
		attempts++
		locked := attempts >= maxAttempts
		status := "active"
		if locked {
			status = "locked"
		}
		if _, err := tx.Exec(ctx, `
			update recovery_email_verification_codes
			set attempt_count = $2, status = $3,
				invalidated_at = case when $3 = 'locked' then coalesce(invalidated_at, now()) else invalidated_at end,
				updated_at = now()
			where id = $1::uuid
		`, codeID, attempts, status); err != nil {
			return VerifiedState{}, err
		}
		if locked {
			metadata, err := json.Marshal(map[string]any{
				"attempt_count": attempts,
				"operation":     "recovery_email_verification_locked",
				"status":        "locked",
			})
			if err != nil {
				return VerifiedState{}, err
			}
			if _, err := tx.Exec(ctx, `
				insert into account_security_audit_logs (actor_type, target_user_id, action, result, metadata)
				values ('user', $1::uuid, 'recovery_email_verification_locked', 'failure', $2::jsonb)
			`, userID, metadata); err != nil {
				return VerifiedState{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return VerifiedState{}, err
		}
		if locked {
			return VerifiedState{}, ErrAttemptsExhausted
		}
		return VerifiedState{}, ErrCodeMismatch
	}

	var verifiedAt time.Time
	if err := tx.QueryRow(ctx, `
		update user_recovery_emails
		set status = 'verified', verified_at = now(), updated_at = now()
		where id = $1::uuid and user_id = $2::uuid and invalidated_at is null and status = 'pending'
		returning verified_at
	`, recoveryEmailID, userID).Scan(&verifiedAt); err != nil {
		return VerifiedState{}, err
	}
	if _, err := tx.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'used', used_at = now(), updated_at = now()
		where id = $1::uuid
	`, codeID); err != nil {
		return VerifiedState{}, err
	}
	if _, err := tx.Exec(ctx, `
		update recovery_email_verification_codes
		set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and recovery_email_id = $2::uuid and id <> $3::uuid
		  and status in ('sending', 'active') and invalidated_at is null
	`, userID, recoveryEmailID, codeID); err != nil {
		return VerifiedState{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"operation":  "recovery_email_verified",
		"new_status": "verified",
	})
	if err != nil {
		return VerifiedState{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into account_security_audit_logs (actor_type, target_user_id, action, result, metadata)
		values ('user', $1::uuid, 'recovery_email_verified', 'success', $2::jsonb)
	`, userID, metadata); err != nil {
		return VerifiedState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifiedState{}, err
	}
	return VerifiedState{EncryptedEmail: encryptedEmail, VerifiedAt: verifiedAt}, nil
}
