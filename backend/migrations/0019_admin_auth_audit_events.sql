-- Admin authentication security audit events.
-- This table is dedicated to admin auth flows and intentionally stores no
-- passwords, password hashes, cookies, session tokens, authorization headers,
-- request bodies, or raw database errors.

create table if not exists admin_auth_audit_events (
	id uuid primary key default gen_random_uuid(),
	event_type text not null check (event_type in (
		'admin_login_succeeded',
		'admin_login_failed',
		'admin_login_rate_limited',
		'admin_logout_succeeded'
	)),
	occurred_at timestamptz not null default now(),
	admin_id uuid references admins(id) on delete set null,
	username_normalized text not null check (char_length(username_normalized) <= 128),
	client_ip text not null check (char_length(client_ip) <= 128),
	result text not null check (result in ('success', 'failure')),
	reason_code text not null check (reason_code in (
		'none',
		'invalid_credentials',
		'account_disabled',
		'rate_limited',
		'database_error',
		'audit_write_error'
	)),
	user_agent_summary text check (user_agent_summary is null or char_length(user_agent_summary) <= 256),
	created_at timestamptz not null default now(),
	constraint admin_auth_audit_reason_result check (
		(result = 'success' and reason_code = 'none')
		or (result = 'failure' and reason_code <> 'none')
	)
);

create index if not exists admin_auth_audit_events_occurred_at_index
	on admin_auth_audit_events(occurred_at desc);

create index if not exists admin_auth_audit_events_type_time_index
	on admin_auth_audit_events(event_type, occurred_at desc);

create index if not exists admin_auth_audit_events_admin_time_index
	on admin_auth_audit_events(admin_id, occurred_at desc);

create index if not exists admin_auth_audit_events_username_time_index
	on admin_auth_audit_events(username_normalized, occurred_at desc);