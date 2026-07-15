# Admin authentication security audit

This feature records security-relevant administrator authentication events in a dedicated database table, `admin_auth_audit_events`.

## Recorded events

The current phase records:

- `admin_login_succeeded`
- `admin_login_failed`
- `admin_login_rate_limited`
- `admin_logout_succeeded`

Session middleware rejects unauthenticated requests with the existing HTTP response and log behavior. It does not write a database audit row for every missing, invalid, or expired session in this phase because those events are noisy and usually cannot be tied to a reliable administrator identity.

## Stored fields

Each audit row stores only bounded, non-secret fields:

- event type
- event time
- administrator id when it is already known
- normalized administrator username
- resolved client IP key
- result (`success` or `failure`)
- reason code
- sanitized user-agent summary

The username, client IP and user-agent values are trimmed, stripped of control characters, and length-limited before storage.

## Reason codes

Current reason codes are:

- `none`
- `invalid_credentials`
- `account_disabled`
- `rate_limited`
- `database_error`
- `audit_write_error`

The external login failure response remains uniform for unknown username, disabled account and wrong password. The audit reason is internal and must not be exposed to the client.

## Data that must never be stored

Audit rows, logs, documentation and tests must not store or print:

- passwords or password hashes
- session cookies or raw session tokens
- Authorization headers
- query codes, binding codes, email verification codes or recovery tokens
- database passwords, DSNs or environment secret values
- raw request bodies

## Audit write behavior

Successful login is transactional: the admin session, `last_login_at` update and success audit row are committed together. If the audit row cannot be written, the login fails with the existing generic 500 response and no session cookie is issued.

Failed login, rate-limited login and logout audit writes are best-effort. If the audit write fails, the user-facing response remains unchanged and the server log records only a sanitized error category.

Rate-limited audit rows are de-duplicated in memory per IP and normalized username for a short window so a retry loop cannot generate unbounded audit noise.

## Retention and access

This phase does not add an admin UI or query API for audit rows. Operators can inspect the table directly with appropriately restricted database access. A future phase can add a read-only, paginated admin audit endpoint and define an explicit retention job after operational requirements are settled.