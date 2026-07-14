package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"pjsk/backend/internal/config"
	"pjsk/backend/internal/database"
	"pjsk/backend/internal/logsafe"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const defaultPrefix = "QUERY_BROWSER_TEST_20260713"

type ids struct {
	UserID          string
	ProjectID       string
	OrderID         string
	ItemPartialID   string
	ItemPaidID      string
	ApprovedPayment string
	VoidedPayment   string
}

type counts struct {
	Users         int
	Projects      int
	Products      int
	Orders        int
	OrderItems    int
	Payments      int
	PaymentItems  int
	QuerySessions int
	ApprovedItems int
	VoidedItems   int
}

func main() {
	mode := flag.String("mode", "create", "create or cleanup")
	prefix := flag.String("prefix", defaultPrefix, "test data prefix")
	cn := flag.String("cn", defaultPrefix, "test CN")
	queryCode := flag.String("query-code", "", "plain query code for create mode")
	flag.Parse()

	normalizedPrefix := strings.TrimSpace(*prefix)
	if normalizedPrefix == "" {
		log.Fatal("-prefix is required")
	}
	normalizedCN := normalize(*cn)
	if normalizedCN == "" {
		log.Fatal("-cn is required")
	}
	if !strings.HasPrefix(normalizedCN, normalizedPrefix) {
		log.Fatalf("cn %q must start with prefix %q", normalizedCN, normalizedPrefix)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := database.Connect(connectCtx, cfg.DatabaseURL)
	connectCancel()
	if err != nil {
		log.Fatalf("connect to database: %s", logsafe.Category(err))
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "create":
		if strings.TrimSpace(*queryCode) == "" {
			log.Fatal("-query-code is required in create mode")
		}
		if len([]rune(*queryCode)) < 4 {
			log.Fatal("-query-code must contain at least 4 characters")
		}
		result, err := createFixture(ctx, pool, normalizedPrefix, normalizedCN, *queryCode)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("created prefix=%s cn=%s payments=%d payment_items=%d approved_payment_items=%d voided_payment_items=%d\n",
			normalizedPrefix, normalizedCN, result.Payments, result.PaymentItems, result.ApprovedItems, result.VoidedItems)
	case "cleanup":
		result, err := cleanupFixture(ctx, pool, normalizedPrefix)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("cleaned prefix=%s users=%d projects=%d products=%d orders=%d order_items=%d payments=%d payment_items=%d query_sessions=%d\n",
			normalizedPrefix, result.Users, result.Projects, result.Products, result.Orders, result.OrderItems, result.Payments, result.PaymentItems, result.QuerySessions)
	default:
		log.Fatalf("unknown -mode %q", *mode)
	}
}

func createFixture(ctx context.Context, pool *pgxpool.Pool, prefix string, cn string, queryCode string) (counts, error) {
	if _, err := cleanupFixture(ctx, pool, prefix); err != nil {
		return counts{}, err
	}

	queryHash, err := bcrypt.GenerateFromPassword([]byte(queryCode), 12)
	if err != nil {
		return counts{}, fmt.Errorf("hash query code: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return counts{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var fixture ids
	err = tx.QueryRow(ctx, `
		with u as (
			insert into users (cn_code, display_name, query_code_hash, status)
			values ($1, $2 || ' User', $3, 'active')
			returning id
		), proj as (
			insert into projects (code, name, description, status, opened_at)
			values ($2 || '_PROJECT', $2 || ' Project', 'Temporary browser acceptance project', 'active', now())
			returning id
		), product_partial as (
			insert into products (project_id, sku, name, character_name, category, series_code, unit_price, status, sort_order)
			select id, $2 || '_SKU_PARTIAL', $2 || ' Partial Item', 'miku', 'badge', $2 || '_SERIES', 10.00, 'active', 1 from proj
			returning id
		), product_paid as (
			insert into products (project_id, sku, name, character_name, category, series_code, unit_price, status, sort_order)
			select id, $2 || '_SKU_PAID', $2 || ' Paid Item', 'rin', 'card', $2 || '_SERIES', 20.00, 'active', 2 from proj
			returning id
		), o as (
			insert into orders (project_id, user_id, order_no, status, total_amount, note)
			select proj.id, u.id, $2 || '_ORDER_001', 'partially_paid', 30.00, 'Temporary browser acceptance order'
			from proj cross join u
			returning id
		), item_partial as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key, legacy_record_id)
			select o.id, product_partial.id, 1, 10.00, 10.00, 'partial', $2 || '_SHEET', $2 || '_ROW_PARTIAL', $2 || '_LEGACY_PARTIAL'
			from o cross join product_partial
			returning id
		), item_paid as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key, legacy_record_id)
			select o.id, product_paid.id, 1, 20.00, 20.00, 'paid', $2 || '_SHEET', $2 || '_ROW_PAID', $2 || '_LEGACY_PAID'
			from o cross join product_paid
			returning id
		)
		select
			(select id::text from u),
			(select id::text from proj),
			(select id::text from o),
			(select id::text from item_partial),
			(select id::text from item_paid)
	`, cn, prefix, string(queryHash)).Scan(&fixture.UserID, &fixture.ProjectID, &fixture.OrderID, &fixture.ItemPartialID, &fixture.ItemPaidID)
	if err != nil {
		return counts{}, fmt.Errorf("insert fixture core rows: %w", err)
	}

	if err := insertPayment(ctx, tx, prefix, fixture.UserID, "approved", "2026-07-13T10:00:00+08:00", prefix+"_PAYMENT_APPROVED", map[string]string{
		fixture.ItemPartialID: "4.00",
		fixture.ItemPaidID:    "20.00",
	}, &fixture.ApprovedPayment); err != nil {
		return counts{}, err
	}
	if err := insertPayment(ctx, tx, prefix, fixture.UserID, "voided", "2026-07-13T10:30:00+08:00", prefix+"_PAYMENT_VOIDED", map[string]string{
		fixture.ItemPaidID: "5.00",
	}, &fixture.VoidedPayment); err != nil {
		return counts{}, err
	}
	if _, err := tx.Exec(ctx, `
		update payments
		set voided_at = paid_at + interval '15 minutes',
			void_reason = $2,
			updated_at = now()
		where id = $1::uuid
	`, fixture.VoidedPayment, "Temporary browser acceptance voided payment"); err != nil {
		return counts{}, fmt.Errorf("mark voided payment: %w", err)
	}

	if err := verifyFixture(ctx, tx, prefix, fixture); err != nil {
		return counts{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return counts{}, err
	}

	return summarizeFixture(ctx, pool, prefix)
}

func insertPayment(ctx context.Context, tx pgx.Tx, prefix string, userID string, status string, paidAt string, key string, allocations map[string]string, paymentID *string) error {
	var totalCents int64
	for _, amount := range allocations {
		cents, err := centsFromAmount(amount)
		if err != nil {
			return err
		}
		totalCents += cents
	}
	total := fmt.Sprintf("%d.%02d", totalCents/100, totalCents%100)
	if err := tx.QueryRow(ctx, `
		insert into payments (user_id, submitted_amount, fee_amount, payable_amount, payment_method, note, status, submitted_at, approved_at, paid_at, idempotency_key)
		values ($1::uuid, $2::numeric(12,2), 0, $2::numeric(12,2), 'alipay', $3, $4, $5::timestamptz, $5::timestamptz, $5::timestamptz, $6)
		returning id::text
	`, userID, total, prefix+" browser acceptance "+status+" payment", status, paidAt, key).Scan(paymentID); err != nil {
		return fmt.Errorf("insert %s payment: %w", status, err)
	}
	for orderItemID, amount := range allocations {
		if _, err := tx.Exec(ctx, `
			insert into payment_items (payment_id, order_item_id, applied_amount)
			values ($1::uuid, $2::uuid, $3::numeric(12,2))
		`, *paymentID, orderItemID, amount); err != nil {
			return fmt.Errorf("insert %s payment item: %w", status, err)
		}
	}
	return nil
}

func verifyFixture(ctx context.Context, tx pgx.Tx, prefix string, fixture ids) error {
	var row struct {
		UserID              string
		OrderID             string
		ApprovedPaymentID   string
		ApprovedItems       int
		VoidedPaymentID     string
		VoidedItems         int
		PartialItemPaid     string
		PartialItemStatus   string
		PaidItemPaid        string
		PaidItemStatus      string
		OrderStatus         string
		QueryCodeHashExists bool
	}
	err := tx.QueryRow(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments p on p.id = pi.payment_id
			join users u on u.id = p.user_id
			where u.cn_code like $1
			group by pi.order_item_id
		)
		select
			u.id::text,
			o.id::text,
			$3::text,
			(select count(*)::int from payment_items where payment_id = $3::uuid),
			$4::text,
			(select count(*)::int from payment_items where payment_id = $4::uuid),
			coalesce((select paid_amount::text from paid_by_item where order_item_id = $5::uuid), '0'),
			(select payment_status from order_items where id = $5::uuid),
			coalesce((select paid_amount::text from paid_by_item where order_item_id = $6::uuid), '0'),
			(select payment_status from order_items where id = $6::uuid),
			o.status,
			u.query_code_hash is not null and u.query_code_hash <> ''
		from users u
		join orders o on o.user_id = u.id
		where u.cn_code like $1
		  and o.order_no = $2
	`, prefix+"%", prefix+"_ORDER_001", fixture.ApprovedPayment, fixture.VoidedPayment, fixture.ItemPartialID, fixture.ItemPaidID).Scan(
		&row.UserID,
		&row.OrderID,
		&row.ApprovedPaymentID,
		&row.ApprovedItems,
		&row.VoidedPaymentID,
		&row.VoidedItems,
		&row.PartialItemPaid,
		&row.PartialItemStatus,
		&row.PaidItemPaid,
		&row.PaidItemStatus,
		&row.OrderStatus,
		&row.QueryCodeHashExists,
	)
	if err != nil {
		return fmt.Errorf("verify fixture relations: %w", err)
	}
	if row.UserID != fixture.UserID || row.OrderID != fixture.OrderID {
		return fmt.Errorf("fixture relation mismatch: user/order ids did not round-trip")
	}
	if row.ApprovedItems != 2 || row.VoidedItems != 1 {
		return fmt.Errorf("fixture payment item counts = approved %d voided %d, want 2/1", row.ApprovedItems, row.VoidedItems)
	}
	if row.PartialItemPaid != "4.00" || row.PartialItemStatus != "partial" {
		return fmt.Errorf("partial item paid/status = %s/%s, want 4.00/partial", row.PartialItemPaid, row.PartialItemStatus)
	}
	if row.PaidItemPaid != "20.00" || row.PaidItemStatus != "paid" {
		return fmt.Errorf("paid item paid/status = %s/%s, want 20.00/paid", row.PaidItemPaid, row.PaidItemStatus)
	}
	if row.OrderStatus != "partially_paid" {
		return fmt.Errorf("order status = %s, want partially_paid", row.OrderStatus)
	}
	if !row.QueryCodeHashExists {
		return fmt.Errorf("query code hash was not stored")
	}
	return nil
}

func summarizeFixture(ctx context.Context, pool *pgxpool.Pool, prefix string) (counts, error) {
	var result counts
	err := pool.QueryRow(ctx, `
		with test_users as (
			select id from users where cn_code like $1
		), test_projects as (
			select id from projects where code like $1
		), test_orders as (
			select id from orders where user_id in (select id from test_users)
		), test_payments as (
			select id, status from payments where user_id in (select id from test_users)
		)
		select
			(select count(*)::int from test_users),
			(select count(*)::int from test_projects),
			(select count(*)::int from products where project_id in (select id from test_projects)),
			(select count(*)::int from test_orders),
			(select count(*)::int from order_items where order_id in (select id from test_orders)),
			(select count(*)::int from test_payments),
			(select count(*)::int from payment_items where payment_id in (select id from test_payments)),
			(select count(*)::int from query_sessions where user_id in (select id from test_users)),
			(select count(*)::int from payment_items where payment_id in (select id from test_payments where status = 'approved')),
			(select count(*)::int from payment_items where payment_id in (select id from test_payments where status = 'voided'))
	`, prefix+"%").Scan(&result.Users, &result.Projects, &result.Products, &result.Orders, &result.OrderItems, &result.Payments, &result.PaymentItems, &result.QuerySessions, &result.ApprovedItems, &result.VoidedItems)
	return result, err
}

func cleanupFixture(ctx context.Context, pool *pgxpool.Pool, prefix string) (counts, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return counts{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := summarizeFixtureTx(ctx, tx, prefix)
	if err != nil {
		return counts{}, err
	}

	statements := []string{
		`delete from query_sessions where user_id in (select id from users where cn_code like $1)`,
		`delete from payment_items where payment_id in (select p.id from payments p join users u on u.id = p.user_id where u.cn_code like $1)`,
		`delete from payments where user_id in (select id from users where cn_code like $1)`,
		`delete from order_items where order_id in (select o.id from orders o join users u on u.id = o.user_id where u.cn_code like $1)`,
		`delete from orders where user_id in (select id from users where cn_code like $1)`,
		`delete from products where project_id in (select id from projects where code like $1)`,
		`delete from projects where code like $1`,
		`delete from users where cn_code like $1`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, prefix+"%"); err != nil {
			return counts{}, fmt.Errorf("cleanup fixture: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return counts{}, err
	}
	return before, nil
}

func summarizeFixtureTx(ctx context.Context, tx pgx.Tx, prefix string) (counts, error) {
	var result counts
	err := tx.QueryRow(ctx, `
		with test_users as (
			select id from users where cn_code like $1
		), test_projects as (
			select id from projects where code like $1
		), test_orders as (
			select id from orders where user_id in (select id from test_users)
		), test_payments as (
			select id, status from payments where user_id in (select id from test_users)
		)
		select
			(select count(*)::int from test_users),
			(select count(*)::int from test_projects),
			(select count(*)::int from products where project_id in (select id from test_projects)),
			(select count(*)::int from test_orders),
			(select count(*)::int from order_items where order_id in (select id from test_orders)),
			(select count(*)::int from test_payments),
			(select count(*)::int from payment_items where payment_id in (select id from test_payments)),
			(select count(*)::int from query_sessions where user_id in (select id from test_users)),
			(select count(*)::int from payment_items where payment_id in (select id from test_payments where status = 'approved')),
			(select count(*)::int from payment_items where payment_id in (select id from test_payments where status = 'voided'))
	`, prefix+"%").Scan(&result.Users, &result.Projects, &result.Products, &result.Orders, &result.OrderItems, &result.Payments, &result.PaymentItems, &result.QuerySessions, &result.ApprovedItems, &result.VoidedItems)
	return result, err
}

func centsFromAmount(value string) (int64, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[1]) != 2 {
		return 0, fmt.Errorf("amount %q must have two decimal places", value)
	}
	var whole int64
	for _, char := range parts[0] {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("invalid amount %q", value)
		}
		whole = whole*10 + int64(char-'0')
	}
	cents := int64(parts[1][0]-'0')*10 + int64(parts[1][1]-'0')
	return whole*100 + cents, nil
}

func normalize(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
