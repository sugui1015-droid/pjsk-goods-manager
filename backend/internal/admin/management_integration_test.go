package admin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/testdb"
)

// Integration coverage for the 0023 admin management storage: appointment,
// linkage, soft revocation with reactivation, session purges, owner
// protection, and the audit trail — against a real migrated database. Gated
// by PJSK_RUN_DB_INTEGRATION_TESTS=1 like every other integration test.

func managementEvent(eventType AdminAuthEventType, actorID string, targetUsername string, reason string) AdminAuthAuditEvent {
	actor := actorID
	event := AdminAuthAuditEvent{
		EventType:          eventType,
		OccurredAt:         time.Now(),
		UsernameNormalized: targetUsername,
		ClientIP:           "integration-test",
		Result:             AdminAuthResultSuccess,
		ReasonCode:         AdminAuthReasonNone,
		ActorAdminID:       &actor,
	}
	if reason != "" {
		event.ManagementReason = &reason
	}
	return event
}

func createUser(t *testing.T, ctx context.Context, store *PostgresStore, cn string) string {
	t.Helper()
	var id string
	if err := store.pool.QueryRow(ctx, `
		insert into users (cn_code, status) values ($1, 'active') returning id::text
	`, cn).Scan(&id); err != nil {
		t.Fatalf("create user %s: %v", cn, err)
	}
	return id
}

func createSession(t *testing.T, ctx context.Context, store *PostgresStore, adminID string, tokenHash string) {
	t.Helper()
	if _, err := store.pool.Exec(ctx, `
		insert into admin_sessions (admin_id, token_hash, expires_at)
		values ($1::uuid, $2, now() + interval '1 hour')
	`, adminID, tokenHash); err != nil {
		t.Fatalf("create session: %v", err)
	}
}

func countSessions(t *testing.T, ctx context.Context, store *PostgresStore, adminID string) int {
	t.Helper()
	var count int
	if err := store.pool.QueryRow(ctx, `
		select count(*) from admin_sessions where admin_id = $1::uuid
	`, adminID).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return count
}

