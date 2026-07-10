create table if not exists import_batches (
	id uuid primary key default gen_random_uuid(),
	project_id uuid references projects(id) on delete set null,
	original_filename varchar(255) not null,
	file_hash varchar(64) not null,
	file_size bigint,
	sheet_count integer,
	total_rows integer not null default 0,
	success_rows integer not null default 0,
	failed_rows integer not null default 0,
	status varchar(30) not null default 'pending',
	imported_by uuid references admins(id) on delete set null,
	started_at timestamptz,
	completed_at timestamptz,
	created_at timestamptz not null default now(),

	constraint import_batches_file_size_nonnegative
		check (file_size is null or file_size >= 0),

	constraint import_batches_row_counts_nonnegative
		check (
			total_rows >= 0
			and success_rows >= 0
			and failed_rows >= 0
		),

	constraint import_batches_status_valid
		check (status in ('pending', 'processing', 'completed', 'partial', 'failed'))
);

create unique index if not exists import_batches_file_hash_unique
	on import_batches(file_hash);

create index if not exists import_batches_project_id_index
	on import_batches(project_id);

create index if not exists import_batches_status_index
	on import_batches(status);

create table if not exists import_errors (
	id uuid primary key default gen_random_uuid(),
	import_batch_id uuid not null references import_batches(id) on delete cascade,
	sheet_name varchar(255),
	row_number integer,
	column_name varchar(255),
	error_code varchar(100),
	error_message text not null,
	raw_data jsonb,
	created_at timestamptz not null default now(),

	constraint import_errors_row_number_positive
		check (row_number is null or row_number > 0)
);

create index if not exists import_errors_batch_id_index
	on import_errors(import_batch_id);

do $$
begin
	if not exists (
		select 1
		from pg_constraint
		where conname = 'order_items_import_batch_id_fkey'
	) then
		alter table order_items
			add constraint order_items_import_batch_id_fkey
			foreign key (import_batch_id)
			references import_batches(id)
			on delete set null;
	end if;
end
$$;
