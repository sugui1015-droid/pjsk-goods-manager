# HANDOVER

Handover notes for whoever continues development on this repository (Codex, Cline, a human dev, etc.). This file intentionally contains no secrets — no real database passwords, admin passwords, query codes, cookies, or API keys.

## 1. Project Overview

PJSK Goods Manager tracks group-buy ("团购") orders for a hobby merchandise community: Excel order sheets come in from various group-buy organizers, get imported into a shared database, and admins reconcile who has paid for what. It is being migrated off a legacy Streamlit + Supabase app onto a Vue 3 + Go + PostgreSQL stack. The legacy app (`legacy-streamlit/`) stays in the repo as a running fallback until the new stack fully covers its functionality.

## 2. Local Directory Structure

```
D:\pjsk
├── backend/            Go API (module "pjsk/backend")
│   ├── cmd/             one-off admin CLI tools (create-admin, set-query-code)
│   ├── internal/        admin, api, config, database, importpreview, orders, payments, query
│   ├── migrations/      plain .sql files, applied in filename order at startup
│   └── main.go
├── frontend/            Vue 3 + TypeScript + Vite app
│   └── src/              App.vue (single-page shell with manual path routing), api/client.ts
├── legacy-streamlit/    old Python/Streamlit app, kept runnable
├── docs/                design notes and workflow docs (see below)
├── testdata/excel/      fixture spreadsheets + expected-result manifest for the import parser
├── backups/, pjsk-data-backup/   local data backups (not build artifacts, do not delete blindly)
└── HANDOVER.md          this file
```

Key docs to read next: [docs/database-design.md](docs/database-design.md), [docs/excel-import-rules.md](docs/excel-import-rules.md), [docs/payment-workflow.md](docs/payment-workflow.md), [docs/normal-use-roadmap.md](docs/normal-use-roadmap.md).

## 3. Tech Stack

- **Backend**: Go (see `backend/go.mod` for the version), `net/http` standard library router (no framework), `github.com/jackc/pgx/v5` for PostgreSQL, `golang.org/x/crypto/bcrypt` for password/query-code hashing, `github.com/joho/godotenv` for `.env` loading.
- **Frontend**: Vue 3 + TypeScript + Vite, no UI framework, no `vue-router` (routing is hand-rolled off `window.location.pathname` in `App.vue`), package manager is `pnpm`.
- **Database**: PostgreSQL (a Supabase-hosted instance in production/staging use; any PostgreSQL works locally).
- **Legacy**: Python + Streamlit, kept as-is.

## 4. Starting Frontend and Backend Locally

Backend:

```bash
cd backend
go run .
# or: backend\run.cmd (sets GOCACHE/GOPATH/GOMODCACHE under D:\pjsk\.cache and D:\go\bin PATH)
```

Listens on `http://127.0.0.1:8080` in local development (`APP_PORT` / `SERVER_PORT` / `BACKEND_PORT` env, in that priority order). Exposes `GET /health` and `GET /api/config` unauthenticated; everything else under `/api/admin/*` requires an admin session cookie, `/api/query/*` requires a query session cookie.

Frontend:

```bash
cd frontend
pnpm install   # first time only
pnpm dev
# or: frontend\run.cmd (binds --host 0.0.0.0)
```

Opens on `http://127.0.0.1:5173` in local development. Dev requests use relative `/api` and `/health`; Vite proxies them to `http://127.0.0.1:8080`. `VITE_API_BASE_URL` is for non-development builds.

First admin account (interactive, password never echoed or logged):

```bash
cd backend
go run ./cmd/create-admin -username admin
```

Set a query code for a CN (used by the regular-user query flow):

```bash
cd backend
go run ./cmd/set-query-code -cn "<cn>"
```

## 5. PostgreSQL Configuration and `.env`

`backend/internal/config/config.go` loads `.env` via `godotenv.Load()` (relative to the process's working directory — i.e. run backend commands from inside `backend/`, or the `.env` won't be found).

Connection is resolved in this order:
1. `DATABASE_URL` if set (full `postgres://...` DSN), else
2. built from `DATABASE_HOST` / `DATABASE_PORT` / `DATABASE_USER` / `DATABASE_PASSWORD` / `DATABASE_NAME` / `DATABASE_SSLMODE`.

