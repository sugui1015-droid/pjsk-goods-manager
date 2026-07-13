alter table users
	add column if not exists query_code_updated_at timestamptz,
	add column if not exists last_query_login_at timestamptz;
