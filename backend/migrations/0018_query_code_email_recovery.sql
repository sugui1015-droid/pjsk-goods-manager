-- Anonymous query-code recovery by a previously verified recovery email.
-- This migration creates storage only. It does not create recovery flows,
-- modify existing query codes, or change recovery-email state.

create table if not exists query_code_recovery_request_events (
	id uuid primary key default gen_random_uuid(),
	cn_hash char(64) not null,
	ip_hash char(64) not null,
	created_at timestamptz not null default now()
);

create index if not exists query_code_recovery_request_cn_time_index
	on query_code_recovery_request_events(cn_hash, created_at desc);

create index if not exists query_code_recovery_request_ip_time_index
	on query_code_recovery_request_events(ip_hash, created_at desc);

create table if not exists query_code_recovery_codes (
	id uuid primary key default gen_random_uuid(),
	user_id uuid not null references users(id) on delete cascade,
	recovery_email_id uuid not null references user_recovery_emails(id) on delete cascade,
	purpose text not null default 'query_code_recovery' check (purpose = 'query_code_recovery'),
	code_hash char(64) not null,
	ip_hash char(64) not null,
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
	constraint query_code_recovery_code_active_sent check (status <> 'active' or sent_at is not null),
	constraint query_code_recovery_code_used_at check (status <> 'used' or used_at is not null)
);

create unique index if not exists query_code_recovery_one_inflight_per_user
	on query_code_recovery_codes(user_id, purpose)
	where status in ('sending', 'active') and invalidated_at is null;

create index if not exists query_code_recovery_code_user_time_index
	on query_code_recovery_codes(user_id, created_at desc);

create index if not exists query_code_recovery_code_email_time_index
	on query_code_recovery_codes(recovery_email_id, created_at desc);

create index if not exists query_code_recovery_code_ip_time_index
	on query_code_recovery_codes(ip_hash, created_at desc);

create index if not exists query_code_recovery_code_expiry_index
	on query_code_recovery_codes(expires_at)
	where status in ('sending', 'active') and invalidated_at is null;

create table if not exists query_code_recovery_sessions (
	id uuid primary key default gen_random_uuid(),
	user_id uuid not null references users(id) on delete cascade,
	recovery_email_id uuid not null references user_recovery_emails(id) on delete cascade,
	purpose text not null default 'query_code_recovery' check (purpose = 'query_code_recovery'),
	token_hash char(64) not null unique,
	status text not null default 'active' check (status in ('active', 'used', 'expired', 'invalidated')),
	expires_at timestamptz not null,
	used_at timestamptz,
	invalidated_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint query_code_recovery_session_used_at check (status <> 'used' or used_at is not null)
);

create unique index if not exists query_code_recovery_one_active_session_per_user
	on query_code_recovery_sessions(user_id, purpose)
	where status = 'active' and invalidated_at is null;

create index if not exists query_code_recovery_session_user_time_index
	on query_code_recovery_sessions(user_id, created_at desc);

create index if not exists query_code_recovery_session_email_time_index
	on query_code_recovery_sessions(recovery_email_id, created_at desc);

create index if not exists query_code_recovery_session_expiry_index
	on query_code_recovery_sessions(expires_at)
	where status = 'active' and invalidated_at is null;
