package users

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pjsk/backend/internal/recoveryemail"
)

func TestPostgresRecoveryEmailAdminLifecycle(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "TEST_RECOVERY_EMAIL_" + time.Now().Format("20060102150405.000000000")
	cleanupRecoveryEmailFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupRecoveryEmailFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertBindTokenAdmin(t, pool, prefix)
	firstUserID := insertQueryAccountUser(t, pool, prefix+"_FIRST", "")
	secondUserID := insertQueryAccountUser(t, pool, prefix+"_SECOND", "")
	disabledUserID := insertQueryAccountUser(t, pool, prefix+"_DISABLED", "")
	mergedUserID := insertQueryAccountUser(t, pool, prefix+"_MERGED", "")
	store := NewPostgresStore(pool)
	protector := testRecoveryProtector(t)

	shared, err := protector.Protect("Shared.Account@EXAMPLE.COM")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.PutRecoveryEmail(ctx, firstUserID, adminID, "initial registration", shared)
	if err != nil || created.Action != "recovery_email_created" || created.Record.Status != "pending" || created.Record.VerifiedAt != "" {
		t.Fatalf("create result = action %q status %q verified %t err %v", created.Action, created.Record.Status, created.Record.VerifiedAt != "", err)
	}
	assertRecoveryEmailStorage(t, pool, firstUserID, shared, 1, 1)

	// The same normalized email may belong to another CN.
	if _, err := store.PutRecoveryEmail(ctx, secondUserID, adminID, "shared family email", shared); err != nil {
		t.Fatalf("same email for another user: %v", err)
	}
	if countRecoveryRows(t, pool, `select count(*)::int from user_recovery_emails where email_lookup_hash = $1`, shared.LookupHash) != 2 {
		t.Fatal("same email was not allowed for two users")
	}

	// Repeating the same email is idempotent: no history or audit row is added.
	unchanged, err := store.PutRecoveryEmail(ctx, firstUserID, adminID, "repeat request", shared)
	if err != nil || unchanged.Action != "" {
		t.Fatalf("idempotent put action = %q, err = %v", unchanged.Action, err)
	}
	assertRecoveryEmailStorage(t, pool, firstUserID, shared, 1, 1)

	replacement, err := protector.Protect("Replacement@EXAMPLE.ORG")
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := store.PutRecoveryEmail(ctx, firstUserID, adminID, "replace destination", replacement)
	if err != nil || replaced.Action != "recovery_email_replaced" || replaced.Record.Status != "pending" {
		t.Fatalf("replace action/status/error = %q/%q/%v", replaced.Action, replaced.Record.Status, err)
	}
	assertRecoveryEmailStorage(t, pool, firstUserID, replacement, 2, 2)
	if countRecoveryRows(t, pool, `select count(*)::int from user_recovery_emails where user_id = $1::uuid and invalidated_at is null`, firstUserID) != 1 {
		t.Fatal("replacement created more than one current email")
	}

	// A failure after the old row is selected/updated must roll the transaction back.
	broken := recoveryemail.Protected{LookupHash: strings.Repeat("a", 64), Masked: "x***@example.org"}
	if _, err := store.PutRecoveryEmail(ctx, firstUserID, adminID, "forced storage failure", broken); err == nil {
		t.Fatal("invalid encrypted payload unexpectedly succeeded")
	}
	assertRecoveryEmailStorage(t, pool, firstUserID, replacement, 2, 2)

	if _, err := pool.Exec(ctx, `update users set status = 'disabled' where id = $1::uuid`, disabledUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutRecoveryEmail(ctx, disabledUserID, adminID, "prepare before re-enable", shared); err != nil {
		t.Fatalf("disabled user admin maintenance failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `update users set status = 'merged' where id = $1::uuid`, mergedUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutRecoveryEmail(ctx, mergedUserID, adminID, "must reject", shared); !errors.Is(err, ErrRecoveryEmailUserMerged) {
		t.Fatalf("merged user error = %v", err)
	}

	changed, err := store.UnbindRecoveryEmail(ctx, firstUserID, adminID, "user requested removal")
	if err != nil || !changed {
		t.Fatalf("unbind = %t, %v", changed, err)
	}
	record, err := store.GetRecoveryEmail(ctx, firstUserID)
	if err != nil || record.HasRecoveryEmail {
		t.Fatalf("record after unbind = %+v, %v", record, err)
	}
	if countRecoveryRows(t, pool, `select count(*)::int from account_security_audit_logs where target_user_id = $1::uuid`, firstUserID) != 3 {
		t.Fatal("unbind audit row missing")
	}
	changed, err = store.UnbindRecoveryEmail(ctx, firstUserID, adminID, "repeat removal")
	if err != nil || changed {
		t.Fatalf("idempotent unbind = %t, %v", changed, err)
	}
}

func TestPostgresRecoveryEmailConcurrentReplacementKeepsOneCurrent(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "TEST_RECOVERY_EMAIL_CONCURRENT_" + time.Now().Format("20060102150405.000000000")
	cleanupRecoveryEmailFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupRecoveryEmailFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertBindTokenAdmin(t, pool, prefix)
	userID := insertQueryAccountUser(t, pool, prefix+"_USER", "")
	store := NewPostgresStore(pool)
	protector := testRecoveryProtector(t)
	initial, err := protector.Protect("Initial@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutRecoveryEmail(ctx, userID, adminID, "initial", initial); err != nil {
		t.Fatal(err)
	}

	inputs := []string{"Concurrent.One@example.org", "Concurrent.Two@example.org"}
	var wait sync.WaitGroup
	errorsChannel := make(chan error, len(inputs))
	for _, input := range inputs {
		protected, err := protector.Protect(input)
		if err != nil {
			t.Fatal(err)
		}
		wait.Add(1)
		go func(value recoveryemail.Protected) {
			defer wait.Done()
			_, err := store.PutRecoveryEmail(ctx, userID, adminID, "concurrent replacement", value)
			errorsChannel <- err
		}(protected)
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent replacement failed: %v", err)
		}
	}
	if countRecoveryRows(t, pool, `select count(*)::int from user_recovery_emails where user_id = $1::uuid and invalidated_at is null`, userID) != 1 {
		t.Fatal("concurrent replacement left multiple current emails")
	}
}

func assertRecoveryEmailStorage(t *testing.T, pool *pgxpool.Pool, userID string, protected recoveryemail.Protected, wantHistory int, wantAudit int) {
	t.Helper()
	var encrypted []byte
	var lookupHash, status string
	var verified bool
	if err := pool.QueryRow(context.Background(), `
		select encrypted_email, email_lookup_hash, status, verified_at is not null
		from user_recovery_emails
		where user_id = $1::uuid and invalidated_at is null
	`, userID).Scan(&encrypted, &lookupHash, &status, &verified); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("@example.")) || bytes.Equal(encrypted, protected.Encrypted[:0]) {
		t.Fatal("database email field contains unexpected plaintext")
	}
	if lookupHash != protected.LookupHash || status != "pending" || verified {
		t.Fatalf("stored state mismatch: hash=%t status=%q verified=%t", lookupHash == protected.LookupHash, status, verified)
	}
	if countRecoveryRows(t, pool, `select count(*)::int from user_recovery_emails where user_id = $1::uuid`, userID) != wantHistory {
		t.Fatalf("history count does not equal %d", wantHistory)
	}
	if countRecoveryRows(t, pool, `select count(*)::int from account_security_audit_logs where target_user_id = $1::uuid`, userID) != wantAudit {
		t.Fatalf("audit count does not equal %d", wantAudit)
	}
	var unsafeAuditCount int
	if err := pool.QueryRow(context.Background(), `
		select count(*)::int from account_security_audit_logs
		where target_user_id = $1::uuid
		  and (metadata::text ilike '%encrypted_email%' or metadata::text ilike '%lookup_hash%')
	`, userID).Scan(&unsafeAuditCount); err != nil {
		t.Fatal(err)
	}
	if unsafeAuditCount != 0 {
		t.Fatal("audit metadata contains forbidden field names")
	}
}

func countRecoveryRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func cleanupRecoveryEmailFixture(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	pattern := prefix + "%"
	var orderCount, paymentCount, mergeCount int
	if err := pool.QueryRow(ctx, `select count(*)::int from orders where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from payments where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		select count(*)::int from cn_merge_logs
		where source_user_id in (select id from users where cn_code like $1)
		   or target_user_id in (select id from users where cn_code like $1)
	`, pattern).Scan(&mergeCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 || paymentCount != 0 || mergeCount != 0 {
		t.Fatalf("refusing fixture cleanup: orders=%d payments=%d merges=%d", orderCount, paymentCount, mergeCount)
	}

	statements := []string{
		`delete from account_security_audit_logs where target_user_id in (select id from users where cn_code like $1)`,
		`delete from user_recovery_emails where user_id in (select id from users where cn_code like $1)`,
		`delete from query_sessions where user_id in (select id from users where cn_code like $1)`,
		`delete from users where cn_code like $1`,
		`delete from admin_sessions where admin_id in (select id from admins where username like $1)`,
		`delete from admins where username like $1`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, pattern); err != nil {
			t.Fatalf("cleanup recovery email fixture: %v", err)
		}
	}
	for label, query := range map[string]string{
		"users":    `select count(*)::int from users where cn_code like $1`,
		"emails":   `select count(*)::int from user_recovery_emails where user_id in (select id from users where cn_code like $1)`,
		"audits":   `select count(*)::int from account_security_audit_logs where target_user_id in (select id from users where cn_code like $1)`,
		"sessions": `select count(*)::int from query_sessions where user_id in (select id from users where cn_code like $1)`,
	} {
		if countRecoveryRows(t, pool, query, pattern) != 0 {
			t.Fatalf("%s fixture rows remain", label)
		}
	}
}
