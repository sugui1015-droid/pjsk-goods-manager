package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Owner-managed administrator accounts (appoint / enable / disable / revoke /
// reset password). Every mutation runs in one transaction with its audit event
// and the target's session purge, refuses to touch the owner row, and never
// stores or logs any plaintext secret. Revocation is soft: the row is kept
// (status 'revoked') so history survives and re-appointing the same user
// reactivates the same account instead of minting a second one.

var (
	// ErrTargetIsOwner reports an attempt to manage the owner account through
	// the admin management surface, which is forbidden on every path.
	ErrTargetIsOwner = errors.New("the owner account cannot be managed here")
	// ErrUserNotFound reports that the appointment target user does not exist
	// or is not active.
	ErrUserNotFound = errors.New("user not found or not active")
	// ErrUsernameTaken reports an admin username collision.
	ErrUsernameTaken = errors.New("username is already taken")
	// ErrUserAlreadyAdmin reports that the user already has a live (non-revoked)
	// admin account.
	ErrUserAlreadyAdmin = errors.New("user already has an admin account")
	// ErrInvalidTransition reports a state change that does not apply to the
	// target's current status (e.g. enabling an account that is not disabled).
	ErrInvalidTransition = errors.New("state change does not apply to the target's current status")
)

// ManagedAdmin is one row of the owner's admin management list. It carries no
// password hash and no secret material.
type ManagedAdmin struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	DisplayName        *string    `json:"display_name,omitempty"`
	Role               string     `json:"role"`
	Status             string     `json:"status"`
	UserID             *string    `json:"user_id,omitempty"`
	UserCN             *string    `json:"user_cn,omitempty"`
	MustChangePassword bool       `json:"must_change_password"`
	CreatedAt          time.Time  `json:"created_at"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
}

// AppointAdminInput describes one appointment. The password hash is produced
// by the handler from a system-generated temporary password; the plaintext
// never reaches the store.
type AppointAdminInput struct {
	UserID       string
	Username     string
	DisplayName  *string
	PasswordHash string
}

// ManagementStore is the storage surface of the owner admin management
// endpoints, a separate interface so handler tests can fake it.
type ManagementStore interface {
	ListManagedAdmins(ctx context.Context) ([]ManagedAdmin, error)
	GetManagedAdmin(ctx context.Context, adminID string) (ManagedAdmin, error)
	// AppointAdmin creates (or, for a previously revoked link to the same
	// user, reactivates) the admin account with must_change_password set.
	AppointAdmin(ctx context.Context, input AppointAdminInput, event AdminAuthAuditEvent) (ManagedAdmin, error)
	// SetManagedAdminStatus flips active/disabled, purging the target's
	// sessions in the same transaction.
	SetManagedAdminStatus(ctx context.Context, adminID string, status string, event AdminAuthAuditEvent) (ManagedAdmin, error)
	// RevokeManagedAdmin soft-revokes the account and purges its sessions.
	RevokeManagedAdmin(ctx context.Context, adminID string, actorID string, event AdminAuthAuditEvent) (ManagedAdmin, error)
	// ResetManagedAdminPassword installs a new temporary password hash, sets
	// must_change_password, and purges the target's sessions.
	ResetManagedAdminPassword(ctx context.Context, adminID string, newHash string, event AdminAuthAuditEvent) (ManagedAdmin, error)
	ListManagedAdminAudit(ctx context.Context, adminID string, limit int) ([]AdminAuthAuditSummaryEntry, error)
}

const managedAdminColumns = `
	a.id::text, a.username, a.display_name, a.role, a.status,
	a.user_id::text, u.cn_code, a.must_change_password,
	a.created_at, a.last_login_at, a.revoked_at
`

func scanManagedAdmin(row pgx.Row) (ManagedAdmin, error) {
	var entry ManagedAdmin
	err := row.Scan(
		&entry.ID, &entry.Username, &entry.DisplayName, &entry.Role, &entry.Status,
		&entry.UserID, &entry.UserCN, &entry.MustChangePassword,
		&entry.CreatedAt, &entry.LastLoginAt, &entry.RevokedAt,
	)
	return entry, err
}

func (s *PostgresStore) ListManagedAdmins(ctx context.Context) ([]ManagedAdmin, error) {
	rows, err := s.pool.Query(ctx, `
		select `+managedAdminColumns+`
		from admins a
		left join users u on u.id = a.user_id
		order by a.role desc, a.created_at, a.username
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]ManagedAdmin, 0, 8)
	for rows.Next() {
		entry, err := scanManagedAdmin(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *PostgresStore) GetManagedAdmin(ctx context.Context, adminID string) (ManagedAdmin, error) {
	entry, err := scanManagedAdmin(s.pool.QueryRow(ctx, `
		select `+managedAdminColumns+`
		from admins a
		left join users u on u.id = a.user_id
		where a.id = $1::uuid
	`, adminID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedAdmin{}, ErrNotFound
	}
	return entry, err
}

func (s *PostgresStore) AppointAdmin(ctx context.Context, input AppointAdminInput, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return ManagedAdmin{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ManagedAdmin{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The target must be an existing, active user.
	var userExists bool
	if err := tx.QueryRow(ctx, `
		select exists(select 1 from users where id = $1::uuid and status = 'active')
	`, input.UserID).Scan(&userExists); err != nil {
		return ManagedAdmin{}, err
	}
	if !userExists {
		return ManagedAdmin{}, ErrUserNotFound
	}

	// One admin account per user, but a soft-revoked link is reactivated in
	// place (same account, original username) rather than duplicated.
	var linkedID, linkedStatus string
	err = tx.QueryRow(ctx, `
		select id::text, status from admins where user_id = $1::uuid for update
	`, input.UserID).Scan(&linkedID, &linkedStatus)
	switch {
	case err == nil && linkedStatus != "revoked":
		return ManagedAdmin{}, ErrUserAlreadyAdmin
	case err == nil:
		if _, err := tx.Exec(ctx, `
			update admins
			set status = 'active', password_hash = $2, must_change_password = true,
				revoked_at = null, revoked_by = null, updated_at = now()
			where id = $1::uuid and role = 'admin' and status = 'revoked'
		`, linkedID, input.PasswordHash); err != nil {
			return ManagedAdmin{}, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `
			insert into admins (username, password_hash, display_name, role, status, user_id, must_change_password)
			values ($1, $2, $3, 'admin', 'active', $4::uuid, true)
			returning id::text
		`, input.Username, input.PasswordHash, input.DisplayName, input.UserID).Scan(&linkedID); err != nil {
			if isUniqueViolation(err, "admins_username_normalized_unique") || isUniqueViolation(err, "admins_username_key") {
				return ManagedAdmin{}, ErrUsernameTaken
			}
			if isUniqueViolation(err, "admins_user_id_key") {
				return ManagedAdmin{}, ErrUserAlreadyAdmin
			}
			return ManagedAdmin{}, err
		}
	default:
		return ManagedAdmin{}, err
	}

	return s.finishManagementTx(ctx, tx, linkedID, event)
}

func (s *PostgresStore) SetManagedAdminStatus(ctx context.Context, adminID string, status string, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	if status != "active" && status != "disabled" {
		return ManagedAdmin{}, ErrInvalidTransition
	}
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return ManagedAdmin{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ManagedAdmin{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.lockManagedTarget(ctx, tx, adminID); err != nil {
		return ManagedAdmin{}, err
	}
	tag, err := tx.Exec(ctx, `
		update admins set status = $2, updated_at = now()
		where id = $1::uuid and role = 'admin' and status <> $2 and status <> 'revoked'
	`, adminID, status)
	if err != nil {
		return ManagedAdmin{}, err
	}
	if tag.RowsAffected() == 0 {
		return ManagedAdmin{}, ErrInvalidTransition
	}
	return s.finishManagementTx(ctx, tx, adminID, event)
}

func (s *PostgresStore) RevokeManagedAdmin(ctx context.Context, adminID string, actorID string, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return ManagedAdmin{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ManagedAdmin{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.lockManagedTarget(ctx, tx, adminID); err != nil {
		return ManagedAdmin{}, err
	}
	tag, err := tx.Exec(ctx, `
		update admins
		set status = 'revoked', revoked_at = now(), revoked_by = $2::uuid, updated_at = now()
		where id = $1::uuid and role = 'admin' and status <> 'revoked'
	`, adminID, actorID)
	if err != nil {
		return ManagedAdmin{}, err
	}
	if tag.RowsAffected() == 0 {
		return ManagedAdmin{}, ErrInvalidTransition
	}
	return s.finishManagementTx(ctx, tx, adminID, event)
}

func (s *PostgresStore) ResetManagedAdminPassword(ctx context.Context, adminID string, newHash string, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	if err := validateAdminAuthAuditEvent(event); err != nil {
		return ManagedAdmin{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ManagedAdmin{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.lockManagedTarget(ctx, tx, adminID); err != nil {
		return ManagedAdmin{}, err
	}
	tag, err := tx.Exec(ctx, `
		update admins
		set password_hash = $2, must_change_password = true, updated_at = now()
		where id = $1::uuid and role = 'admin' and status <> 'revoked'
	`, adminID, newHash)
	if err != nil {
		return ManagedAdmin{}, err
	}
	if tag.RowsAffected() == 0 {
		return ManagedAdmin{}, ErrInvalidTransition
	}
	return s.finishManagementTx(ctx, tx, adminID, event)
}

func (s *PostgresStore) ListManagedAdminAudit(ctx context.Context, adminID string, limit int) ([]AdminAuthAuditSummaryEntry, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		select event_type, occurred_at, client_ip, result, reason_code
		from admin_auth_audit_events
		where admin_id = $1::uuid or actor_admin_id = $1::uuid
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

// lockManagedTarget locks the target row and rejects the owner on every
// management path. This role check is the storage-level wall the requirements
// demand on top of the HTTP-layer RequireOwner + handler checks; status
// transitions are guarded by each caller's UPDATE predicate.
func (s *PostgresStore) lockManagedTarget(ctx context.Context, tx pgx.Tx, adminID string) error {
	var role string
	err := tx.QueryRow(ctx, `
		select role from admins where id = $1::uuid for update
	`, adminID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == "owner" {
		return ErrTargetIsOwner
	}
	return nil
}

// finishManagementTx purges the target's sessions, writes the audit event,
// commits, and returns the fresh row — the shared tail of every mutation.
func (s *PostgresStore) finishManagementTx(ctx context.Context, tx pgx.Tx, adminID string, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	if _, err := tx.Exec(ctx, `delete from admin_sessions where admin_id = $1::uuid`, adminID); err != nil {
		return ManagedAdmin{}, err
	}
	id := adminID
	event.AdminID = &id
	if err := insertAuditEventTx(ctx, tx, event); err != nil {
		return ManagedAdmin{}, err
	}
	entry, err := scanManagedAdmin(tx.QueryRow(ctx, `
		select `+managedAdminColumns+`
		from admins a
		left join users u on u.id = a.user_id
		where a.id = $1::uuid
	`, adminID))
	if err != nil {
		return ManagedAdmin{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedAdmin{}, err
	}
	return entry, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