func TestAdminManagementLifecycle(t *testing.T) {
	pool := testdb.New(t, "adminmgmt")
	store := NewPostgresStore(pool)
	ctx := context.Background()

	ownerID := createAdmin(t, ctx, store, "boss", "admin", "active")
	if err := store.PromoteOwner(ctx, ownerID, integrationEvent(AdminAuthEventOwnerPromoted, ownerID)); err != nil {
		t.Fatalf("promote owner: %v", err)
	}
	userID := createUser(t, ctx, store, "helper_cn")

	// --- appoint ---
	appointed, err := store.AppointAdmin(ctx, AppointAdminInput{
		UserID:       userID,
		Username:     "helper_admin",
		PasswordHash: "x-temp-hash-1",
	}, managementEvent(AdminAuthEventAdminAppointed, ownerID, "helper_admin", ""))
	if err != nil {
		t.Fatalf("appoint: %v", err)
	}
	if appointed.Role != "admin" || appointed.Status != "active" || !appointed.MustChangePassword {
		t.Fatalf("unexpected appointed row: %+v", appointed)
	}
	if appointed.UserID == nil || *appointed.UserID != userID || appointed.UserCN == nil || *appointed.UserCN != "helper_cn" {
		t.Fatalf("user linkage missing: %+v", appointed)
	}

	// Appointing the same user again must be refused while the account lives.
	if _, err := store.AppointAdmin(ctx, AppointAdminInput{
		UserID: userID, Username: "helper_admin_2", PasswordHash: "x",
	}, managementEvent(AdminAuthEventAdminAppointed, ownerID, "helper_admin_2", "")); err != ErrUserAlreadyAdmin {
		t.Fatalf("expected ErrUserAlreadyAdmin, got %v", err)
	}

	// A second user cannot take an existing username.
	otherUserID := createUser(t, ctx, store, "other_cn")
	if _, err := store.AppointAdmin(ctx, AppointAdminInput{
		UserID: otherUserID, Username: "helper_admin", PasswordHash: "x",
	}, managementEvent(AdminAuthEventAdminAppointed, ownerID, "helper_admin", "")); err != ErrUsernameTaken {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}

	// A missing user is refused.
	if _, err := store.AppointAdmin(ctx, AppointAdminInput{
		UserID: "5c4dbb10-0000-0000-0000-0000000000ff", Username: "ghost_admin", PasswordHash: "x",
	}, managementEvent(AdminAuthEventAdminAppointed, ownerID, "ghost_admin", "")); err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	// --- owner protection on every mutation ---
	if _, err := store.SetManagedAdminStatus(ctx, ownerID, "disabled",
		managementEvent(AdminAuthEventAdminDisabled, ownerID, "boss", "")); err != ErrTargetIsOwner {
		t.Fatalf("disable owner: expected ErrTargetIsOwner, got %v", err)
	}
	if _, err := store.RevokeManagedAdmin(ctx, ownerID, ownerID,
		managementEvent(AdminAuthEventAdminRevoked, ownerID, "boss", "")); err != ErrTargetIsOwner {
		t.Fatalf("revoke owner: expected ErrTargetIsOwner, got %v", err)
	}
	if _, err := store.ResetManagedAdminPassword(ctx, ownerID, "x",
		managementEvent(AdminAuthEventAdminPasswordResetByOwner, ownerID, "boss", "")); err != ErrTargetIsOwner {
		t.Fatalf("reset owner: expected ErrTargetIsOwner, got %v", err)
	}

	// --- disable purges sessions ---
	createSession(t, ctx, store, appointed.ID, "hash-live-1")
	createSession(t, ctx, store, appointed.ID, "hash-live-2")
	disabled, err := store.SetManagedAdminStatus(ctx, appointed.ID, "disabled",
		managementEvent(AdminAuthEventAdminDisabled, ownerID, "helper_admin", "轮岗"))
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.Status != "disabled" || countSessions(t, ctx, store, appointed.ID) != 0 {
		t.Fatalf("disable must set status and purge sessions: %+v", disabled)
	}

	// Redundant transition is refused.
	if _, err := store.SetManagedAdminStatus(ctx, appointed.ID, "disabled",
		managementEvent(AdminAuthEventAdminDisabled, ownerID, "helper_admin", "")); err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}

	// --- enable ---
	enabled, err := store.SetManagedAdminStatus(ctx, appointed.ID, "active",
		managementEvent(AdminAuthEventAdminEnabled, ownerID, "helper_admin", ""))
	if err != nil || enabled.Status != "active" {
		t.Fatalf("enable: %v %+v", err, enabled)
	}

	// --- owner password reset flags the forced change and purges sessions ---
	createSession(t, ctx, store, appointed.ID, "hash-live-3")
	reset, err := store.ResetManagedAdminPassword(ctx, appointed.ID, "x-temp-hash-2",
		managementEvent(AdminAuthEventAdminPasswordResetByOwner, ownerID, "helper_admin", ""))
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if !reset.MustChangePassword || countSessions(t, ctx, store, appointed.ID) != 0 {
		t.Fatal("reset must set must_change_password and purge sessions")
	}

	// The regular password change clears the flag.
	createSession(t, ctx, store, appointed.ID, "hash-keep")
	if err := store.UpdatePasswordKeepSession(ctx, appointed.ID, "x-final-hash", "hash-keep", time.Now(),
		integrationEvent(AdminAuthEventPasswordChanged, appointed.ID)); err != nil {
		t.Fatalf("change password: %v", err)
	}
	account, err := store.FindByUsername(ctx, "helper_admin")
	if err != nil || account.MustChangePassword {
		t.Fatalf("password change must clear must_change_password: %v %+v", err, account)
	}

	// --- revoke is soft and purges sessions ---
	createSession(t, ctx, store, appointed.ID, "hash-live-4")
	revoked, err := store.RevokeManagedAdmin(ctx, appointed.ID, ownerID,
		managementEvent(AdminAuthEventAdminRevoked, ownerID, "helper_admin", "不再需要"))
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.Status != "revoked" || revoked.RevokedAt == nil || countSessions(t, ctx, store, appointed.ID) != 0 {
		t.Fatalf("revoke must be soft with purged sessions: %+v", revoked)
	}
	// The linked user row is untouched by the whole lifecycle.
	var userStatus string
	if err := store.pool.QueryRow(ctx, `select status from users where id = $1::uuid`, userID).Scan(&userStatus); err != nil || userStatus != "active" {
		t.Fatalf("user must stay active: %v %q", err, userStatus)
	}

	// A revoked account can never resolve a session (status wall).
	createSession(t, ctx, store, appointed.ID, "hash-after-revoke")
	if _, err := store.FindBySession(ctx, "hash-after-revoke"); err != ErrNotFound {
		t.Fatalf("revoked admin session must not resolve, got %v", err)
	}

	// --- re-appointment reactivates the same account ---
	reappointed, err := store.AppointAdmin(ctx, AppointAdminInput{
		UserID:       userID,
		Username:     "ignored_new_name",
		PasswordHash: "x-temp-hash-3",
	}, managementEvent(AdminAuthEventAdminAppointed, ownerID, "helper_admin", ""))
	if err != nil {
		t.Fatalf("re-appoint: %v", err)
	}
	if reappointed.ID != appointed.ID || reappointed.Username != "helper_admin" {
		t.Fatalf("re-appointment must reactivate the same account: %+v", reappointed)
	}
	if reappointed.Status != "active" || !reappointed.MustChangePassword || reappointed.RevokedAt != nil {
		t.Fatalf("reactivated account state wrong: %+v", reappointed)
	}

	// --- audit trail: every management event carries the actor ---
	var managementEvents, withActor int
	if err := store.pool.QueryRow(ctx, `
		select count(*),
			count(*) filter (where actor_admin_id is not null)
		from admin_auth_audit_events
		where event_type in ('admin_appointed','admin_revoked','admin_enabled','admin_disabled','admin_password_reset_by_owner')
	`).Scan(&managementEvents, &withActor); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if managementEvents == 0 || managementEvents != withActor {
		t.Fatalf("management audit incomplete: %d events, %d with actor", managementEvents, withActor)
	}
	var reasons int
	if err := store.pool.QueryRow(ctx, `
		select count(*) from admin_auth_audit_events where management_reason is not null
	`).Scan(&reasons); err != nil || reasons < 2 {
		t.Fatalf("management reasons missing: %v %d", err, reasons)
	}
}

// TestMigration0023Idempotent re-executes the 0023 statements against an
// already-migrated database to prove the file is safely re-runnable.
func TestMigration0023Idempotent(t *testing.T) {
	pool := testdb.New(t, "mig0023")
	ctx := context.Background()

	sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0023_admin_management.sql"))
	if err != nil {
		t.Fatalf("read 0023: %v", err)
	}
	if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
		t.Fatalf("re-running 0023 must be idempotent: %v", err)
	}

	// The status vocabulary accepts 'revoked' and still rejects garbage.
	if _, err := pool.Exec(ctx, `
		insert into admins (username, password_hash, status) values ('revoked_probe', 'x', 'revoked')
	`); err != nil {
		t.Fatalf("status 'revoked' must be accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into admins (username, password_hash, status) values ('bad_probe', 'x', 'sleeping')
	`); err == nil || !strings.Contains(err.Error(), "admins_status_check") {
		t.Fatalf("invalid status must be rejected by admins_status_check, got %v", err)
	}
}
