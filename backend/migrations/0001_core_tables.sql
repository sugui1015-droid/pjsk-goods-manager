create extension if not exists pgcrypto;

create table if not exists admins (
	id uuid primary key default gen_random_uuid(),
	username text not null unique,
	password_hash text not null,
	display_name text,
	role text not null default 'admin',
	status text not null default 'active' check (status in ('active', 'disabled')),
	last_login_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists users (
	id uuid primary key default gen_random_uuid(),
	cn_code text not null unique,
	display_name text,
	query_code_hash text,
	status text not null default 'active' check (status in ('active', 'disabled')),
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists projects (
	id uuid primary key default gen_random_uuid(),
	code text not null unique,
	name text not null,
	description text,
	status text not null default 'draft' check (status in ('draft', 'active', 'archived')),
	opened_at timestamptz,
	closed_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists products (
	id uuid primary key default gen_random_uuid(),
	project_id uuid not null references projects(id) on delete cascade,
	sku text,
	name text not null,
	character_name text,
	category text,
	unit_price numeric(12,2) not null default 0 check (unit_price >= 0),
	currency text not null default 'CNY',
	status text not null default 'active' check (status in ('active', 'inactive')),
	sort_order integer not null default 0,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (project_id, sku)
);

create table if not exists orders (
	id uuid primary key default gen_random_uuid(),
	project_id uuid not null references projects(id),
	user_id uuid not null references users(id),
	order_no text not null unique,
	status text not null default 'draft' check (status in ('draft', 'submitted', 'partially_paid', 'paid', 'cancelled')),
	total_amount numeric(12,2) not null default 0 check (total_amount >= 0),
	note text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists order_items (
	id uuid primary key default gen_random_uuid(),
	order_id uuid not null references orders(id) on delete cascade,
	product_id uuid not null references products(id),
	quantity numeric(12,2) not null check (quantity > 0),
	unit_price numeric(12,2) not null check (unit_price >= 0),
	amount numeric(12,2) not null check (amount >= 0),
	payment_status text not null default 'unpaid' check (payment_status in ('unpaid', 'partial', 'paid')),
	import_batch_id uuid,
	source_sheet text,
	source_row_key text,
	legacy_record_id text unique,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists payments (
	id uuid primary key default gen_random_uuid(),
	user_id uuid not null references users(id),
	submitted_amount numeric(12,2) not null check (submitted_amount >= 0),
	payment_method text,
	screenshot_storage_path text,
	note text,
	status text not null default 'submitted' check (status in ('submitted', 'approved', 'rejected', 'cancelled')),
	submitted_at timestamptz not null default now(),
	approved_at timestamptz,
	approved_by uuid references admins(id),
	rejected_at timestamptz,
	rejected_by uuid references admins(id),
	rejection_reason text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists payment_items (
	id uuid primary key default gen_random_uuid(),
	payment_id uuid not null references payments(id) on delete cascade,
	order_item_id uuid not null references order_items(id),
	applied_amount numeric(12,2) not null check (applied_amount >= 0),
	created_at timestamptz not null default now(),
	unique (payment_id, order_item_id)
);

create index if not exists idx_products_project_id on products(project_id);
create index if not exists idx_orders_project_id on orders(project_id);
create index if not exists idx_orders_user_id on orders(user_id);
create index if not exists idx_order_items_order_id on order_items(order_id);
create index if not exists idx_order_items_product_id on order_items(product_id);
create index if not exists idx_payments_user_id on payments(user_id);
create index if not exists idx_payments_status on payments(status);
create index if not exists idx_payment_items_payment_id on payment_items(payment_id);
create index if not exists idx_payment_items_order_item_id on payment_items(order_item_id);
