alter table payments
    add column if not exists voided_at timestamptz,
    add column if not exists voided_by_admin_id uuid references admins(id) on delete set null,
    add column if not exists void_reason text;

alter table payments
    drop constraint if exists payments_status_check;

alter table payments
    add constraint payments_status_check
        check (status in ('submitted', 'approved', 'rejected', 'cancelled', 'voided'));

create index if not exists payments_voided_by_admin_id_index
    on payments(voided_by_admin_id);
