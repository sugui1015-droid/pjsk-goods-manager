# Normal Use Roadmap

The app shell is now runnable. To become a real replacement for the Streamlit version, the next steps should happen in this order.

## 1. Local Running Baseline

Goal: frontend and backend both run locally.

Status:

- `frontend\run.cmd` starts the Vue app on `http://localhost:5173`.
- `backend\run.cmd` starts the Go API on `http://localhost:8080`.
- `GET /health` returns `ok`.
- `GET /api/config` feeds the frontend status panel.

## 2. Database Baseline

Goal: Supabase PostgreSQL has the new tables before business APIs are written.

Needed:

- Create `backend/migrations`.
- Add tables for admins, batches, goods_records, payments, payment_items, import_jobs, and audit_logs.
- Keep the old `records` and `payment_records` untouched until migration is verified.
- Put real `DATABASE_URL` and Supabase values in a local `.env`, never in Git.

## 3. Admin Login

Goal: one admin can log in and call protected APIs.

Needed:

- Seed first admin.
- Store password hashes with bcrypt.
- Issue JWT tokens from Go.
- Add Vue login view and token storage.

## 4. Goods Query

Goal: normal users can query their own goods.

Needed:

- Use `CN + query code` first.
- Backend validates query code hash.
- Frontend shows goods records with filtering and selection.

## 5. Payment Draft And Review

Goal: users can submit payment proof and admins can approve or reject it.

Needed:

- Create payment from selected goods records.
- Backend recalculates total amount.
- Upload screenshots to Supabase Storage.
- Admin approval updates payments and related goods_records in one transaction.

## 6. Excel Import

Goal: Go parser matches legacy Python parser before replacing import flow.

Needed:

- Keep using `testdata/excel/manifest.csv` as expected results.
- Implement Go parser with Excelize.
- Compare parsed row count, CN count, sheet count, and total amount against legacy parser.
- Only then connect import to database writes.

## Current User Flow

Right now you can open `http://localhost:5173` and use the new shell/status page. It is not yet a full business replacement. The old Streamlit app remains the production fallback until database, login, query, and payment review are connected.