Other env vars actually read by the Go backend: `APP_ENV`, `APP_PORT` / `SERVER_PORT` / `BACKEND_PORT`, `SERVER_HOST` (bind address, defaults to `127.0.0.1` — loopback only), `ADMIN_SESSION_TTL` (Go duration string, e.g. `12h`), `ADMIN_COOKIE_SECURE` (`true`/`false`), `TRUSTED_PROXY_CIDRS`, `CORS_ALLOWED_ORIGINS`, `LEGACY_STREAMLIT_ADMIN_PORT`, `LEGACY_STREAMLIT_USER_PORT`, plus the recovery-email/SMTP and HMAC key variables (`RECOVERY_EMAIL_*`, `QUERY_CODE_RECOVERY_HMAC_KEY`) documented in [docs/internal-deployment-secrets.md](docs/internal-deployment-secrets.md) and [docs/internal-network-deployment.md](docs/internal-network-deployment.md).

There are two `.env.example` files with different shapes — see [Known Issues](#14-known-issues) below; don't assume they're interchangeable without checking `config.go`.

Never commit a real `.env`. Real credentials belong only in a local, gitignored `.env` file.

## 6. Database Migrations

`backend/internal/database/migrations.go` runs on every backend startup:

- Ensures a `schema_migrations(version text primary key, applied_at timestamptz)` table exists.
- Reads all `*.sql` files under `backend/migrations/`, **sorted by filename** (not by any embedded number parsed out separately — the whole filename is the sort key and the stored `version`).
- For each file not already present in `schema_migrations.version`, runs it inside a transaction and inserts a row keyed by the exact filename.

Current migration files: `0001_core_tables.sql` … `0018_query_code_email_recovery.sql`. There is a known numbering irregularity (two files named `0005_*`, no `0006_*`) — see [Known Issues](#14-known-issues). Do not rename existing migration files without understanding that `schema_migrations.version` is the filename itself; renaming a file makes the migration runner think it's unapplied and try to run it again.

## 7. Implemented Features

- Admin login/session (cookie-based, token hash stored server-side, not JWT).
- Excel import: preview → confirm, import history, import revert.
- Admin order query (list + detail, read-only).
- Regular user CN + query-code login and order query.
- Admin unpaid-balance query per CN.
- Manual payment entry by admin, created directly as `approved`.
- WeChat payment fee calculation (0.1% of principal, rounded up to the cent).
- Partial payment across multiple payments per order item.
- Payment detail view.
- Payment void with mandatory reason and full audit trail (`voided_at` / `voided_by` / `void_reason`), which rolls back item/order payment status.
- CSV/Excel export of users, payments, and order items (admin-only, `backend/internal/export`).
- Admin query-account management, one-time query-code bind tokens, user query-code change, CN merge.
- Recovery email (AES-GCM encrypted + HMAC blind index), logged-in email verification codes, and anonymous query-code recovery by email — with an independent `QUERY_CODE_RECOVERY_HMAC_KEY` root key (required in production, no legacy fallback there).
- SMTP sender with strict TLS (1.2+, certificate verification always on, `tls`/`starttls` only) and a test-only fake sender (`APP_ENV=test` only; development and production both reject it). `/api/config` exposes a single `emailDeliveryEnabled` boolean.
- Internal-deployment hardening (commit `8dc21e1b587c7c69214ace268c114e3bdb3eadf0`): trusted-proxy client-IP resolution (`backend/internal/clientip`, `TRUSTED_PROXY_CIDRS`, X-Forwarded-For only, off by default), loopback-default `SERVER_HOST`, strict `CORS_ALLOWED_ORIGINS` (production defaults to none), and sanitized error logging (`backend/internal/logsafe` — no raw database errors, DSNs, or user input in logs). See [docs/internal-network-deployment.md](docs/internal-network-deployment.md).
- Admin login rate limiting: per-IP 20 attempts/minute and per IP+username 5 failures/10 minutes → 10-minute block (in-memory, process-local; cleared on restart), wired to the shared client-IP resolver.
- Query-login rate limiter memory is bounded: both in-memory maps (`backend/internal/query/ratelimit.go`) are capped at `maxTrackedKeys` (default 10000) with lazy expiry, LRU eviction of the least-recently-seen **unblocked** key, and a safe degradation (don't create a new failure key) when every key is actively blocked — a currently-blocked pair is never evicted early. Thresholds/block duration/429/error text are unchanged. The admin limiter uses a simpler (also-bounded) fail-closed cap; unifying the two into one shared limiter is a possible future cleanup, not required.

Full status table: `GET /api/config` and [docs/normal-use-roadmap.md](docs/normal-use-roadmap.md).

## 8. Current Main Pages and APIs

Frontend routes (all inside the single `App.vue` shell, see `RouteName` type):

- `/` — overview / module status
- `/query` — CN + query-code lookup for regular users
- `/admin/imports`, `/admin/imports/history`, `/admin/imports/history/{id}` — Excel import preview/confirm/history/detail
- `/admin/orders`, `/admin/orders/{id}` — admin order list/detail
- `/admin/payments`, `/admin/payments/{id}` — admin payment records list/detail

Backend routes (see `backend/internal/api/router.go`):

- `GET /health`, `GET /api/config`
- `POST /api/admin/login`, `GET /api/admin/me`, `POST /api/admin/logout`
- `POST /api/admin/imports/preview`, `POST /api/admin/imports/confirm`, `GET /api/admin/imports`, `GET /api/admin/imports/{id}`
- `GET /api/admin/orders`, `GET /api/admin/orders/{id}`
- `GET /api/admin/payments/cn?cn=`, `GET /api/admin/payments/unpaid?cn=`
- `GET /api/admin/payments`, `POST /api/admin/payments`, `GET /api/admin/payments/{id}`, `POST /api/admin/payments/{id}/void`
- `POST /api/query/login`, `GET /api/query/orders`, `POST /api/query/logout`

## 9. Excel Import Key Rules

Full detail in [docs/excel-import-rules.md](docs/excel-import-rules.md); short version:

- Two sheet formats are recognized: "matrix summary" (tried first) and "detail table" (fallback). Sheets matching neither are skipped without erroring.
- Detail tables: header row found by alias matching in the first 30 rows; recognizes `cn` / `item_name` / `role` / `batch` / `quantity` / `unit_price` / `amount` / `collected` under many Chinese aliases.
- Matrix tables: locates `种类` / `单价` (+ optional `昵称总数`) in the first 30 rows to find CN column, amount column, and role columns.
- Dedup key is an MD5 of `source_file + source_sheet + batch + cn + item_name + role` plus an occurrence index.
- Parser output must be checked against `testdata/excel/manifest.csv` for the known fixture files.

## 10. Payment Key Rules

Full detail in [docs/payment-workflow.md](docs/payment-workflow.md); short version:

- Admin-only, manual entry — no user-submitted screenshots, no submitted/rejected review step (see [Known Issues](#14-known-issues) — not built yet).
- Created payments are `approved` immediately.
- One payment can cover multiple order items; one order item can be paid across multiple payments (partial payment supported).
- WeChat fee = `ceil(principal_cents / 1000)` cents (0.1% of principal, rounded up, never rounded down or to nearest).
- Payments cannot be edited after creation. To correct one: void it (with a required reason) and create a new one. Voided payments carry `voided_at` / `voided_by` / `void_reason` and stop counting toward paid totals.

## 11. Regular User Query Method

Regular users authenticate with `CN + query code` (not username/password) at `POST /api/query/login`. The query code is bcrypt-hashed server-side; a dummy hash is compared against on unknown CNs to avoid timing leaks. A successful login sets a separate session cookie (`pjsk_query_session`) used to call `GET /api/query/orders`.

## 12. Test and Build Commands

Backend (run from `backend/`):

```bash
go fmt ./...
go build ./...
go test ./...
go vet ./...
```

Some backend tests (e.g. `void_integration_test.go`) hit a real PostgreSQL instance — they read connection info the same way `config.Load()` does, so a reachable local/dev database is expected when running the full suite.

Frontend (run from `frontend/`):

```bash
pnpm run build   # runs vue-tsc -b && vite build
```

There is currently no frontend automated test script (`package.json` only defines `dev`, `build`, `preview`) — see [Known Issues](#14-known-issues).

## 13. Git Commit and Push Method

- Standard flow: `git add <specific files>`, `git commit -m "..."`, `git push origin main`.
- If a plain `git push` fails (e.g. proxy/TLS issues), this has been used as a fallback:
  ```bash
  git -c http.proxy=http://127.0.0.1:7897 -c http.version=HTTP/1.1 -c http.sslBackend=openssl push origin main
  ```
  Only use the proxy fallback if the plain push actually fails — don't default to it.
- Prefer small, single-purpose commits over bundling unrelated changes. Never use `git add -A`/`git add .` blindly — review `git status` first so unrelated local files (backups, caches, `.env`) don't get swept in.
- `.gitattributes` enforces LF for Go/Vue/TS/JS/JSON/CSS/SQL/Markdown/YAML and CRLF for `.cmd`/`.bat`/`.ps1`; binary types (`.xlsx`, images, fonts, `.db`) are marked `binary`. Run `git diff --check` before committing if you've touched a lot of files, to catch stray whitespace/line-ending issues.

## 14. Known Issues

- **Duplicate `0005` migration filenames**: `backend/migrations/0005_import_history.sql` and `backend/migrations/0005_product_series.sql` both exist, and there is no `0006_*` file. Because the migration runner keys `schema_migrations` on the full filename (not just the numeric prefix) and sorts by filename, both `0005_*` files are independent, distinctly-tracked migrations and can both be applied without conflict — but the numbering itself is inconsistent with the rest of the sequence. **Resolved as a permanent, documented exception (2026-07-15)**: the pair is intentionally kept as-is forever — never rename either file, never backfill a `0006`, never reuse a numeric prefix. Offline tests (`backend/internal/database/migrations_test.go`, `backend/main_test.go`) enforce these rules, and [docs/migration-numbering-deployment-check.md](docs/migration-numbering-deployment-check.md) records the exception plus the read-only SQL checklist to run against a target database before any production deployment.
- **Two database configuration styles in `.env.example` files**: root `.env.example` demonstrates a single `DATABASE_URL`; `backend/.env.example` demonstrates split `DATABASE_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`. Both styles are supported; when `DATABASE_URL` is non-empty it takes precedence. Follow `config.go` as the source of truth.
- **No frontend test script** — only `dev`/`build`/`preview` exist in `frontend/package.json`.
- **No CI** — `go test`, `go vet`, and `pnpm run build` are only run manually/locally today.
- **`App.vue` is a single large component** with hand-rolled routing; no `vue-router`, no component decomposition yet (tracked in the roadmap, not addressed in this update).
- See [docs/normal-use-roadmap.md](docs/normal-use-roadmap.md) "Not done / still needed" for the fuller backlog (admin user management, CN merge, diff import, role/category dictionary maintenance UI, data export, unified audit log, permission tiers, production deployment).

## 15. Next Development Order (Suggested)

Roughly in priority order, per [docs/normal-use-roadmap.md](docs/normal-use-roadmap.md):

1. Migration numbering already continued from `0013` onward (now at `0019`), leaving the historical duplicate-`0005` pair untouched; the cleanup question is now settled — the pair stays forever as a documented exception with test enforcement (see Known Issues and [docs/migration-numbering-deployment-check.md](docs/migration-numbering-deployment-check.md)). New migrations start at `0020`.
2. ~~Data export (CSV/Excel)~~ — done (`backend/internal/export`, admin export routes).
3. Unified admin audit log spanning imports/orders/payments, not just payment voids.
4. CN merge and diff-based re-import — both touch import correctness, so they should land with strong test coverage.
5. Role/category dictionary maintenance UI.
6. Admin user management, permission tiers.
7. Frontend decomposition (`vue-router` + component split) — do this once the API surface above has stabilized, to avoid rewriting routing twice.
8. CI pipeline (`go test`, `go vet`, `pnpm run build` on push).
9. Production deployment: domain, HTTPS, backup strategy.
   - Database backup/restore tooling: done (`scripts/database/`, [docs/database-backup-restore.md](docs/database-backup-restore.md)) — verified via an isolated restore drill.
   - Backup retention tooling: done ([docs/database-backup-retention.md](docs/database-backup-retention.md)) — a read-only scan/report script (`Get-PostgresBackupRetentionReport.ps1`) and a **DryRun-by-default** cleanup script (`Remove-ExpiredPostgresBackups.ps1`, shared logic in `_RetentionCommon.ps1`). Real deletion needs `-Execute` + `-ExpectedRootName` + a confirmation phrase + a clean re-scan, and only ever removes validated backups outside all retention tiers. Safety tests are in `Invoke-RetentionSafetyTests.ps1` (also run by `Invoke-ScriptSafetyTests.ps1`). No real backups were ever deleted.
   - Internal reverse proxy / web gateway: **the LAN HTTP stage is live (2026-07-16)**. Service `pjsk-caddy` (display name "PJSK Goods Manager Web Gateway") runs Caddy **v2.11.4** (`D:\PJSK-Service\caddy\caddy.exe`, official release verified against the published checksums) under NSSM as `NT AUTHORITY\LocalService`, start type Automatic, depends on `pjsk-backend`. It listens on **:8081** (port 80 is held by this machine's IIS/W3SVC — likely used by Siemens tooling — and was deliberately left untouched), serves the production frontend build from `D:\PJSK-Deploy\frontend` with SPA history fallback, and proxies only `/api/*` and `/health` to `127.0.0.1:8080` (`/admin/*` is a frontend route, never proxied). Config: `D:\PJSK-Service\caddy\Caddyfile` (outside the repo; sanitized template at `deploy/windows-service/Caddyfile.http-lan.example`); logs: `D:\PJSK-Runtime\logs\caddy`. Firewall: one rule "PJSK Caddy HTTP LAN" — Inbound/Allow/TCP 8081, Private profile, LocalSubnet only, bound to caddy.exe. Entry points: LAN `http://192.168.1.10:8081/`, loopback `http://127.0.0.1:8081/`, proxied health `http://192.168.1.10:8081/health`. Acceptance passed (homepage, static assets, SPA fallback for `/admin/orders` and `/query`, `/api/config` 200 JSON, unauthenticated admin API 401, no directory listing, no dev-server markers) plus stop/restart/kill-recovery reliability tests. Cross-device access **has been verified via a Windows mobile hotspot** (phone opened the gateway homepage and `/health` successfully); the original CMCC-4cXx Wi-Fi could not be used for device-to-device access due to suspected client/AP isolation (phone got `ERR_ADDRESS_UNREACHABLE`, PC could not ping the phone either — a network-side limitation, not a Caddy/firewall issue; router left untouched). A real full-machine reboot test passed (2026-07-16): with no manual intervention, `postgresql-x64-18`, `pjsk-backend` and `pjsk-caddy` all came back Running/Automatic, backend stayed loopback-only on 8080, Caddy listened on 8081, all three health/homepage checks returned 200, and the phone reached the gateway again via the Windows mobile hotspot. PostgreSQL 5432 loopback hardening is **resolved (2026-07-16)**: PostgreSQL now listens only on `127.0.0.1:5432` and `[::1]:5432` (`listen_addresses = '127.0.0.1, ::1'`), so the database is no longer directly exposed on LAN interfaces; `pjsk-backend` continues to connect via `::1`, and `pg_hba.conf` was not changed. Separate pending risk: Windows Firewall Domain/Private/Public profiles were observed OFF during the PostgreSQL hardening investigation; that remains an independent follow-up and was not changed by this listener work. Still pending: internal DNS/domain, HTTPS, internal-CA trust, Caddy upgrade/rollback drills, and full manual UAT of business pages. Evidence log: [docs/development-logs/2026-07-16-internal-caddy-deployment.md](docs/development-logs/2026-07-16-internal-caddy-deployment.md) and [docs/development-logs/2026-07-16-postgresql-listen-address-hardening.md](docs/development-logs/2026-07-16-postgresql-listen-address-hardening.md).
   - Windows service deployment: **the backend is now live as a Windows service (2026-07-16)**. Service `pjsk-backend` (display name "PJSK Goods Manager Backend") runs `D:\pjsk\backend\bin\pjsk-backend.exe` under **NSSM 2.24-101-g897c7ad** (`D:\PJSK-Service\backend\nssm.exe`, outside the repo) as `NT AUTHORITY\LocalService`, start type Auto, depends on `postgresql-x64-18`; health check at `http://127.0.0.1:8080/health`; stdout/stderr in `D:\PJSK-Runtime\logs\backend\backend-{out,err}.log` (Go stdlib logging goes to stderr, so an empty out log is normal); config is loaded from the ACL-restricted, gitignored `backend\.env` (LocalService has read-only access) — no secrets live in the NSSM registry parameters. WinSW was tried first and failed under the restricted account (WinSW v3-series binary, file version 3.0.0.0: `Failed to open the service. Access denied` in service mode, before the backend child ever started — see winsw/winsw#872); the old WinSW wrapper/XML/wrapper.log are kept **outside the repo as forensic evidence only** — never start the old WinSW wrapper while the NSSM service exists (both would fight over port 8080). Stop/start/restart and kill-recovery acceptance all passed (NSSM auto-restarts the backend ~5 s after an abnormal exit). To upgrade the backend: `Stop-Service pjsk-backend` → back up the current exe (timestamped copy) → build and swap in the new exe → `Start-Service pjsk-backend` → verify `/health`. Full evidence log: [docs/development-logs/2026-07-16-windows-service-deployment.md](docs/development-logs/2026-07-16-windows-service-deployment.md). Auto-start after a real full-machine reboot was verified on 2026-07-16 (backend came back Running with no manual action). Still pending: a dedicated service account decision (LocalService in use today). The Caddy gateway service (`pjsk-caddy`) was deployed later the same day — see the reverse-proxy bullet above; never start the old WinSW wrapper, and note both services restart their apps via NSSM (~5 s after an abnormal exit).

## 16. High-Risk Operations — Read Before Touching

- **Never modify a `payments` row directly.** The void-then-recreate flow is the only sanctioned correction path; it exists specifically so there's always an audit trail. Bypassing it (e.g. a manual `UPDATE`) breaks the paid-amount recalculation and the audit history.
- **Never rename an already-applied migration file** or hand-edit `schema_migrations`. The runner treats the filename as the identity of the migration; changing it re-runs (or skips) migrations unexpectedly.
- **Don't run destructive SQL against a database with real data** without a fresh backup and explicit sign-off — `backups/` and `pjsk-data-backup/` exist for a reason, treat them as important, not disposable.
- **`idempotency_key` matters for payment creation** — don't strip or auto-generate it client-side in a way that could collide across different real payments, or duplicate-submission protection breaks.
- **CORS / cookie settings**: `CORS_ALLOWED_ORIGINS` is now configurable (production defaults to no cross-origin access; development/test keep the two local Vite origins) — the old "hardcoded to localhost in `Load()`" note no longer applies. For a reverse-proxy deployment serve frontend and API from the same origin (leave `CORS_ALLOWED_ORIGINS` empty) and set `ADMIN_COOKIE_SECURE=true` under HTTPS. See [docs/internal-https-reverse-proxy.md](docs/internal-https-reverse-proxy.md) and [docs/internal-network-deployment.md](docs/internal-network-deployment.md).

## Admin authentication security audit

- Admin authentication events are recorded in `admin_auth_audit_events` via migration `0019_admin_auth_audit_events.sql`.
- Covered events: login success, login failure, login rate-limited, and logout success.
- Successful login writes the session, `last_login_at`, and audit row in one transaction. If that audit write fails, no session cookie is issued.
- Failed login, rate-limited login, and logout audits are best-effort and preserve existing user-facing responses; logs use sanitized error categories.
- Audit storage intentionally excludes passwords, password hashes, cookies, session tokens, Authorization headers, query/binding/recovery codes, DSNs, and environment secret values.
- No admin audit query UI/API exists yet; future work can add read-only paginated access and a retention policy.
