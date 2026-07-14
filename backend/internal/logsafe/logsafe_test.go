package logsafe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// sensitiveMarkers stand in for values that must never reach a log line.
var sensitiveMarkers = []string{
	"secret-password",
	"10.99.88.77",
	"sensitive_db_user",
	"SELECT * FROM payments",
	"Key (cn)=(SENSITIVE-CN)",
	"sensitive@example.invalid",
}

func sensitivePgError(code string) *pgconn.PgError {
	return &pgconn.PgError{
		Severity: "ERROR",
		Code:     code,
		Message:  "invalid input syntax for type integer: \"SENSITIVE-CN\" near SELECT * FROM payments",
		Detail:   "Key (cn)=(SENSITIVE-CN) already exists for sensitive@example.invalid",
		Hint:     "connect as sensitive_db_user to 10.99.88.77 with secret-password",
		Where:    "SQL statement SELECT * FROM payments",
	}
}

func assertClean(t *testing.T, category string) {
	t.Helper()
	for _, marker := range sensitiveMarkers {
		if strings.Contains(category, marker) {
			t.Fatalf("category %q leaked %q", category, marker)
		}
	}
}

func TestCategoryContextErrors(t *testing.T) {
	if got := Category(context.DeadlineExceeded); got != "timeout" {
		t.Fatalf("deadline = %q", got)
	}
	if got := Category(fmt.Errorf("query: %w", context.DeadlineExceeded)); got != "timeout" {
		t.Fatalf("wrapped deadline = %q", got)
	}
	if got := Category(context.Canceled); got != "cancelled" {
		t.Fatalf("canceled = %q", got)
	}
	if got := Category(fmt.Errorf("query: %w", context.Canceled)); got != "cancelled" {
		t.Fatalf("wrapped canceled = %q", got)
	}
}

func TestCategoryPostgresClasses(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{code: "23505", want: "database unique violation"},
		{code: "23503", want: "database foreign key violation"},
		{code: "40001", want: "database serialization conflict or deadlock"},
		{code: "40P01", want: "database serialization conflict or deadlock"},
		{code: "57014", want: "database query cancelled or timed out"},
		{code: "08006", want: "database connection error"},
		{code: "28P01", want: "database authentication error"},
		{code: "22P02", want: "database error (SQLSTATE 22P02)"},
	}
	for _, test := range tests {
		err := fmt.Errorf("store: %w", sensitivePgError(test.code))
		got := Category(err)
		if got != test.want {
			t.Fatalf("code %s: category = %q, want %q", test.code, got, test.want)
		}
		assertClean(t, got)
	}
}

func TestCategoryNeverEchoesMessageDetailHintWhere(t *testing.T) {
	for _, code := range []string{"23505", "22P02", "XX000"} {
		assertClean(t, Category(sensitivePgError(code)))
	}
}

func TestCategoryConnectError(t *testing.T) {
	connectErr := &pgconn.ConnectError{}
	got := Category(fmt.Errorf("connect: %w", connectErr))
	if got != "database connection failed: authentication or connectivity error" {
		t.Fatalf("connect error category = %q", got)
	}
	assertClean(t, got)
}

type fakeNetError struct{ timeout bool }

func (e fakeNetError) Error() string   { return "dial tcp 10.99.88.77:5432: refused" }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return false }

func TestCategoryNetErrors(t *testing.T) {
	if got := Category(fakeNetError{timeout: true}); got != "network timeout" {
		t.Fatalf("net timeout = %q", got)
	}
	got := Category(fakeNetError{})
	if got != "network error" {
		t.Fatalf("net error = %q", got)
	}
	assertClean(t, got)
}

func TestCategoryFallbackAndNil(t *testing.T) {
	got := Category(errors.New("postgres://sensitive_db_user:secret-password@10.99.88.77/db failed"))
	if got != "internal error" {
		t.Fatalf("fallback = %q", got)
	}
	assertClean(t, got)
	if Category(nil) != "" {
		t.Fatal("nil error must map to an empty category")
	}
}
