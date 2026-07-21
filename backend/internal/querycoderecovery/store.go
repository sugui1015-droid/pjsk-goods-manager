package querycoderecovery

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotEligible       = errors.New("query code recovery is not eligible")
	ErrRateLimited       = errors.New("query code recovery is rate limited")
	ErrRejected          = errors.New("query code recovery was rejected")
	ErrCodeMismatch      = errors.New("query code recovery code mismatch")
	ErrAttemptsExhausted = errors.New("query code recovery attempts exhausted")
	ErrDeliveryFailed    = errors.New("query code recovery delivery failed")
	ErrSameQueryCode     = errors.New("new query code matches current query code")
)

type PreparedSend struct {
	ID              string
	UserID          string
	RecoveryEmailID string
	NormalizedCN    string
	EncryptedEmail  []byte
	Code            string
	ExpiresAt       time.Time
}

type VerifiedCode struct {
	ResetToken string
	ExpiresAt  time.Time
}

type Store interface {
	PrepareRequest(context.Context, string, string) (PreparedSend, error)
	ConfirmSent(context.Context, PreparedSend) error
	MarkDeliveryFailed(context.Context, string, string) error
	VerifyCode(context.Context, string, string) (VerifiedCode, error)
	// ResetQueryCode returns the CN of the account whose query code was
	// reset, so callers can lift that account's login-side rate-limit block
	// once the write has committed.
	ResetQueryCode(context.Context, string, string) (string, error)
}

type PostgresStore struct {
	pool    *pgxpool.Pool
	manager *Manager
	policy  Policy
}

func NewPostgresStore(pool *pgxpool.Pool, manager *Manager) *PostgresStore {
	return &PostgresStore{pool: pool, manager: manager, policy: manager.Policy()}
}

