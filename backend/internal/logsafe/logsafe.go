// Package logsafe maps errors to short, fixed categories that are safe to
// write to logs. Raw database and network errors can embed connection
// targets, SQL fragments, or user-supplied parameter values; Category never
// includes any part of the original error text — only a fixed description
// and, for PostgreSQL errors, the five-character SQLSTATE code.
package logsafe

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgconn"
)

// Category returns a fixed, log-safe description of err. It never returns
// the error's own text. A nil error yields an empty string.
func Category(err error) string {
	if err == nil {
		return ""
	}

	var pgErr *pgconn.PgError
	var connectErr *pgconn.ConnectError
	var netErr net.Error

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &pgErr):
		return pgCategory(pgErr)
	case errors.As(err, &connectErr):
		// ConnectError's text includes host, user, and database name.
		return "database connection failed: authentication or connectivity error"
	case errors.As(err, &netErr):
		if netErr.Timeout() {
			return "network timeout"
		}
		return "network error"
	}
	return "internal error"
}

func pgCategory(pgErr *pgconn.PgError) string {
	switch pgErr.Code {
	case "23505":
		return "database unique violation"
	case "23503":
		return "database foreign key violation"
	case "40001", "40P01":
		return "database serialization conflict or deadlock"
	case "57014":
		return "database query cancelled or timed out"
	}
	if len(pgErr.Code) >= 2 {
		switch pgErr.Code[:2] {
		case "08":
			return "database connection error"
		case "28":
			return "database authentication error"
		}
	}
	// The SQLSTATE code is a fixed five-character class identifier; unlike
	// the message, it can never carry SQL text or parameter values.
	return fmt.Sprintf("database error (SQLSTATE %s)", pgErr.Code)
}
