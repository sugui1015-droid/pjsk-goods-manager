-- 0022: system owner role and admin security recovery.
--
-- Adds the owner role on top of the existing admins table, database-level
-- guarantees that there is at most one owner and that the last owner can
-- never be deleted, disabled, or demoted, per-admin recovery email storage
-- (AES-GCM ciphertext plus HMAC lookup hash, never plaintext), one-time
-- recovery codes (raw HMAC-SHA256 digests only, batch-revoked), the
-- re-authentication timestamp on admin sessions, and the extended admin auth
-- audit event vocabulary. No plaintext secret, password, code, or email ever
-- lands in these tables.

-- 1) Role vocabulary and the at-most-one-owner guarantee.
alter table admins
	add constraint admins_role_check check (role in ('admin', 'owner'));

create unique index if not exists admins_single_owner_unique
	on admins ((true))
	where role = 'owner';

-- 2) The last active owner can never be removed, disabled, or demoted.
--    This is a database-level DEFERRED constraint trigger so every path —
--    SQL console, CLI, and any future admin management API — hits the same
--    wall, while a future owner transfer can still demote the old owner and
--    promote the new one inside a single transaction: the check runs at
--    commit, when the final state must again contain an active owner. Before
--    the first owner is promoted no row matches the trigger's condition, so
--    the bootstrap zero-owner state stays legal.
create or replace function admins_protect_last_owner() returns trigger
language plpgsql
as $$
begin
	if old.role = 'owner' and old.status = 'active' then
		if not exists (
			select 1 from admins where role = 'owner' and status = 'active'
		) then
			raise exception 'cannot remove, disable, or demote the last active owner account';
		end if;
	end if;
	return null;
end;
$$;

drop trigger if exists admins_protect_last_owner_trigger on admins;
create constraint trigger admins_protect_last_owner_trigger
	after update or delete on admins
	deferrable initially deferred
	for each row execute function admins_protect_last_owner();

-- 3) Admin recovery emails: ciphertext plus lookup hash, mirroring the user
--    recovery email design but under an admin-specific AAD in the backend.
create table if not exists admin_recovery_emails (
	admin_id uuid primary key references admins(id) on delete cascade,
	email_encrypted bytea not null,
	email_hash char(64) not null,
	status text not null default 'pending' check (status in ('pending', 'verified')),
	verified_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

-- 4) Short-lived email verification codes for binding and password reset.
--    Only HMAC hex digests are stored; attempts are counted to allow
--    lockout without keeping any plaintext.
create table if not exists admin_recovery_email_codes (
	id uuid primary key default gen_random_uuid(),
	admin_id uuid not null references admins(id) on delete cascade,
	purpose text not null check (purpose in ('bind', 'reset')),
	code_hash char(64) not null,
	expires_at timestamptz not null,
	consumed_at timestamptz,
	attempt_count integer not null default 0 check (attempt_count >= 0),
	created_at timestamptz not null default now()
);

create index if not exists admin_recovery_email_codes_admin_purpose_index
	on admin_recovery_email_codes(admin_id, purpose, created_at desc);

-- 5) One-time recovery codes. Raw HMAC-SHA256 digests (bytea), one row per
--    code, whole batches revoked when a new batch is generated, and each
--    code single-use via used_at.
create table if not exists admin_recovery_codes (
	id uuid primary key default gen_random_uuid(),
	admin_id uuid not null references admins(id) on delete cascade,
	code_hash bytea not null unique check (octet_length(code_hash) = 32),
	batch_id uuid not null,
	used_at timestamptz,
	revoked_at timestamptz,
	created_at timestamptz not null default now()
);

create index if not exists admin_recovery_codes_admin_index
	on admin_recovery_codes(admin_id, batch_id);

-- 6) Fresh re-authentication timestamp for high-risk operations.
alter table admin_sessions
	add column if not exists reauth_at timestamptz;

-- 7) Extended admin auth audit vocabulary. The 0019 inline checks are closed
--    lists, so they are dropped and recreated with the new members. Existing
--    rows all use the original vocabulary and remain valid.
alter table admin_auth_audit_events
	drop constraint if exists admin_auth_audit_events_event_type_check;
alter table admin_auth_audit_events
	add constraint admin_auth_audit_events_event_type_check check (event_type in (
		'admin_login_succeeded',
		'admin_login_failed',
		'admin_login_rate_limited',
		'admin_logout_succeeded',
		'admin_reauth_succeeded',
		'admin_reauth_failed',
		'admin_password_changed',
		'admin_recovery_email_bound',
		'admin_recovery_email_bind_failed',
		'admin_recovery_codes_generated',
		'admin_recovery_code_reset_succeeded',
		'admin_recovery_code_reset_failed',
		'admin_recovery_email_reset_succeeded',
		'admin_recovery_email_reset_failed',
		'owner_promoted',
		'owner_cli_password_reset'
	));

alter table admin_auth_audit_events
	drop constraint if exists admin_auth_audit_events_reason_code_check;
alter table admin_auth_audit_events
	add constraint admin_auth_audit_events_reason_code_check check (reason_code in (
		'none',
		'invalid_credentials',
		'account_disabled',
		'rate_limited',
		'database_error',
		'audit_write_error',
		'invalid_recovery_code',
		'invalid_verification_code',
		'verification_code_expired',
		'recovery_email_not_configured',
		'email_delivery_disabled',
		'weak_password',
		'not_owner',
		'validation_failed'
	));
