-- Lightweight user feedback MVP with plain text and the five approved fields.

create table if not exists feedbacks (
	id uuid primary key default gen_random_uuid(),
	user_id uuid not null references users(id),
	content text not null
		check (char_length(btrim(content)) between 1 and 1000),
	created_at timestamptz not null default now(),
	status text not null default 'new'
		check (status in ('new', 'processed'))
);

create index if not exists feedbacks_status_created_at_index
	on feedbacks (status, created_at desc);

create index if not exists feedbacks_user_created_at_index
	on feedbacks (user_id, created_at desc);
