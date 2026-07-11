alter table import_batches
	add column if not exists warnings_accepted boolean not null default false;

create index if not exists import_batches_created_at_index
	on import_batches(created_at);
