alter table import_batches
	drop constraint if exists import_batches_status_valid;

alter table import_batches
	add constraint import_batches_status_valid
		check (status in (
			'pending',
			'processing',
			'previewed',
			'confirmed',
			'completed',
			'partial',
			'failed',
			'cancelled'
		));

alter table import_batches
	add column if not exists preview_payload jsonb,
	add column if not exists error_count integer not null default 0,
	add column if not exists warning_count integer not null default 0,
	add column if not exists notice_count integer not null default 0,
	add column if not exists confirmed_by uuid references admins(id) on delete set null,
	add column if not exists confirmed_at timestamptz,
	add column if not exists confirmed_project_id uuid references projects(id) on delete set null,
	add column if not exists confirm_result jsonb;

alter table import_batches
	add constraint import_batches_issue_counts_nonnegative
	check (
		error_count >= 0
		and warning_count >= 0
		and notice_count >= 0
	);

create index if not exists import_batches_confirmed_at_index
	on import_batches(confirmed_at);
