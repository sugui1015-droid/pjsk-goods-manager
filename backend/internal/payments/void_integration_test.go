package payments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type paymentDBFixture struct {
	pool    *pgxpool.Pool
	store   *PostgresStore
	adminID string
	prefix  string
}

type paymentCaseData struct {
	CN          string
	UserID      string
	ProjectID   string
	OrderID     string
	OrderNo     string
	ItemAID     string
	ItemBID     string
	ItemAAmount float64
	ItemBAmount float64
}

func TestPostgresVoidPartialPaymentRollsBackToSubmitted(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 0)

	created, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "test",
		PaidAt:         "2026-07-12T13:00",
		Note:           "partial void test",
		IdempotencyKey: fixture.prefix + "-partial",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 4}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if created.Summary.PartialCount != 1 {
		t.Fatalf("partial count = %d, want 1", created.Summary.PartialCount)
	}

	voided, err := fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: created.PaymentID, Reason: "rollback partial"}, fixture.adminID)
	if err != nil {
		t.Fatalf("VoidPayment: %v", err)
	}
	if voided.Payment.Status != "voided" || voided.Payment.VoidReason != "rollback partial" {
		t.Fatalf("voided payment = %#v", voided.Payment)
	}
	fixture.assertOrderAndItems(t, data.OrderNo, "submitted", map[string]string{data.ItemAID: "unpaid"})
}

func TestPostgresVoidOneOfMultiplePaymentsRecalculatesRemainingPayment(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 0)

	first, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "test",
		PaidAt:         "2026-07-12T13:05",
		IdempotencyKey: fixture.prefix + "-multi-1",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 4}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment first: %v", err)
	}
	if _, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "test",
		PaidAt:         "2026-07-12T13:06",
		IdempotencyKey: fixture.prefix + "-multi-2",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 3}},
	}, fixture.adminID); err != nil {
		t.Fatalf("CreatePayment second: %v", err)
	}

	if _, err := fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: first.PaymentID, Reason: "void first only"}, fixture.adminID); err != nil {
		t.Fatalf("VoidPayment: %v", err)
	}
	fixture.assertOrderAndItems(t, data.OrderNo, "partially_paid", map[string]string{data.ItemAID: "partial"})
	fixture.assertPaidAmount(t, data.CN, 3)
}

func TestPostgresVoidFullPaymentRollsBackPaidStatus(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 0)

	created, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "test",
		PaidAt:         "2026-07-12T13:10",
		IdempotencyKey: fixture.prefix + "-full",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 10}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	fixture.assertOrderAndItems(t, data.OrderNo, "paid", map[string]string{data.ItemAID: "paid"})

	if _, err := fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: created.PaymentID, Reason: "void full"}, fixture.adminID); err != nil {
		t.Fatalf("VoidPayment: %v", err)
	}
	fixture.assertOrderAndItems(t, data.OrderNo, "submitted", map[string]string{data.ItemAID: "unpaid"})
}

func TestPostgresVoidPaymentTwiceReturnsConflictError(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 0)
	created, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		IdempotencyKey: fixture.prefix + "-twice",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 5}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if _, err := fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: created.PaymentID, Reason: "first"}, fixture.adminID); err != nil {
		t.Fatalf("first VoidPayment: %v", err)
	}
	_, err = fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: created.PaymentID, Reason: "second"}, fixture.adminID)
	if !errors.Is(err, ErrPaymentAlreadyVoid) {
		t.Fatalf("second VoidPayment error = %v, want ErrPaymentAlreadyVoid", err)
	}
}

func TestPostgresVoidPaymentConcurrentOnlyOneSucceeds(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 0)
	created, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		IdempotencyKey: fixture.prefix + "-concurrent",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 5}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: created.PaymentID, Reason: fmt.Sprintf("concurrent %d", index)}, fixture.adminID)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		if errors.Is(err, ErrPaymentAlreadyVoid) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent error: %v", err)
	}
	if successes != 1 || conflicts != workers-1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/%d", successes, conflicts, workers-1)
	}
	fixture.assertOrderAndItems(t, data.OrderNo, "submitted", map[string]string{data.ItemAID: "unpaid"})
}

func newPaymentDBFixture(t *testing.T) paymentDBFixture {
	t.Helper()
	_ = godotenv.Load("../../.env", "../.env", ".env")
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database is not available: %v", err)
	}
	fixture := paymentDBFixture{
		pool:   pool,
		store:  NewPostgresStore(pool),
		prefix: fmt.Sprintf("PAYMENT_VOID_TEST_%d", time.Now().UnixNano()),
	}
	fixture.applyVoidMigration(t)
	fixture.adminID = fixture.createAdmin(t)
	t.Cleanup(func() {
		fixture.cleanup(t)
		pool.Close()
	})
	return fixture
}

