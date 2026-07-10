-- Run this once in Supabase SQL Editor.
-- Keep SUPABASE_SERVICE_ROLE_KEY in Streamlit secrets, not in this repository.

insert into storage.buckets (id, name, public)
values ('pjsk', 'pjsk', false)
on conflict (id) do nothing;

create table if not exists public.records (
    record_id text primary key,
    cn text,
    item_name text,
    role text,
    batch text,
    quantity numeric default 0,
    unit_price numeric default 0,
    amount numeric default 0,
    collected boolean default false,
    source_sheet text,
    source_file text
);

create table if not exists public.payment_records (
    payment_id text primary key,
    cn text,
    item_list text,
    amount numeric default 0,
    method text,
    note text,
    image_path text,
    approved boolean default false,
    approved_at text,
    created_at text,
    updated_at text
);

alter table public.payment_records
    add column if not exists approved boolean default false;

alter table public.payment_records
    add column if not exists approved_at text;