-- Recovery-email verification challenges for signed-in users. This
-- migration creates storage only; it does not create challenges or change
-- any existing recovery-email status.

create table if not exists recovery_email_verification_codes (
	id uuid primary key default gen_random_uuid(),
	user_id uuid not null references users(id) on delete cascade,
	recovery_email_id uuid not null references user_recovery_emails(id) on delete cascade,
	code_hash char(64) not null,
	status text not null default 'sending' check (
		status in ('sending', 'active', 'used', 'locked', 'expired', 'invalidated', 'delivery_failed')
	),
	expires_at timestamptz not null,
	attempt_count integer not null default 0 check (attempt_count >= 0),
	max_attempts integer not null default 5 check (max_attempts > 0),
	sent_at timestamptz,
	used_at timestamptz,
	invalidated_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint recovery_email_verification_active_sent check (status <> 'active' or sent_at is not null),
	constraint recovery_email_verification_used_at check (status <> 'used' or used_at is not null)
);

create unique index if not exists recovery_email_verification_one_inflight_per_user
	on recovery_email_verification_codes(user_id)
	where status in ('sending', 'active') and invalidated_at is null;

create index if not exists recovery_email_verification_user_created_index
	on recovery_email_verification_codes(user_id, created_at desc);

create index if not exists recovery_email_verification_email_created_index
	on recovery_email_verification_codes(recovery_email_id, created_at desc);

create index if not exists recovery_email_verification_expires_index
	on recovery_email_verification_codes(expires_at)
	where status in ('sending', 'active') and invalidated_at is null;
