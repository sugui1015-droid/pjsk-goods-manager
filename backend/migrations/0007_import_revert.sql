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
            'cancelled',
            'reverted'
        ));

alter table import_batches
    add column if not exists revoked_by uuid references admins(id) on delete set null,
    add column if not exists revoked_at timestamptz,
    add column if not exists revoke_result jsonb;

alter table order_items
    add column if not exists revoked_by uuid references admins(id) on delete set null,
    add column if not exists revoked_at timestamptz,
    add column if not exists revoke_reason text;

create index if not exists import_batches_revoked_at_index
    on import_batches(revoked_at);

create index if not exists order_items_import_batch_active_index
    on order_items(import_batch_id)
    where revoked_at is null;
