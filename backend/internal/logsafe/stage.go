package logsafe

import "errors"

// Stage wraps err with a fixed, developer-written stage label so a failure can
// be located without logging the driver's own message.
//
// The label must be a compile-time constant string. It is the only part of the
// wrapper that Detail prints, which is what keeps the log free of SQL text,
// parameter values, connection strings, and user data — the same guarantee
// Category makes.
//
// Stage participates in the normal %w chain: errors.Is and errors.As still see
// through it, so the sentinel checks in the handlers are unaffected.
func Stage(label string, err error) error {
	if err == nil {
		return nil
	}
	return &stageError{label: label, err: err}
}

type stageError struct {
	label string
	err   error
}

func (e *stageError) Error() string { return e.label + ": " + e.err.Error() }

func (e *stageError) Unwrap() error { return e.err }

// Detail renders a log-safe trace: every Stage label from the outside in,
// followed by Category's fixed description of the underlying error. For a
// failure inside the product upsert it reads like
//
//	persist import: upsert product -> database error (SQLSTATE 42703)
//
// which names the exact statement that failed without quoting it. A nil error
// yields an empty string.
func Detail(err error) string {
	if err == nil {
		return ""
	}
	trace := ""
	for current := err; current != nil; current = errors.Unwrap(current) {
		stage, ok := current.(*stageError)
		if !ok {
			continue
		}
		if trace != "" {
			trace += ": "
		}
		trace += stage.label
	}
	category := Category(err)
	if trace == "" {
		return category
	}
	return trace + " -> " + category
}
