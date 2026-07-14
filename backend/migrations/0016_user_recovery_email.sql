-- Recovery email foundation. This migration creates storage and audit
-- structures only; it does not create email records for existing users.

create table if not exists user_recovery_emails (
	id uuid primary key default gen_random_uuid(),
	user_id uuid not null references users(id) on delete cascade,
	encrypted_email bytea not null,
	email_lookup_hash char(64) not null,
	status text not null default 'pending' check (status in ('pending', 'verified', 'disabled')),
	verified_at timestamptz,
	created_by_admin_id uuid references admins(id) on delete set null,
	updated_by_admin_id uuid references admins(id) on delete set null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	invalidated_at timestamptz,
	constraint recovery_email_pending_unverified check (status <> 'pending' or verified_at is null)
);

create unique index if not exists user_recovery_emails_one_current_per_user
	on user_recovery_emails(user_id)
	where invalidated_at is null;

create index if not exists user_recovery_emails_user_id_index
	on user_recovery_emails(user_id);

create index if not exists user_recovery_emails_lookup_hash_index
	on user_recovery_emails(email_lookup_hash);

create index if not exists user_recovery_emails_status_index
	on user_recovery_emails(status);

create index if not exists user_recovery_emails_invalidated_at_index
	on user_recovery_emails(invalidated_at);

create table if not exists account_security_audit_logs (
	id uuid primary key default gen_random_uuid(),
	actor_type text not null check (actor_type in ('admin', 'user', 'system')),
	admin_id uuid references admins(id) on delete set null,
	target_user_id uuid not null references users(id) on delete cascade,
	action text not null,
	result text not null check (result in ('success', 'failure')),
	reason text,
	metadata jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now()
);

create index if not exists account_security_audit_target_user_index
	on account_security_audit_logs(target_user_id, created_at desc);

create index if not exists account_security_audit_admin_index
	on account_security_audit_logs(admin_id, created_at desc);

create index if not exists account_security_audit_action_index
	on account_security_audit_logs(action, created_at desc);
