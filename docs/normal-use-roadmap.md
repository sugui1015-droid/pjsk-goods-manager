# Normal Use Roadmap

This reflects the actual state of the code, APIs, and tests in this repository as of this update — not the original plan. See [HANDOVER.md](../HANDOVER.md) for a fuller technical handover.

## Done

- **Admin login and session** — `POST /api/admin/login`, `GET /api/admin/me`, `POST /api/admin/logout` in [backend/internal/admin](../backend/internal/admin). HttpOnly session cookie, token hash stored in PostgreSQL.
- **Excel preview and confirm import** — `POST /api/admin/imports/preview`, `POST /api/admin/imports/confirm` in [backend/internal/importpreview](../backend/internal/importpreview), matrix-summary and detail-table parsing per [docs/excel-import-rules.md](excel-import-rules.md).
- **Import history and import revert** — `GET /api/admin/imports`, `GET /api/admin/imports/{id}`, revert support (migration `0007_import_revert.sql`).
- **Admin order query** — `GET /api/admin/orders`, `GET /api/admin/orders/{id}` in [backend/internal/orders](../backend/internal/orders).
- **Regular user CN + query-code lookup** — `POST /api/query/login`, `GET /api/query/orders`, `POST /api/query/logout` in [backend/internal/query](../backend/internal/query). Query code is bcrypt-hashed; session cookie separate from admin session.
- **Admin unpaid-balance query** — `GET /api/admin/payments/unpaid?cn=`, `GET /api/admin/payments/cn?cn=` in [backend/internal/payments](../backend/internal/payments).
- **Manual payment entry** — `POST /api/admin/payments`, created directly as `approved`, idempotency-key protected.
- **WeChat fee handling** — 0.1% of principal, rounded up to the cent (see [docs/payment-workflow.md](payment-workflow.md)).
- **Partial payment** — one order item can be paid across multiple payments; `payment_status` recalculated per item and per order.
- **Payment detail** — `GET /api/admin/payments/{id}` returns principal/fee/total and the order items it covers.
- **Payment void and audit** — `POST /api/admin/payments/{id}/void`, requires a reason, records `voided_at` / `voided_by` / `void_reason`, rolls back item/order payment status.

## Not done / still needed

- Full admin user management (create/disable/reset other admin accounts beyond `cmd/create-admin`).
- CN merge (combining duplicate CN records for the same real person).
- Diff-based re-import (importing an updated sheet and only applying the delta instead of a fresh batch).
- Backend maintenance UI for the character (`role`) and category dictionaries — corrections currently happen through import rules, not a standalone admin screen.
- Data export (CSV/Excel export of orders, payments, or query results).
- A unified admin audit log across modules (payments has void audit fields; imports/orders/admin actions do not share one audit log table yet).
- Role-based permission tiers — every authenticated admin currently has the same access.
- Frontend component and route decomposition — [frontend/src/App.vue](../frontend/src/App.vue) is a single large component with hand-rolled path routing; no `vue-router`.
- CI (no automated pipeline runs `go test` / `pnpm run build` on push yet).
- Production deployment, domain, HTTPS, and backup strategy.

## Explicitly out of scope for now

- User-submitted payment screenshots and a `submitted / approved / rejected` review flow — see [docs/payment-workflow.md](payment-workflow.md) for why this is listed as a future option, not a current feature.
