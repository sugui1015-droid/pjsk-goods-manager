package users

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	queryapi "pjsk/backend/internal/query"
	"pjsk/backend/internal/querycode"
)

// TestPostgresBulkBindTokenBatch covers the batch against a real database:
// which users it selects, that the issued codes actually bind, that each
// user's earlier unused code dies, and — the property that must not regress —
// that a user who becomes ineligible mid-batch is reported as skipped with a
// reason rather than quietly dropped from the file.
func TestPostgresBulkBindTokenBatch(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "BULK_BIND_TEST_" + time.Now().Format("20060102150405")
	cleanupBindTokenFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupBindTokenFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertBindTokenAdmin(t, pool, prefix)
	store := NewPostgresStore(pool)

	// Four users, only two of which a bind code can ever be valid for.
	eligibleA := insertQueryAccountUser(t, pool, prefix+"_A", "")
	eligibleB := insertQueryAccountUser(t, pool, prefix+"_B", "")
	insertQueryAccountUser(t, pool, prefix+"_C", "existing-code")
	disabled := insertQueryAccountUser(t, pool, prefix+"_D", "")
	if _, err := pool.Exec(ctx, `update users set status = 'disabled' where id = $1::uuid`, disabled); err != nil {
		t.Fatalf("disable fixture user: %v", err)
	}

	// Give eligibleA an older unused code, so the batch has one to invalidate.
	oldToken, err := store.CreateQueryCodeBindToken(ctx, eligibleA, adminID)
	if err != nil {
		t.Fatalf("seed old bind token: %v", err)
	}

	filters := parseFilters(t, "cn="+prefix+"_A&cn="+prefix+"_B&cn="+prefix+"_C&cn="+prefix+"_D")

	// The preview must see exactly the two eligible users, and must notice that
	// one of them holds a still-valid older code.
	preview, err := store.PreviewBulkQueryCodeBindTokens(ctx, filters)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.EligibleCount != 2 {
		t.Fatalf("preview eligible = %d, want 2 (users with a query code and disabled users must be excluded)", preview.EligibleCount)
	}
	if preview.ExistingUnusedCount != 1 {
		t.Fatalf("preview existing unused = %d, want 1", preview.ExistingUnusedCount)
	}
	if preview.ValidDays != 7 {
		t.Fatalf("preview valid days = %d, want 7", preview.ValidDays)
	}

	result, err := store.BulkCreateQueryCodeBindTokens(ctx, filters, adminID)
	if err != nil {
		t.Fatalf("bulk create: %v", err)
	}
	if result.Requested != 2 || len(result.Issued) != 2 || len(result.Skipped) != 0 {
		t.Fatalf("result = requested %d / issued %d / skipped %d, want 2/2/0", result.Requested, len(result.Issued), len(result.Skipped))
	}
	if len(result.Issued)+len(result.Skipped) != result.Requested {
		t.Fatal("issued + skipped must always equal requested")
	}

	byCN := map[string]BulkBindToken{}
	for _, item := range result.Issued {
		if item.BindToken == "" || len(item.BindToken) != querycode.BindTokenLength {
			t.Fatalf("issued token for %s has wrong shape", item.CNCode)
		}
		byCN[item.CNCode] = item
	}
	if _, ok := byCN[prefix+"_A"]; !ok {
		t.Fatal("eligible user A missing from batch")
	}
	if !byCN[prefix+"_A"].ReplacedUnused {
		t.Fatal("user A held an unused code; the batch must report it was replaced")
	}
	if byCN[prefix+"_B"].ReplacedUnused {
		t.Fatal("user B held no code; nothing should be reported as replaced")
	}

	// Only hashes are stored — the plaintext must not be findable in the table.
	for _, item := range result.Issued {
		var count int
		if err := pool.QueryRow(ctx, `
			select count(*) from user_query_code_bind_tokens where token_hash = $1
		`, item.BindToken).Scan(&count); err != nil {
			t.Fatalf("probe for plaintext: %v", err)
		}
		if count != 0 {
			t.Fatalf("plaintext token for %s is stored in the database", item.CNCode)
		}
	}

	// The batch issues a 7-day code, not the 24-hour single-user one.
	var hoursToExpiry float64
	if err := pool.QueryRow(ctx, `
		select extract(epoch from (expires_at - now())) / 3600
		from user_query_code_bind_tokens
		where user_id = $1::uuid and used_at is null and invalidated_at is null
	`, eligibleB).Scan(&hoursToExpiry); err != nil {
		t.Fatalf("read batch expiry: %v", err)
	}
	if hoursToExpiry < 7*24-1 || hoursToExpiry > 7*24 {
		t.Fatalf("batch expiry = %.1f hours, want ~168 (7 days)", hoursToExpiry)
	}

	// The old code is dead, and the new one works.
	if err := queryapi.NewPostgresStore(pool).BindQueryCode(
		ctx, prefix+"_A", querycode.HashBindToken(oldToken.BindToken), "irrelevant-hash",
	); err == nil {
		t.Fatal("the superseded code still binds; the batch must invalidate it")
	}

	newCode := "BulkNewCode123"
	if err := bindWithToken(t, pool, prefix+"_A", byCN[prefix+"_A"].BindToken, newCode); err != nil {
		t.Fatalf("bind with fresh batch code: %v", err)
	}
	// Single use: the same code cannot be replayed.
	if err := bindWithToken(t, pool, prefix+"_A", byCN[prefix+"_A"].BindToken, "AnotherCode456"); err == nil {
		t.Fatal("a batch code was reusable after a successful bind")
	}
}

