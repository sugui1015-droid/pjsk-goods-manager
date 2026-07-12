-- CN merge support: allow marking a user row as merged into another user,
-- and keep a permanent audit log of every merge.

alter table users drop constraint if exists users_status_check;
alter table users add constraint users_status_check check (status in ('active', 'disabled', 'merged'));

create table if not exists cn_merge_logs (
	id uuid primary key default gen_random_uuid(),
	source_user_id uuid not null references users(id),
	target_user_id uuid not null references users(id),
	source_cn text not null,
	target_cn text not null,
	moved_order_count int not null default 0,
	moved_payment_count int not null default 0,
	reason text not null,
	merged_by uuid not null references admins(id),
	merged_at timestamptz not null default now(),
	constraint cn_merge_logs_no_self check (source_user_id <> target_user_id)
);

create index if not exists idx_cn_merge_logs_source on cn_merge_logs(source_user_id);
create index if not exists idx_cn_merge_logs_target on cn_merge_logs(target_user_id);
