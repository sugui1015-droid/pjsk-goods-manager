alter table payments
    add column if not exists paid_at timestamptz,
    add column if not exists created_by uuid references admins(id) on delete set null,
    add column if not exists idempotency_key text;

update payments
set paid_at = coalesce(paid_at, approved_at, submitted_at)
where paid_at is null;

create unique index if not exists payments_idempotency_key_unique
    on payments(idempotency_key)
    where idempotency_key is not null;

create index if not exists payment_items_order_item_active_index
    on payment_items(order_item_id);
