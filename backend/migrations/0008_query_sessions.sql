create table if not exists query_sessions (
	id uuid primary key default gen_random_uuid(),
	user_id uuid not null references users(id) on delete cascade,
	token_hash char(64) not null unique,
	expires_at timestamptz not null,
	last_used_at timestamptz,
	created_at timestamptz not null default now()
);

create index if not exists query_sessions_user_id_index
	on query_sessions(user_id);

create index if not exists query_sessions_expires_at_index
	on query_sessions(expires_at);