func (s *PostgresStore) PrepareRequest(ctx context.Context, cn string, ip string) (PreparedSend, error) {
	cn = NormalizeCN(cn)
	cnHash, err := s.manager.IdentifierHash("cn", cn)
	if err != nil {
		return PreparedSend{}, err
	}
	ipHash, err := s.manager.IdentifierHash("ip", ip)
	if err != nil {
		return PreparedSend{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PreparedSend{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var cnEvents, ipEvents int
	if err := tx.QueryRow(ctx, `
		select
			count(*) filter (where cn_hash = $1 and created_at > now() - $3::interval)::int,
			count(*) filter (where ip_hash = $2 and created_at > now() - $3::interval)::int
		from query_code_recovery_request_events
	`, cnHash, ipHash, s.policy.Window.String()).Scan(&cnEvents, &ipEvents); err != nil {
		return PreparedSend{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into query_code_recovery_request_events (cn_hash, ip_hash) values ($1, $2)
	`, cnHash, ipHash); err != nil {
		return PreparedSend{}, err
	}
	if cnEvents >= s.policy.WindowLimit || ipEvents >= s.policy.IPWindowLimit {
		if err := tx.Commit(ctx); err != nil {
			return PreparedSend{}, err
		}
		return PreparedSend{}, ErrRateLimited
	}

	prepared := PreparedSend{NormalizedCN: cn}
	var userStatus, currentQueryCodeHash string
	err = tx.QueryRow(ctx, `
		select id::text, status, coalesce(query_code_hash, '')
		from users
		where lower(regexp_replace(btrim(cn_code), '\s+', ' ', 'g')) = $1
		for update
	`, cn).Scan(&prepared.UserID, &userStatus, &currentQueryCodeHash)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return PreparedSend{}, err
		}
		return PreparedSend{}, ErrNotEligible
	}
	if err != nil {
		return PreparedSend{}, err
	}
	if userStatus != "active" || currentQueryCodeHash == "" {
		if err := tx.Commit(ctx); err != nil {
			return PreparedSend{}, err
		}
		return PreparedSend{}, ErrNotEligible
	}

	var emailStatus string
	err = tx.QueryRow(ctx, `
		select id::text, encrypted_email, status
		from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
		for update
	`, prepared.UserID).Scan(&prepared.RecoveryEmailID, &prepared.EncryptedEmail, &emailStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return PreparedSend{}, err
		}
		return PreparedSend{}, ErrNotEligible
	}
	if err != nil {
		return PreparedSend{}, err
	}
	if emailStatus != "verified" {
		if err := tx.Commit(ctx); err != nil {
			return PreparedSend{}, err
		}
		return PreparedSend{}, ErrNotEligible
	}

	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status = 'expired', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and purpose = $2 and status = 'active'
		  and invalidated_at is null and expires_at <= now()
	`, prepared.UserID, Purpose); err != nil {
		return PreparedSend{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status = 'delivery_failed', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and purpose = $2 and status = 'sending'
		  and invalidated_at is null and created_at <= now() - interval '2 minutes'
	`, prepared.UserID, Purpose); err != nil {
		return PreparedSend{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_sessions
		set status = 'expired', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and purpose = $2 and status = 'active'
		  and invalidated_at is null and expires_at <= now()
	`, prepared.UserID, Purpose); err != nil {
		return PreparedSend{}, err
	}

	var databaseNow time.Time
	var latestCreated, oldestInWindow *time.Time
	var windowCount int
	if err := tx.QueryRow(ctx, `
		select now(), max(created_at),
			min(created_at) filter (where created_at > now() - $3::interval),
			count(*) filter (where created_at > now() - $3::interval)::int
		from query_code_recovery_codes
		where user_id = $1::uuid and recovery_email_id = $2::uuid and purpose = 'query_code_recovery'
	`, prepared.UserID, prepared.RecoveryEmailID, s.policy.Window.String()).Scan(&databaseNow, &latestCreated, &oldestInWindow, &windowCount); err != nil {
		return PreparedSend{}, err
	}
	if latestCreated != nil && databaseNow.Before(latestCreated.Add(s.policy.Cooldown)) {
		if err := tx.Commit(ctx); err != nil {
			return PreparedSend{}, err
		}
		return PreparedSend{}, ErrRateLimited
	}
	if windowCount >= s.policy.WindowLimit && oldestInWindow != nil {
		if err := tx.Commit(ctx); err != nil {
			return PreparedSend{}, err
		}
		return PreparedSend{}, ErrRateLimited
	}

	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and purpose = $2 and status in ('sending', 'active') and invalidated_at is null
	`, prepared.UserID, Purpose); err != nil {
		return PreparedSend{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_sessions
		set status = 'invalidated', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where user_id = $1::uuid and purpose = $2 and status = 'active' and invalidated_at is null
	`, prepared.UserID, Purpose); err != nil {
		return PreparedSend{}, err
	}

	prepared.Code, err = s.manager.GenerateCode()
	if err != nil {
		return PreparedSend{}, err
	}
	codeHash, err := s.manager.HashCode(prepared.UserID, prepared.RecoveryEmailID, prepared.NormalizedCN, prepared.Code)
	if err != nil {
		return PreparedSend{}, err
	}
	err = tx.QueryRow(ctx, `
		insert into query_code_recovery_codes (
			user_id, recovery_email_id, purpose, code_hash, ip_hash, status, expires_at, max_attempts
		) values ($1::uuid, $2::uuid, $3, $4, $5, 'sending', now() + $6::interval, $7)
		returning id::text, expires_at
	`, prepared.UserID, prepared.RecoveryEmailID, Purpose, codeHash, ipHash, s.policy.CodeTTL.String(), s.policy.MaxAttempts).Scan(&prepared.ID, &prepared.ExpiresAt)
	if err != nil {
		return PreparedSend{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PreparedSend{}, err
	}
	return prepared, nil
}

func (s *PostgresStore) ConfirmSent(ctx context.Context, prepared PreparedSend) error {
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
		return ErrRejected
	}
	var emailStatus string
	if err := tx.QueryRow(ctx, `
		select status from user_recovery_emails
		where id = $1::uuid and user_id = $2::uuid and invalidated_at is null
		for update
	`, prepared.RecoveryEmailID, prepared.UserID).Scan(&emailStatus); err != nil || emailStatus != "verified" {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return ErrRejected
	}
	var codeStatus string
	if err := tx.QueryRow(ctx, `
		select status from query_code_recovery_codes
		where id = $1::uuid and user_id = $2::uuid and recovery_email_id = $3::uuid and purpose = $4
		for update
	`, prepared.ID, prepared.UserID, prepared.RecoveryEmailID, Purpose).Scan(&codeStatus); err != nil {
		return err
	}
	if codeStatus != "sending" {
		return ErrRejected
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes set status = 'active', sent_at = now(), updated_at = now()
		where id = $1::uuid
	`, prepared.ID); err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{"eligible": true, "operation": "query_code_recovery_requested", "status": "active"})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into account_security_audit_logs (actor_type, target_user_id, action, result, metadata)
		values ('system', $1::uuid, 'query_code_recovery_requested', 'success', $2::jsonb)
	`, prepared.UserID, metadata); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) MarkDeliveryFailed(ctx context.Context, codeID string, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status = 'delivery_failed', invalidated_at = coalesce(invalidated_at, now()), updated_at = now()
		where id = $1::uuid and user_id = $2::uuid and purpose = $3 and status = 'sending'
	`, codeID, userID, Purpose)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		metadata, err := json.Marshal(map[string]any{"operation": "query_code_recovery_failed", "result_category": "delivery_failed"})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			insert into account_security_audit_logs (actor_type,target_user_id,action,result,metadata)
			values ('system',$1::uuid,'query_code_recovery_failed','failure',$2::jsonb)
		`, userID, metadata); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) VerifyCode(ctx context.Context, cn string, code string) (VerifiedCode, error) {
	cn = NormalizeCN(cn)
	if !ValidCode(code) {
		return VerifiedCode{}, ErrInvalidCode
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return VerifiedCode{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID, userStatus string
	err = tx.QueryRow(ctx, `
		select id::text, status from users
		where lower(regexp_replace(btrim(cn_code), '\s+', ' ', 'g')) = $1
		for update
	`, cn).Scan(&userID, &userStatus)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && userStatus != "active") {
		return VerifiedCode{}, ErrRejected
	}
	if err != nil {
		return VerifiedCode{}, err
	}
	var emailID, emailStatus string
	err = tx.QueryRow(ctx, `
		select id::text, status from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
		for update
	`, userID).Scan(&emailID, &emailStatus)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && emailStatus != "verified") {
		return VerifiedCode{}, ErrRejected
	}
	if err != nil {
		return VerifiedCode{}, err
	}

	var codeID, codeHash, codeStatus string
	var attempts, maxAttempts int
	var expired bool
	err = tx.QueryRow(ctx, `
		select id::text, code_hash, status, attempt_count, max_attempts, expires_at <= now()
		from query_code_recovery_codes
		where user_id = $1::uuid and recovery_email_id = $2::uuid and purpose = $3
		order by created_at desc limit 1 for update
	`, userID, emailID, Purpose).Scan(&codeID, &codeHash, &codeStatus, &attempts, &maxAttempts, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifiedCode{}, ErrRejected
	}
	if err != nil {
		return VerifiedCode{}, err
	}
	if codeStatus != "active" {
		if codeStatus == "locked" {
			return VerifiedCode{}, ErrAttemptsExhausted
		}
		return VerifiedCode{}, ErrRejected
	}
	if expired {
		if _, err := tx.Exec(ctx, `update query_code_recovery_codes set status='expired', invalidated_at=coalesce(invalidated_at,now()), updated_at=now() where id=$1::uuid`, codeID); err != nil {
			return VerifiedCode{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return VerifiedCode{}, err
		}
		return VerifiedCode{}, ErrRejected
	}
	if !s.manager.MatchesCode(userID, emailID, cn, code, codeHash) {
		attempts++
		locked := attempts >= maxAttempts
		status := "active"
		if locked {
			status = "locked"
		}
		if _, err := tx.Exec(ctx, `
			update query_code_recovery_codes
			set attempt_count=$2, status=$3,
				invalidated_at=case when $3='locked' then coalesce(invalidated_at,now()) else invalidated_at end,
				updated_at=now()
			where id=$1::uuid
		`, codeID, attempts, status); err != nil {
			return VerifiedCode{}, err
		}
		if locked {
			metadata, err := json.Marshal(map[string]any{"attempt_count": attempts, "operation": "query_code_recovery_locked", "status": "locked"})
			if err != nil {
				return VerifiedCode{}, err
			}
			if _, err := tx.Exec(ctx, `
				insert into account_security_audit_logs (actor_type,target_user_id,action,result,metadata)
				values ('system',$1::uuid,'query_code_recovery_locked','failure',$2::jsonb)
			`, userID, metadata); err != nil {
				return VerifiedCode{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return VerifiedCode{}, err
		}
		if locked {
			return VerifiedCode{}, ErrAttemptsExhausted
		}
		return VerifiedCode{}, ErrCodeMismatch
	}

	token, err := s.manager.GenerateToken()
	if err != nil {
		return VerifiedCode{}, err
	}
	tokenHash, err := s.manager.HashToken(token)
	if err != nil {
		return VerifiedCode{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes set status='used', used_at=now(), updated_at=now() where id=$1::uuid
	`, codeID); err != nil {
		return VerifiedCode{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status='invalidated', invalidated_at=coalesce(invalidated_at,now()), updated_at=now()
		where user_id=$1::uuid and purpose=$2 and id<>$3::uuid and status in ('sending','active') and invalidated_at is null
	`, userID, Purpose, codeID); err != nil {
		return VerifiedCode{}, err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_sessions
		set status='invalidated', invalidated_at=coalesce(invalidated_at,now()), updated_at=now()
		where user_id=$1::uuid and purpose=$2 and status='active' and invalidated_at is null
	`, userID, Purpose); err != nil {
		return VerifiedCode{}, err
	}
	result := VerifiedCode{ResetToken: token}
	if err := tx.QueryRow(ctx, `
		insert into query_code_recovery_sessions (user_id,recovery_email_id,purpose,token_hash,status,expires_at)
		values ($1::uuid,$2::uuid,$3,$4,'active',now()+$5::interval)
		returning expires_at
	`, userID, emailID, Purpose, tokenHash, s.policy.TokenTTL.String()).Scan(&result.ExpiresAt); err != nil {
		return VerifiedCode{}, err
	}
	metadata, err := json.Marshal(map[string]any{"operation": "query_code_recovery_code_verified", "status": "verified"})
	if err != nil {
		return VerifiedCode{}, err
	}
	if _, err := tx.Exec(ctx, `
		insert into account_security_audit_logs (actor_type,target_user_id,action,result,metadata)
		values ('system',$1::uuid,'query_code_recovery_code_verified','success',$2::jsonb)
	`, userID, metadata); err != nil {
		return VerifiedCode{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifiedCode{}, err
	}
	return result, nil
}

func (s *PostgresStore) ResetQueryCode(ctx context.Context, tokenHash string, newQueryCode string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sessionID, userID, emailID string
	err = tx.QueryRow(ctx, `
		select id::text,user_id::text,recovery_email_id::text
		from query_code_recovery_sessions
		where token_hash=$1 and purpose=$2
	`, tokenHash, Purpose).Scan(&sessionID, &userID, &emailID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRejected
	}
	if err != nil {
		return "", err
	}
	var userStatus, currentHash, cnCode string
	if err := tx.QueryRow(ctx, `select status,coalesce(query_code_hash,''),cn_code from users where id=$1::uuid for update`, userID).Scan(&userStatus, &currentHash, &cnCode); err != nil {
		return "", err
	}
	if userStatus != "active" || currentHash == "" {
		return "", ErrRejected
	}
	var currentEmailID, emailStatus string
	err = tx.QueryRow(ctx, `
		select id::text,status from user_recovery_emails
		where user_id=$1::uuid and invalidated_at is null for update
	`, userID).Scan(&currentEmailID, &emailStatus)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (currentEmailID != emailID || emailStatus != "verified")) {
		return "", ErrRejected
	}
	if err != nil {
		return "", err
	}
	var sessionStatus string
	var expired bool
	if err := tx.QueryRow(ctx, `
		select status,expires_at<=now() from query_code_recovery_sessions
		where id=$1::uuid and token_hash=$2 and user_id=$3::uuid and recovery_email_id=$4::uuid and purpose=$5
		for update
	`, sessionID, tokenHash, userID, emailID, Purpose).Scan(&sessionStatus, &expired); err != nil {
		return "", err
	}
	if sessionStatus != "active" || expired {
		if sessionStatus == "active" && expired {
			if _, err := tx.Exec(ctx, `update query_code_recovery_sessions set status='expired',invalidated_at=coalesce(invalidated_at,now()),updated_at=now() where id=$1::uuid`, sessionID); err != nil {
				return "", err
			}
			if err := tx.Commit(ctx); err != nil {
				return "", err
			}
		}
		return "", ErrRejected
	}
	if bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(newQueryCode)) == nil {
		return "", ErrSameQueryCode
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newQueryCode), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		update users set query_code_hash=$2,query_code_updated_at=now(),updated_at=now() where id=$1::uuid
	`, userID, string(newHash)); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `delete from query_sessions where user_id=$1::uuid`, userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_codes
		set status='invalidated',invalidated_at=coalesce(invalidated_at,now()),updated_at=now()
		where user_id=$1::uuid and purpose=$2 and status in ('sending','active') and invalidated_at is null
	`, userID, Purpose); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_sessions
		set status='invalidated',invalidated_at=coalesce(invalidated_at,now()),updated_at=now()
		where user_id=$1::uuid and purpose=$2 and id<>$3::uuid and status='active' and invalidated_at is null
	`, userID, Purpose, sessionID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		update query_code_recovery_sessions set status='used',used_at=now(),updated_at=now() where id=$1::uuid
	`, sessionID); err != nil {
		return "", err
	}
	metadata, err := json.Marshal(map[string]any{"operation": "query_code_recovery_completed", "query_sessions_invalidated": true, "reset_completed": true})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		insert into account_security_audit_logs (actor_type,target_user_id,action,result,metadata)
		values ('system',$1::uuid,'query_code_recovery_completed','success',$2::jsonb)
	`, userID, metadata); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return cnCode, nil
}
