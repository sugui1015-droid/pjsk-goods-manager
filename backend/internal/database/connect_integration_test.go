package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/logsafe"
)

// Test 5: connecting to an unreachable database must fail quickly and must not
// consume anything like the migration budget, and the error must not leak the
// DSN or a password.
//
// This deliberately targets 127.0.0.1:1 — a port nothing listens on — so no
// real database, least of all production, is involved. The credentials are
// fabricated and have no value.
func TestIsolatedConnectFailsFastOnUnreachableDatabase(t *testing.T) {
	requireIsolatedTestEnv(t)

	// A DSN that cannot possibly reach a real server. The password here is a
	// throwaway literal used only to prove it never reaches a log.
	const sentinelPassword = "not-a-real-password-sentinel"
	unreachableDSN := "postgres://pjsk_fake_test_user:" + sentinelPassword + "@127.0.0.1:1/pjsk_migration_test_unreachable?sslmode=disable"

	// Mirrors main.go's databaseConnectTimeout policy: connect gets a short
	// budget, independent of (and far smaller than) the migration budget.
	const connectBudget = 10 * time.Second
	const migrationBudget = 2 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), connectBudget)
	defer cancel()

	start := time.Now()
	pool, err := Connect(ctx, unreachableDSN)
	elapsed := time.Since(start)

	if err == nil {
		if pool != nil {
			pool.Close()
		}
		t.Fatal("expected Connect to fail against 127.0.0.1:1")
	}
	if pool != nil {
		t.Error("Connect must not return a usable pool when it fails")
	}

	// Must be nowhere near the migration budget. Not asserting ~10s exactly:
	// a refused connection returns almost immediately, and this only needs to
	// prove connect does not wait on the migration budget.
	t.Logf("Connect failed after %s", elapsed.Round(time.Millisecond))
	if elapsed >= connectBudget {
		t.Errorf("Connect took %s, which reached the connect budget of %s", elapsed, connectBudget)
	}
	if elapsed >= migrationBudget {
		t.Errorf("Connect took %s, which is at or beyond the migration budget of %s", elapsed, migrationBudget)
	}

	// main.go logs logsafe.Category(err), never the raw error. Prove that what
	// would be logged carries no password, no DSN, and no host details.
	logged := logsafe.Category(err)
	t.Logf("logsafe category that main.go would log: %q", logged)
	for _, secret := range []string{sentinelPassword, unreachableDSN, "pjsk_fake_test_user", "postgres://", "127.0.0.1"} {
		if strings.Contains(logged, secret) {
			t.Errorf("the logged category leaked %q: %q", secret, logged)
		}
	}
}
