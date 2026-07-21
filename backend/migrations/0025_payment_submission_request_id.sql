-- Idempotency for user-submitted payment proofs.
--
-- Why: a proof upload is a slow request over a phone uplink (production showed
-- 10.3 s for 1.25 MB, with attempts abandoned at 24 s and 188 s). A user who
-- sees no progress retries, and every retry that reached the server used to
-- create a second 收肾记录 for the same payment — extra rows for the admin to
-- reconcile by hand.
--
-- The client generates a request_id once per submission and reuses it across
-- retries of that same submission. The unique index below makes a retry return
-- the original row instead of inserting a new one.
--
-- Scope is deliberately (user_id, request_id), NOT the image hash: two users
-- may legitimately upload the same screenshot, and one user may legitimately
-- submit the same screenshot against different orders. Only an explicit retry
-- of one specific submission is deduplicated.
--
-- request_id is nullable so existing rows and any client that does not send one
-- keep working; the partial index leaves those rows unconstrained.
--
-- This migration is idempotent and safe to re-run.

alter table payment_submissions
	add column if not exists request_id text;

do $$
begin
	if not exists (
		select 1 from pg_constraint
		where conname = 'payment_submissions_request_id_shape'
	) then
		alter table payment_submissions
			add constraint payment_submissions_request_id_shape
			check (request_id is null or (length(request_id) between 8 and 100));
	end if;
end
$$;

create unique index if not exists payment_submissions_user_request_id_key
	on payment_submissions (user_id, request_id)
	where request_id is not null;