func (f paymentDBFixture) applyVoidMigration(t *testing.T) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		alter table payments
			add column if not exists voided_at timestamptz,
			add column if not exists voided_by_admin_id uuid references admins(id) on delete set null,
			add column if not exists void_reason text;
		alter table payments drop constraint if exists payments_status_check;
		alter table payments add constraint payments_status_check
			check (status in ('submitted', 'approved', 'rejected', 'cancelled', 'voided'));
	`)
	if err != nil {
		t.Fatalf("apply void migration: %v", err)
	}
}

func (f paymentDBFixture) createAdmin(t *testing.T) string {
	t.Helper()
	username := f.prefix + "_admin"
	var id string
	if err := f.pool.QueryRow(context.Background(), `
		insert into admins (username, password_hash, status)
		values ($1, 'test-hash', 'active')
		returning id::text
	`, username).Scan(&id); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	return id
}

func (f paymentDBFixture) createPaymentCase(t *testing.T, itemAAmount float64, itemBAmount float64) paymentCaseData {
	t.Helper()
	ctx := context.Background()
	cn := f.prefix + "_CN"
	projectCode := f.prefix + "_PROJECT"
	orderNo := f.prefix + "_ORDER"
	var data paymentCaseData
	data.CN = cn
	data.OrderNo = orderNo
	data.ItemAAmount = itemAAmount
	data.ItemBAmount = itemBAmount
	err := f.pool.QueryRow(ctx, `
		with u as (
			insert into users (cn_code, display_name, status)
			values ($1, 'Payment void test user', 'active')
			returning id
		), p as (
			insert into projects (code, name, status)
			values ($2, $2, 'active')
			returning id
		), product_a as (
			insert into products (project_id, sku, name, category, unit_price, status, sort_order)
			select id, $2 || '_A', 'Void Test Item A', 'test', $4, 'active', 1 from p
			returning id
		), product_b as (
			insert into products (project_id, sku, name, category, unit_price, status, sort_order)
			select id, $2 || '_B', 'Void Test Item B', 'test', greatest($5, 0), 'active', 2 from p
			where $5 > 0
			returning id
		), o as (
			insert into orders (project_id, user_id, order_no, status, total_amount, note)
			select p.id, u.id, $3, 'submitted', $4 + $5, 'payment void test'
			from p cross join u
			returning id
		), item_a as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_a.id, 1, $4, $4, 'unpaid', 'payment_void_test', 'A1'
			from o cross join product_a
			returning id
		), item_b as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_b.id, 1, $5, $5, 'unpaid', 'payment_void_test', 'B1'
			from o cross join product_b
			where $5 > 0
			returning id
		)
		select (select id::text from u), (select id::text from p), (select id::text from o), (select id::text from item_a), coalesce((select id::text from item_b), '')
	`, cn, projectCode, orderNo, itemAAmount, itemBAmount).Scan(&data.UserID, &data.ProjectID, &data.OrderID, &data.ItemAID, &data.ItemBID)
	if err != nil {
		t.Fatalf("create payment case: %v", err)
	}
	return data
}

func (f paymentDBFixture) assertOrderAndItems(t *testing.T, orderNo string, wantOrderStatus string, wantItems map[string]string) {
	t.Helper()
	var orderStatus string
	if err := f.pool.QueryRow(context.Background(), `select status from orders where order_no = $1`, orderNo).Scan(&orderStatus); err != nil {
		t.Fatalf("read order status: %v", err)
	}
	if orderStatus != wantOrderStatus {
		t.Fatalf("order status = %q, want %q", orderStatus, wantOrderStatus)
	}
	for itemID, wantStatus := range wantItems {
		var status string
		if err := f.pool.QueryRow(context.Background(), `select payment_status from order_items where id = $1::uuid`, itemID).Scan(&status); err != nil {
			t.Fatalf("read item status %s: %v", itemID, err)
		}
		if status != wantStatus {
			t.Fatalf("item %s status = %q, want %q", itemID, status, wantStatus)
		}
	}
}

func (f paymentDBFixture) assertPaidAmount(t *testing.T, cn string, want float64) {
	t.Helper()
	response, err := f.store.GetCNPayment(context.Background(), cn)
	if err != nil {
		t.Fatalf("GetCNPayment: %v", err)
	}
	if response.Summary.PaidAmount != want {
		t.Fatalf("paid amount = %.2f, want %.2f", response.Summary.PaidAmount, want)
	}
}

func (f paymentDBFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	patterns := []string{
		`delete from payment_items where payment_id in (select p.id from payments p join users u on u.id = p.user_id where u.cn_code like $1)`,
		`delete from payments where user_id in (select id from users where cn_code like $1)`,
		`delete from order_items where order_id in (select o.id from orders o join users u on u.id = o.user_id where u.cn_code like $1)`,
		`delete from orders where user_id in (select id from users where cn_code like $1)`,
		`delete from products where project_id in (select id from projects where code like $1)`,
		`delete from projects where code like $1`,
		`delete from admin_sessions where admin_id in (select id from admins where username like $1)`,
		`delete from admins where username like $1`,
		`delete from users where cn_code like $1`,
	}
	for _, statement := range patterns {
		if _, err := f.pool.Exec(ctx, statement, f.prefix+"%"); err != nil {
			t.Errorf("cleanup %s: %v", statement, err)
		}
	}
}
