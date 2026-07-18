-- Static payment collection QR codes (alipay / wechat).
--
-- Storage model: the image bytes live in the database as bytea so they are
-- captured by the existing full-database pg_dump logical backup and migrate to
-- a Linux host via pg_restore with no separate file-sync step. There are only
-- ever two small (<=5 MiB) images, so blob bloat is irrelevant.
--
-- History model: rows are immutable in content. Uploading a code inserts a new
-- enabled row; replacing a code disables the current enabled row (enabled=false,
-- disabled_at/disabled_by set) and inserts a new enabled row in one transaction;
-- disabling flips the enabled row to disabled. Old images are never physically
-- deleted, preserving audit history. created_by/created_at answer "who configured
-- this code and when"; disabled_by/disabled_at record the retirement.
--
-- "At most one active code per method" is enforced by the partial unique index
-- payment_qr_codes_active_method_unique, which also serves as the concurrency
-- backstop: two concurrent inserts of enabled=true for the same method cannot
-- both commit. This table intentionally stores no server file path and no
-- user-controlled filename. It leaves room for a future dynamic-QR feature
-- (e.g. a later code_type column) without implementing one now.

create table if not exists payment_qr_codes (
	id             uuid primary key default gen_random_uuid(),
	payment_method text not null check (payment_method in ('alipay', 'wechat')),
	image_data     bytea not null,
	mime_type      text not null check (mime_type in ('image/png', 'image/jpeg', 'image/webp')),
	byte_size      integer not null check (byte_size > 0 and byte_size <= 5242880),
	sha256         text not null check (char_length(sha256) = 64),
	enabled        boolean not null default true,
	created_by     uuid references admins(id) on delete set null,
	created_at     timestamptz not null default now(),
	disabled_by    uuid references admins(id) on delete set null,
	disabled_at    timestamptz,
	constraint payment_qr_codes_disabled_consistency check (
		(enabled = true and disabled_at is null and disabled_by is null)
		or (enabled = false and disabled_at is not null)
	)
);

-- At most one enabled (currently effective) code per payment method.
create unique index if not exists payment_qr_codes_active_method_unique
	on payment_qr_codes(payment_method)
	where enabled = true;

-- History lookups by method, newest first.
create index if not exists payment_qr_codes_method_created_idx
	on payment_qr_codes(payment_method, created_at desc);
