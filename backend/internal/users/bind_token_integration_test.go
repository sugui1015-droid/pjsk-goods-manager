package users

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	queryapi "pjsk/backend/internal/query"
	"pjsk/backend/internal/querycode"
)

// TestPostgresBindTokenLifecycle covers the full MVP2 flow against a real
// database with prefixed, self-cleaning fixture data: admin generation,
// regeneration invalidating old tokens, hash-only storage, wrong-token
// counting, the failure cap, single use, and first-login with the new code.
func TestPostgresBindTokenLifecycle(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "BIND_TOKEN_TEST_" + time.Now().Format("20060102150405")
	cleanupBindTokenFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupBindTokenFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertBindTokenAdmin(t, pool, prefix)
	store := NewPostgresStore(pool)
	queryStore := queryapi.NewPostgresStore(pool)
	queryHandler := queryapi.NewHandler(queryStore, time.Hour, false)

	cn := prefix + "CN"
	userID := insertQueryAccountUser(t, pool, cn, "")

	// Generate: token has the documented shape; only its hash is stored;
	// admin and expiry are recorded.
	first, err := store.CreateQueryCodeBindToken(ctx, userID, adminID)
	if err != nil {
		t.Fatalf("create bind token: %v", err)
	}
	if len(first.BindToken) != querycode.BindTokenLength {
		t.Fatalf("token length = %d, want %d", len(first.BindToken), querycode.BindTokenLength)
	}
	if strings.ContainsAny(first.BindToken, "0O1IL") {
		t.Fatalf("token %q contains confusable characters", first.BindToken)
	}
	var storedHash, createdBy string
	var minutesToExpiry float64
	if err := pool.QueryRow(ctx, `
		select token_hash, created_by_admin_id::text, extract(epoch from (expires_at - now())) / 60
		from user_query_code_bind_tokens
		where user_id = $1::uuid and invalidated_at is null
	`, userID).Scan(&storedHash, &createdBy, &minutesToExpiry); err != nil {
		t.Fatalf("read stored token: %v", err)
	}
	if storedHash == first.BindToken || storedHash != querycode.HashBindToken(first.BindToken) {
		t.Fatalf("stored hash is wrong or plaintext: %q", storedHash)
	}
	if createdBy != adminID {
		t.Fatalf("created_by = %s, want %s", createdBy, adminID)
	}
	if minutesToExpiry < 25 || minutesToExpiry > 35 {
		t.Fatalf("expiry = %.1f minutes from now, want ~30", minutesToExpiry)
	}

	detail, err := store.GetUserDetail(ctx, userID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !detail.HasActiveBindToken || detail.BindTokenExpiresAt == "" {
		t.Fatalf("detail bind token status = %+v", detail)
	}

	// Regenerate: previous token becomes invalid immediately.
	second, err := store.CreateQueryCodeBindToken(ctx, userID, adminID)
	if err != nil {
		t.Fatalf("regenerate bind token: %v", err)
	}
	var firstInvalidated bool
	if err := pool.QueryRow(ctx, `
		select invalidated_at is not null
		from user_query_code_bind_tokens
		where user_id = $1::uuid and token_hash = $2
	`, userID, querycode.HashBindToken(first.BindToken)).Scan(&firstInvalidated); err != nil {
		t.Fatalf("read first token: %v", err)
	}
	if !firstInvalidated {
		t.Fatal("regeneration did not invalidate the previous token")
	}

	newCode := "BindNew-123"
	newHashBytes, err := bcrypt.GenerateFromPassword([]byte(newCode), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	// Wrong token: rejected and counted.
	if err := queryStore.BindQueryCode(ctx, cn, querycode.HashBindToken("WRONGTOKEN"), string(newHashBytes)); !errors.Is(err, queryapi.ErrBindRejected) {
		t.Fatalf("wrong token error = %v, want ErrBindRejected", err)
	}
	var failedAttempts int
	if err := pool.QueryRow(ctx, `
		select failed_attempts from user_query_code_bind_tokens
		where user_id = $1::uuid and token_hash = $2
	`, userID, querycode.HashBindToken(second.BindToken)).Scan(&failedAttempts); err != nil {
		t.Fatal(err)
	}
	if failedAttempts != 1 {
		t.Fatalf("failed_attempts = %d, want 1", failedAttempts)
	}

	// Invalidated first token: rejected even though the hash matches a row.
	if err := queryStore.BindQueryCode(ctx, cn, querycode.HashBindToken(first.BindToken), string(newHashBytes)); !errors.Is(err, queryapi.ErrBindRejected) {
		t.Fatalf("invalidated token error = %v, want ErrBindRejected", err)
	}

	// Correct token: binds, marks used, and the new code can log in.
	if err := queryStore.BindQueryCode(ctx, cn, querycode.HashBindToken(second.BindToken), string(newHashBytes)); err != nil {
		t.Fatalf("bind: %v", err)
	}
	var usedAtSet bool
	if err := pool.QueryRow(ctx, `
		select used_at is not null from user_query_code_bind_tokens
		where user_id = $1::uuid and token_hash = $2
	`, userID, querycode.HashBindToken(second.BindToken)).Scan(&usedAtSet); err != nil {
		t.Fatal(err)
	}
	if !usedAtSet {
		t.Fatal("token not marked used")
	}
	assertHasQueryCode(t, pool, userID, true)
	queryLogin(t, queryHandler, cn, newCode, 200)

	detail, err = store.GetUserDetail(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.HasActiveBindToken {
		t.Fatal("detail still reports an active bind token after use")
	}

	// Used token cannot be replayed; user with a code cannot get a new one.
	if err := queryStore.BindQueryCode(ctx, cn, querycode.HashBindToken(second.BindToken), string(newHashBytes)); !errors.Is(err, queryapi.ErrBindRejected) {
		t.Fatalf("replay error = %v, want ErrBindRejected", err)
	}
	if _, err := store.CreateQueryCodeBindToken(ctx, userID, adminID); !errors.Is(err, ErrBindTokenUserHasCode) {
		t.Fatalf("generate for user with code = %v, want ErrBindTokenUserHasCode", err)
	}
}

func TestPostgresBindTokenIneligibleUsersAndCaps(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "BIND_TOKEN_TEST2_" + time.Now().Format("20060102150405")
	cleanupBindTokenFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupBindTokenFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertBindTokenAdmin(t, pool, prefix)
	store := NewPostgresStore(pool)
	queryStore := queryapi.NewPostgresStore(pool)

	// Disabled and merged users cannot generate.
	disabledID := insertQueryAccountUser(t, pool, prefix+"DISABLED", "")
	if _, err := pool.Exec(ctx, `update users set status = 'disabled' where id = $1::uuid`, disabledID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQueryCodeBindToken(ctx, disabledID, adminID); !errors.Is(err, ErrBindTokenUserInactive) {
		t.Fatalf("disabled generate = %v, want ErrBindTokenUserInactive", err)
	}
	mergedID := insertQueryAccountUser(t, pool, prefix+"MERGED", "")
	if _, err := pool.Exec(ctx, `update users set status = 'merged' where id = $1::uuid`, mergedID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQueryCodeBindToken(ctx, mergedID, adminID); !errors.Is(err, ErrBindTokenUserInactive) {
		t.Fatalf("merged generate = %v, want ErrBindTokenUserInactive", err)
	}

	newHashBytes, err := bcrypt.GenerateFromPassword([]byte("BindNew-456"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	// Expired token is rejected.
	expiredCN := prefix + "EXPIRED"
	expiredID := insertQueryAccountUser(t, pool, expiredCN, "")
	expiredToken, err := store.CreateQueryCodeBindToken(ctx, expiredID, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update user_query_code_bind_tokens set expires_at = now() - interval '1 minute' where user_id = $1::uuid`, expiredID); err != nil {
		t.Fatal(err)
	}
	if err := queryStore.BindQueryCode(ctx, expiredCN, querycode.HashBindToken(expiredToken.BindToken), string(newHashBytes)); !errors.Is(err, queryapi.ErrBindRejected) {
		t.Fatalf("expired token = %v, want ErrBindRejected", err)
	}

	// Token from another user's CN is rejected (token/CN mismatch).
	capCN := prefix + "CAP"
	capID := insertQueryAccountUser(t, pool, capCN, "")
	capToken, err := store.CreateQueryCodeBindToken(ctx, capID, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if err := queryStore.BindQueryCode(ctx, expiredCN, querycode.HashBindToken(capToken.BindToken), string(newHashBytes)); !errors.Is(err, queryapi.ErrBindRejected) {
		t.Fatalf("cross-user token = %v, want ErrBindRejected", err)
	}

	// Five wrong attempts invalidate the token; the correct token then fails.
	for i := 0; i < 5; i++ {
		if err := queryStore.BindQueryCode(ctx, capCN, querycode.HashBindToken("WRONGTOKEN"), string(newHashBytes)); !errors.Is(err, queryapi.ErrBindRejected) {
			t.Fatalf("wrong attempt %d = %v, want ErrBindRejected", i+1, err)
		}
	}
	var invalidated bool
	if err := pool.QueryRow(ctx, `
		select invalidated_at is not null from user_query_code_bind_tokens
		where user_id = $1::uuid and token_hash = $2
	`, capID, querycode.HashBindToken(capToken.BindToken)).Scan(&invalidated); err != nil {
		t.Fatal(err)
	}
	if !invalidated {
		t.Fatal("token not invalidated after reaching the failure cap")
	}
	if err := queryStore.BindQueryCode(ctx, capCN, querycode.HashBindToken(capToken.BindToken), string(newHashBytes)); !errors.Is(err, queryapi.ErrBindRejected) {
		t.Fatalf("capped token = %v, want ErrBindRejected", err)
	}
	assertHasQueryCode(t, pool, capID, false)
}

func insertBindTokenAdmin(t *testing.T, pool *pgxpool.Pool, prefix string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("bind-token-test-admin-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	var adminID string
	if err := pool.QueryRow(context.Background(), `
		insert into admins (username, password_hash, display_name, role, status)
		values ($1, $2, 'Bind token test admin', 'admin', 'active')
		returning id::text
	`, prefix+"admin", string(hash)).Scan(&adminID); err != nil {
		t.Fatalf("insert test admin: %v", err)
	}
	return adminID
}

func cleanupBindTokenFixture(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`delete from user_query_code_bind_tokens where user_id in (select id from users where cn_code like $1)`,
		`delete from query_sessions where user_id in (select id from users where cn_code like $1)`,
		`delete from users where cn_code like $1`,
		`delete from admin_sessions where admin_id in (select id from admins where username like $1)`,
		`delete from admins where username like $1`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, prefix+"%"); err != nil {
			t.Fatalf("cleanup bind token fixture: %v", err)
		}
	}
}
