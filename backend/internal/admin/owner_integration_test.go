package admin

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/testdb"
)

// These tests exercise the 0022 owner guarantees against a real throwaway
// PostgreSQL database built from the repository migrations. They are gated by
// PJSK_RUN_DB_INTEGRATION_TESTS=1 exactly like every other integration test.

func integrationEvent(eventType AdminAuthEventType, adminID string) AdminAuthAuditEvent {
	id := adminID
	return AdminAuthAuditEvent{
		EventType:          eventType,
		OccurredAt:         time.Now(),
		AdminID:            &id,
		UsernameNormalized: "integration",
		ClientIP:           "server-cli",
		Result:             AdminAuthResultSuccess,
		ReasonCode:         AdminAuthReasonNone,
	}
}

func createAdmin(t *testing.T, ctx context.Context, store *PostgresStore, username string, role string, status string) string {
	t.Helper()
	var id string
	err := store.pool.QueryRow(ctx, `
		insert into admins (username, password_hash, role, status)
		values ($1, 'x-not-a-real-hash', $2, $3)
		returning id::text
	`, username, role, status).Scan(&id)
	if err != nil {
		t.Fatalf("create admin %s: %v", username, err)
	}
	return id
}

func TestOwnerDatabaseGuarantees(t *testing.T) {
	pool := testdb.New(t, "adminowner")
	store := NewPostgresStore(pool)
	ctx := context.Background()

	firstID := createAdmin(t, ctx, store, "first_admin", "admin", "active")
	secondID := createAdmin(t, ctx, store, "second_admin", "admin", "active")

	// 0022 applied and visible to the CLI guard.
	applied, err := store.SchemaMigrationApplied(ctx, "0022_admin_owner_security.sql")
	if err != nil || !applied {
		t.Fatalf("0022 must be applied: %v / %v", applied, err)
	}

	// Bootstrap promotion works exactly once while zero owners exist.
	if err := store.PromoteOwner(ctx, firstID, integrationEvent(AdminAuthEventOwnerPromoted, firstID)); err != nil {
		t.Fatalf("bootstrap promotion failed: %v", err)
	}
	if err := store.PromoteOwner(ctx, secondID, integrationEvent(AdminAuthEventOwnerPromoted, secondID)); err == nil {
		t.Fatal("second promotion must be rejected while an owner exists")
	}

	// The partial unique index blocks a second owner even by direct SQL.
	if _, err := pool.Exec(ctx, `update admins set role = 'owner' where id = $1::uuid`, secondID); err == nil {
		t.Fatal("unique owner index must reject a second owner")
	} else if !strings.Contains(err.Error(), "admins_single_owner_unique") {
		t.Fatalf("expected unique index violation, got %v", err)
	}

	// The deferred constraint trigger blocks losing the last active owner…
	if _, err := pool.Exec(ctx, `update admins set role = 'admin' where id = $1::uuid`, firstID); err == nil {
		t.Fatal("demoting the last active owner must fail at commit")
	}
	if _, err := pool.Exec(ctx, `update admins set status = 'disabled' where id = $1::uuid`, firstID); err == nil {
		t.Fatal("disabling the last active owner must fail at commit")
	}
	if _, err := pool.Exec(ctx, `delete from admins where id = $1::uuid`, firstID); err == nil {
		t.Fatal("deleting the last active owner must fail at commit")
	}

	// …but an atomic transfer (demote old, promote new, one transaction)
	// commits cleanly with no transient 0-owner or 2-owner state visible.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update admins set role = 'admin' where id = $1::uuid`, firstID); err != nil {
		t.Fatalf("transfer demote step: %v", err)
	}
	if _, err := tx.Exec(ctx, `update admins set role = 'owner' where id = $1::uuid`, secondID); err != nil {
		t.Fatalf("transfer promote step: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("owner transfer must commit in one transaction: %v", err)
	}
	owner, err := store.FindOwner(ctx)
	if err != nil || owner.ID != secondID {
		t.Fatalf("owner after transfer: %+v / %v", owner, err)
	}
}

