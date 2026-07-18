-- User-submitted payment proof ("收肾记录").
--
-- Purpose: a user uploads a screenshot of a completed payment ("收肾记录").
-- The submission is EVIDENCE ONLY. It never, by itself, affects any paid
-- amount. Paid amounts are still computed exclusively from
-- sum(payment_items.applied_amount) filter (where payments.status='approved'),
-- so a row in this table cannot move that sum. Only an admin review that
-- creates a real approved `payments` row (via the existing record-payment
-- flow) increases the paid total. This table then links to that payment via
-- linked_payment_id.
--
-- Storage model: the image bytes live in the database as bytea (same choice as
-- payment_qr_codes) so they are captured by pg_dump and migrate to a Linux
-- host with no separate file-sync step, and there is never a server file path
-- or a user-controlled filename on disk — eliminating path traversal.
--
-- State machine (status):
--   submitted  -- 已交肾（待管理员核对）；图片校验通过且事务落库成功后才写入
--   approved   -- 核对通过；此时且仅此时已创建正式 approved payments 行
--   rejected   -- 已驳回（reject_reason 必填），用户可重新提交（新的一行）
-- "已撤销" 属于正式 payments 的生命周期，不在本表表达。
--
-- This migration is idempotent and safe to re-run.

create table if not exists payment_submissions (
	id                     uuid primary key default gen_random_uuid(),
	user_id                uuid not null references users(id) on delete cascade,
	cn_code                text not null,
	payment_method         text not null check (payment_method in ('alipay', 'wechat')),

	-- Amounts the user was shown at submission time (本金 / 手续费 / 本次应付).
	-- Kept for the admin's reference; they do NOT drive any paid total.
	principal_amount       numeric(12,2) not null check (principal_amount >= 0),
	fee_amount             numeric(12,2) not null check (fee_amount >= 0),
	payable_amount         numeric(12,2) not null check (payable_amount >= 0),

	-- Proof image stored as bytes; no filesystem path, no user filename on disk.
	image_data             bytea not null,
	mime_type              text not null check (mime_type in ('image/png', 'image/jpeg', 'image/webp')),
	byte_size              integer not null check (byte_size > 0 and byte_size <= 10485760),
	sha256                 char(64) not null check (char_length(sha256) = 64),
	original_filename_safe text not null default '',

	status                 text not null default 'submitted'
		check (status in ('submitted', 'approved', 'rejected')),
	submitted_at           timestamptz not null default now(),

	reviewed_by_admin_id   uuid references admins(id) on delete set null,
	reviewed_at            timestamptz,
	reject_reason          text,

	-- Set only when a review creates/links a real approved payment.
	linked_payment_id      uuid references payments(id) on delete set null,

	created_at             timestamptz not null default now(),
	updated_at             timestamptz not null default now(),

	-- A rejected submission must record why; a non-rejected one must not carry
	-- a stale reason.
	constraint payment_submissions_reject_reason
		check (
			(status = 'rejected' and reject_reason is not null and char_length(btrim(reject_reason)) > 0)
			or (status <> 'rejected')
		),
	-- An approved submission must link to the payment it produced.
	constraint payment_submissions_approved_link
		check (status <> 'approved' or linked_payment_id is not null)
);

create index if not exists payment_submissions_user_index
	on payment_submissions (user_id, submitted_at desc);

create index if not exists payment_submissions_status_index
	on payment_submissions (status, submitted_at desc);

create index if not exists payment_submissions_cn_index
	on payment_submissions (cn_code);

create index if not exists payment_submissions_linked_payment_index
	on payment_submissions (linked_payment_id);