// TestPostgresBulkBindTokenReportsSkips is the regression guard for the rule
// that a batch must never come up short in silence.
//
// The race it reproduces is narrow by construction: the candidate read and the
// locked re-check happen microseconds apart inside one call, so simply
// mutating a user beforehand only shrinks the candidate set — it does not
// exercise the skip path at all. To land the state change in the window that
// matters, the test holds a row lock on the target user from a second
// connection: the batch reads all three candidates, then blocks on that row,
// and by the time the lock is released the user has a query code. That is
// exactly the situation the re-check exists for, and it must surface as a
// reported skip rather than a quietly shorter file.
func TestPostgresBulkBindTokenReportsSkips(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "BULK_SKIP_TEST_" + time.Now().Format("20060102150405")
	cleanupBindTokenFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupBindTokenFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertBindTokenAdmin(t, pool, prefix)
	store := NewPostgresStore(pool)

	// The batch walks candidates in cn_code order, so the locked user sorts
	// last and the batch is guaranteed to have read every candidate — and
	// issued the first one — before it blocks.
	insertQueryAccountUser(t, pool, prefix+"_A_KEEP", "")
	raced := insertQueryAccountUser(t, pool, prefix+"_B_RACED", "")

	filters := parseFilters(t, "cn="+prefix+"_A_KEEP&cn="+prefix+"_B_RACED")
	preview, err := store.PreviewBulkQueryCodeBindTokens(ctx, filters)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.EligibleCount != 2 {
		t.Fatalf("preview eligible = %d, want 2", preview.EligibleCount)
	}

	// Hold the row lock before the batch starts.
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocking tx: %v", err)
	}
	defer func() { _ = blocker.Rollback(ctx) }()
	var lockedID string
	if err := blocker.QueryRow(ctx, `select id::text from users where id = $1::uuid for update`, raced).Scan(&lockedID); err != nil {
		t.Fatalf("lock raced user: %v", err)
	}

	type batchOutcome struct {
		result BulkBindTokenResult
		err    error
	}
	done := make(chan batchOutcome, 1)
	go func() {
		result, err := store.BulkCreateQueryCodeBindTokens(ctx, filters, adminID)
		done <- batchOutcome{result, err}
	}()

	// Wait until the batch is demonstrably parked on the lock, so the mutation
	// below cannot land before the candidate read.
	waitForLockWaiter(t, pool, raced)

	if _, err := blocker.Exec(ctx, `
		update users set query_code_hash = 'set-during-batch' where id = $1::uuid
	`, raced); err != nil {
		t.Fatalf("race in a query code: %v", err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("commit blocking tx: %v", err)
	}

	var outcome batchOutcome
	select {
	case outcome = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("batch did not finish after the lock was released")
	}
	if outcome.err != nil {
		t.Fatalf("bulk create: %v", outcome.err)
	}
	result := outcome.result

	if result.Requested != 2 {
		t.Fatalf("requested = %d, want 2 (the candidate read ran before the mutation)", result.Requested)
	}
	if len(result.Issued)+len(result.Skipped) != result.Requested {
		t.Fatalf("issued %d + skipped %d != requested %d", len(result.Issued), len(result.Skipped), result.Requested)
	}
	if len(result.Issued) != 1 {
		t.Fatalf("issued = %d, want 1", len(result.Issued))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("skipped = %d, want 1 — a user dropped by the re-check must be reported", len(result.Skipped))
	}
	skipped := result.Skipped[0]
	if skipped.CNCode != prefix+"_B_RACED" {
		t.Fatalf("skipped CN = %q, want %q", skipped.CNCode, prefix+"_B_RACED")
	}
	if skipped.Reason != SkipReasonNowHasQueryCode {
		t.Fatalf("skip reason = %q, want %q", skipped.Reason, SkipReasonNowHasQueryCode)
	}

	// The skipped user must not have been issued anything.
	var tokenCount int
	if err := pool.QueryRow(ctx, `
		select count(*) from user_query_code_bind_tokens where user_id = $1::uuid
	`, raced).Scan(&tokenCount); err != nil {
		t.Fatalf("count tokens for skipped user: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("skipped user has %d bind tokens, want 0", tokenCount)
	}
}

// waitForLockWaiter blocks until some backend is waiting on a lock held
// against the given user row, so the test never races the batch it is trying
// to interleave with.
func waitForLockWaiter(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(context.Background(), `
			select count(*)
			from pg_stat_activity
			where wait_event_type = 'Lock' and state = 'active' and query ilike '%for update%'
		`).Scan(&waiting); err != nil {
			t.Fatalf("probe lock waiters: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("batch never blocked on the row lock; the race window was not reproduced")
}

// bindWithToken drives the real anonymous bind path so the test proves an
// issued code actually works end to end, not merely that a row exists.
func bindWithToken(t *testing.T, pool *pgxpool.Pool, cn string, token string, newCode string) error {
	t.Helper()
	hashed, err := bcrypt.GenerateFromPassword([]byte(newCode), bcrypt.MinCost)
	if err != nil {
		return err
	}
	return queryapi.NewPostgresStore(pool).BindQueryCode(
		context.Background(), cn, querycode.HashBindToken(token), string(hashed),
	)
}