func TestRecoveryCodeLifecycleIntegration(t *testing.T) {
	pool := testdb.New(t, "adminrecovery")
	store := NewPostgresStore(pool)
	ctx := context.Background()

	adminID := createAdmin(t, ctx, store, "recovery_admin", "admin", "active")
	key := bytes.Repeat([]byte{0x51}, 32)

	firstBatch := [][]byte{
		hashRecoveryCode(key, strings.Repeat("A", 20)),
		hashRecoveryCode(key, strings.Repeat("B", 20)),
	}
	if err := store.ReplaceRecoveryCodes(ctx, adminID, "0e0f6a52-8b52-4c0e-9dbb-111111111111", firstBatch, integrationEvent(AdminAuthEventRecoveryCodesGenerated, adminID)); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	status, err := store.RecoveryCodeStatus(ctx, adminID)
	if err != nil || status.RemainingCodes != 2 {
		t.Fatalf("expected 2 live codes, got %+v / %v", status, err)
	}

	// Insert sessions, then reset with a valid code: code consumed once,
	// sessions all revoked, audit written atomically.
	if _, err := pool.Exec(ctx, `
		insert into admin_sessions (admin_id, token_hash, expires_at, reauth_at)
		values ($1::uuid, repeat('a', 64), now() + interval '1 hour', now()),
			($1::uuid, repeat('b', 64), now() + interval '1 hour', null)
	`, adminID); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetPasswordWithRecoveryCode(ctx, adminID, firstBatch[0], "new-hash", integrationEvent(AdminAuthEventRecoveryCodeResetSucceeded, adminID)); err != nil {
		t.Fatalf("reset with valid code: %v", err)
	}
	if err := store.ResetPasswordWithRecoveryCode(ctx, adminID, firstBatch[0], "new-hash-2", integrationEvent(AdminAuthEventRecoveryCodeResetSucceeded, adminID)); err != ErrNoUsableCode {
		t.Fatalf("used code must be unusable, got %v", err)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `select count(*) from admin_sessions where admin_id = $1::uuid`, adminID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("all sessions must be revoked after recovery, %d remain", sessions)
	}

	// Regeneration revokes the remaining old-batch code.
	secondBatch := [][]byte{hashRecoveryCode(key, strings.Repeat("C", 20))}
	if err := store.ReplaceRecoveryCodes(ctx, adminID, "0e0f6a52-8b52-4c0e-9dbb-222222222222", secondBatch, integrationEvent(AdminAuthEventRecoveryCodesGenerated, adminID)); err != nil {
		t.Fatalf("second batch: %v", err)
	}
	if err := store.ResetPasswordWithRecoveryCode(ctx, adminID, firstBatch[1], "new-hash-3", integrationEvent(AdminAuthEventRecoveryCodeResetSucceeded, adminID)); err != ErrNoUsableCode {
		t.Fatalf("old-batch code must be revoked, got %v", err)
	}
	status, err = store.RecoveryCodeStatus(ctx, adminID)
	if err != nil || status.RemainingCodes != 1 {
		t.Fatalf("expected 1 live code after regeneration, got %+v / %v", status, err)
	}

	// Audit rows for the lifecycle exist.
	var audits int
	if err := pool.QueryRow(ctx, `
		select count(*) from admin_auth_audit_events
		where admin_id = $1::uuid and event_type in ('admin_recovery_codes_generated', 'admin_recovery_code_reset_succeeded')
	`, adminID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 3 {
		t.Fatalf("expected 3 audit rows (2 generations + 1 reset), got %d", audits)
	}
}

func TestSessionReauthRoundTripIntegration(t *testing.T) {
	pool := testdb.New(t, "adminreauth")
	store := NewPostgresStore(pool)
	ctx := context.Background()

	adminID := createAdmin(t, ctx, store, "reauth_admin", "admin", "active")
	tokenHash := strings.Repeat("c", 64)
	if _, err := pool.Exec(ctx, `
		insert into admin_sessions (admin_id, token_hash, expires_at)
		values ($1::uuid, $2, now() + interval '1 hour')
	`, adminID, tokenHash); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := store.SessionReauthAt(ctx, tokenHash); err != nil || ok {
		t.Fatalf("fresh session must have no reauth: %v / %v", ok, err)
	}
	stamp := time.Now().UTC().Truncate(time.Millisecond)
	if err := store.SetSessionReauth(ctx, tokenHash, stamp, integrationEvent(AdminAuthEventReauthSucceeded, adminID)); err != nil {
		t.Fatalf("set reauth: %v", err)
	}
	at, ok, err := store.SessionReauthAt(ctx, tokenHash)
	if err != nil || !ok {
		t.Fatalf("reauth must be readable: %v / %v", ok, err)
	}
	if at.Sub(stamp).Abs() > time.Second {
		t.Fatalf("reauth timestamp drifted: %v vs %v", at, stamp)
	}
	if err := store.SetSessionReauth(ctx, strings.Repeat("d", 64), stamp, integrationEvent(AdminAuthEventReauthSucceeded, adminID)); err != ErrNotFound {
		t.Fatalf("unknown session must be ErrNotFound, got %v", err)
	}
}
