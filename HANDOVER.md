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

Listens on `http://localhost:8080` by default (`APP_PORT` / `SERVER_PORT` / `BACKEND_PORT` env, in that priority order). Exposes `GET /health` and `GET /api/config` unauthenticated; everything else under `/api/admin/*` requires an admin session cookie, `/api/query/*` requires a query session cookie.

Frontend:

```bash
cd frontend
pnpm install   # first time only
pnpm dev
# or: frontend\run.cmd (binds --host 0.0.0.0)
```

Opens on `http://localhost:5173`. `VITE_API_BASE_URL` controls which backend it talks to.

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

Other env vars actually read by the Go backend: `APP_PORT` / `SERVER_PORT` / `BACKEND_PORT`, `ADMIN_SESSION_TTL` (Go duration string, e.g. `12h`), `ADMIN_COOKIE_SECURE` (`true`/`false`), `LEGACY_STREAMLIT_ADMIN_PORT`, `LEGACY_STREAMLIT_USER_PORT`.

There are two `.env.example` files with different shapes — see [Known Issues](#14-known-issues) below; don't assume they're interchangeable without checking `config.go`.

Never commit a real `.env`. Real credentials belong only in a local, gitignored `.env` file.

## 6. Database Migrations

`backend/internal/database/migrations.go` runs on every backend startup:

- Ensures a `schema_migrations(version text primary key, applied_at timestamptz)` table exists.
- Reads all `*.sql` files under `backend/migrations/`, **sorted by filename** (not by any embedded number parsed out separately — the whole filename is the sort key and the stored `version`).
- For each file not already present in `schema_migrations.version`, runs it inside a transaction and inserts a row keyed by the exact filename.

Current migration files: `0001_core_tables.sql` … `0012_normalize_payment_methods.sql`. There is a known numbering irregularity (two files named `0005_*`, no `0006_*`) — see [Known Issues](#14-known-issues). Do not rename existing migration files without understanding that `schema_migrations.version` is the filename itself; renaming a file makes the migration runner think it's unapplied and try to run it again.

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

- **Duplicate `0005` migration filenames**: `backend/migrations/0005_import_history.sql` and `backend/migrations/0005_product_series.sql` both exist, and there is no `0006_*` file. Because the migration runner keys `schema_migrations` on the full filename (not just the numeric prefix) and sorts by filename, both `0005_*` files are independent, distinctly-tracked migrations and can both be applied without conflict — but the numbering itself is inconsistent with the rest of the sequence. **This has not been resolved and is intentionally left untouched in this update.** Do not rename either file or renumber later migrations without a deliberate, separately-reviewed migration-numbering pass — renaming a migration file changes its `schema_migrations.version` key and will make an already-applied migration look unapplied.
- **Two mismatched `.env.example` files**: root `.env.example` uses a single `DATABASE_URL`; `backend/.env.example` uses split `DATABASE_HOST/PORT/USER/PASSWORD/NAME` plus a `JWT_SECRET` that is not actually read anywhere in the Go code (admin auth uses opaque session tokens hashed server-side, not JWTs). Follow `config.go` as the source of truth over either example file.
- **No frontend test script** — only `dev`/`build`/`preview` exist in `frontend/package.json`.
- **No CI** — `go test`, `go vet`, and `pnpm run build` are only run manually/locally today.
- **`App.vue` is a single large component** with hand-rolled routing; no `vue-router`, no component decomposition yet (tracked in the roadmap, not addressed in this update).
- See [docs/normal-use-roadmap.md](docs/normal-use-roadmap.md) "Not done / still needed" for the fuller backlog (admin user management, CN merge, diff import, role/category dictionary maintenance UI, data export, unified audit log, permission tiers, production deployment).

## 15. Next Development Order (Suggested)

Roughly in priority order, per [docs/normal-use-roadmap.md](docs/normal-use-roadmap.md):

1. Decide and execute the migration-numbering fix (see Known Issues) as its own reviewed change — likely continuing new migrations from `0013` and leaving history untouched, but that decision should be made deliberately, not implied by this document.
2. Data export (CSV/Excel) for orders/payments — high value, low risk, no schema change needed.
3. Unified admin audit log spanning imports/orders/payments, not just payment voids.
4. CN merge and diff-based re-import — both touch import correctness, so they should land with strong test coverage.
5. Role/category dictionary maintenance UI.
6. Admin user management, permission tiers.
7. Frontend decomposition (`vue-router` + component split) — do this once the API surface above has stabilized, to avoid rewriting routing twice.
8. CI pipeline (`go test`, `go vet`, `pnpm run build` on push).
9. Production deployment: domain, HTTPS, backup strategy.

## 16. High-Risk Operations — Read Before Touching

- **Never modify a `payments` row directly.** The void-then-recreate flow is the only sanctioned correction path; it exists specifically so there's always an audit trail. Bypassing it (e.g. a manual `UPDATE`) breaks the paid-amount recalculation and the audit history.
- **Never rename an already-applied migration file** or hand-edit `schema_migrations`. The runner treats the filename as the identity of the migration; changing it re-runs (or skips) migrations unexpectedly.
- **Don't run destructive SQL against a database with real data** without a fresh backup and explicit sign-off — `backups/` and `pjsk-data-backup/` exist for a reason, treat them as important, not disposable.
- **`idempotency_key` matters for payment creation** — don't strip or auto-generate it client-side in a way that could collide across different real payments, or duplicate-submission protection breaks.
- **CORS / cookie settings** (`ADMIN_COOKIE_SECURE`, `FrontendOrigins` in `config.go`) are currently hardcoded to localhost origins in `Load()`. Don't point a production frontend at a backend running this code without revisiting that.
