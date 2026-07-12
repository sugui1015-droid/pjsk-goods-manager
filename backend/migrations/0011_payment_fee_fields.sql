-- Add fee_amount and payable_amount to payments (idempotent).
-- submitted_amount is the order principal.
-- payable_amount is the amount actually paid by the user.

alter table payments
    add column if not exists fee_amount numeric(12,2),
    add column if not exists payable_amount numeric(12,2);

update payments
set
    fee_amount = 0,
    payable_amount = submitted_amount
where fee_amount is null
   or payable_amount is null;

alter table payments
    alter column fee_amount set not null,
    alter column payable_amount set not null;

do $$
begin
    if not exists (select 1 from pg_constraint where conname = 'payments_fee_amount_check' and conrelid = 'payments'::regclass) then
        alter table payments
            add constraint payments_fee_amount_check
                check (fee_amount >= 0);
    end if;
    if not exists (select 1 from pg_constraint where conname = 'payments_payable_amount_check' and conrelid = 'payments'::regclass) then
        alter table payments
            add constraint payments_payable_amount_check
                check (payable_amount = submitted_amount + fee_amount);
    end if;
end;
$$;
