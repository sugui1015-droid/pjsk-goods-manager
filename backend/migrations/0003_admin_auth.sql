create unique index if not exists admins_username_normalized_unique
	on admins (lower(btrim(username)));

create table if not exists admin_sessions (
	id uuid primary key default gen_random_uuid(),
	admin_id uuid not null references admins(id) on delete cascade,
	token_hash char(64) not null unique,
	expires_at timestamptz not null,
	last_used_at timestamptz,
	created_at timestamptz not null default now()
);

create index if not exists admin_sessions_admin_id_index
	on admin_sessions(admin_id);

create index if not exists admin_sessions_expires_at_index
	on admin_sessions(expires_at);
