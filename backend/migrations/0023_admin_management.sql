-- 0023: owner-managed administrator accounts.
--
-- Links an admin account to at most one normal user (appointment from the
-- user list), adds the forced first-login password change for system-generated
-- temporary passwords, soft revocation (the row is kept for audit and for
-- re-appointment of the same user), and the management audit vocabulary with
-- an explicit actor column. Technical role values stay 'owner'/'admin'; no
-- existing business data is rewritten. Every statement is idempotent so the
-- migration can be re-applied safely, and every new column is nullable or
-- defaulted so a rolled-back release keeps running against this schema.

-- 1) Admin ↔ user link. UNIQUE keeps one admin account per user; revocation
--    keeps the link so re-appointing the same user reactivates the same
--    account instead of minting a second one.
alter table admins
	add column if not exists user_id uuid unique references users(id) on delete set null;

-- 2) Forced first-login password change for system-generated temp passwords.
alter table admins
	add column if not exists must_change_password boolean not null default false;

-- 3) Soft revocation bookkeeping.
alter table admins
	add column if not exists revoked_at timestamptz;
alter table admins
	add column if not exists revoked_by uuid references admins(id) on delete set null;

-- 4) Status vocabulary gains 'revoked'. Revoked accounts can never log in
--    (every session path requires status = 'active') but stay on record.
alter table admins
	drop constraint if exists admins_status_check;
alter table admins
	add constraint admins_status_check check (status in ('active', 'disabled', 'revoked'));

-- 5) Management audit: the acting owner, and an optional operator-entered
--    reason (sanitized, bounded, never secret material).
alter table admin_auth_audit_events
	add column if not exists actor_admin_id uuid references admins(id) on delete set null;
alter table admin_auth_audit_events
	add column if not exists management_reason text
		check (management_reason is null or char_length(management_reason) <= 256);

create index if not exists admin_auth_audit_events_actor_time_index
	on admin_auth_audit_events(actor_admin_id, occurred_at desc);

-- 6) Extended event vocabulary: the five owner management actions.
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
		'owner_cli_password_reset',
		'admin_appointed',
		'admin_revoked',
		'admin_enabled',
		'admin_disabled',
		'admin_password_reset_by_owner'
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
		'validation_failed',
		'target_is_owner',
		'user_not_found',
		'username_taken',
		'user_already_admin'
	));
