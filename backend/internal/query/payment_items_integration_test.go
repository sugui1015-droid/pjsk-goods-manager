package query

import (
	"context"
	"testing"
	"time"

	"pjsk/backend/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresUserPaymentsCarryMatchingItems verifies against a real
// database that each user-visible payment carries exactly the items it was
// allocated to, with the right applied amount per goods name — including
// the case where one payment spans multiple items and where an item's own
// subtotal differs from the allocated amount (partial payment).
func TestPostgresUserPaymentsCarryMatchingItems(t *testing.T) {
	pool := newQueryTestPool(t)
	prefix := "QUERY_PAYITEMS_TEST_" + time.Now().Format("20060102150405")
	cleanupQueryFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupQueryFixture(t, pool, prefix) })

	userID := createQueryPaymentFixture(t, pool, prefix)
	store := NewPostgresStore(pool)

	records, err := store.listPaymentsForUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("listPaymentsForUser: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("payment count = %d, want 2: %#v", len(records), records)
	}

	// Ordered by paid_at desc: the voided single-item payment is newest.
	voided, approved := records[0], records[1]
	if voided.Status != "voided" || approved.Status != "approved" {
		t.Fatalf("statuses = %s/%s, want voided/approved", voided.Status, approved.Status)
	}

	if len(voided.Items) != 1 {
		t.Fatalf("voided payment items = %#v, want 1 item", voided.Items)
	}
	// Item B has no category, so its display name is the bare product name.
	if voided.Items[0].DisplayName != prefix+" Item B" || voided.Items[0].AppliedAmount != 5.00 {
		t.Fatalf("voided item = %#v, want Item B applied 5.00", voided.Items[0])
	}

	if len(approved.Items) != 2 {
		t.Fatalf("approved payment items = %#v, want 2 items", approved.Items)
	}
	byName := map[string]PaymentItem{}
	for _, item := range approved.Items {
		byName[item.DisplayName] = item
	}
	itemA := byName[prefix+" Item A-徽章"]
	if itemA.AppliedAmount != 4.00 || itemA.Amount != 10.00 || itemA.UnitPrice != 10.00 || itemA.Quantity != 1 {
		t.Fatalf("item A = %#v, want applied 4.00 of subtotal 10.00", itemA)
	}
	if itemA.PaymentStatus != "partial" {
		t.Fatalf("item A status = %q, want partial", itemA.PaymentStatus)
	}
	if itemA.CharacterName != "miku" || itemA.Category != "徽章" {
		t.Fatalf("item A business fields = %#v", itemA)
	}
	if itemA.DisplayName != prefix+" Item A-徽章" {
		t.Fatalf("item A display name = %q, want composed name-category", itemA.DisplayName)
	}

	itemB := byName[prefix+" Item B"]
	if itemB.AppliedAmount != 20.00 || itemB.Amount != 20.00 {
		t.Fatalf("item B = %#v, want applied 20.00 of subtotal 20.00", itemB)
	}
	if itemB.PaymentStatus != "paid" {
		t.Fatalf("item B status = %q, want paid", itemB.PaymentStatus)
	}
}

func createQueryPaymentFixture(t *testing.T, pool *pgxpool.Pool, prefix string) string {
	t.Helper()
	ctx := context.Background()
	var userID, itemAID, itemBID string
	err := pool.QueryRow(ctx, `
		with u as (
			insert into users (cn_code, display_name, status)
			values ($1 || 'CN', 'Query payment items test user', 'active')
			returning id
		), proj as (
			insert into projects (code, name, status)
			values ($1 || 'PROJECT', $1 || 'PROJECT', 'active')
			returning id
		), product_a as (
			insert into products (project_id, sku, name, character_name, category, unit_price, status, sort_order)
			select id, $1 || '_A', $1 || ' Item A', 'miku', '徽章', 10, 'active', 1 from proj
			returning id
		), product_b as (
			insert into products (project_id, sku, name, character_name, category, unit_price, status, sort_order)
			select id, $1 || '_B', $1 || ' Item B', '', '', 20, 'active', 2 from proj
			returning id
		), o as (
			insert into orders (project_id, user_id, order_no, status, total_amount, note)
			select proj.id, u.id, $1 || 'ORDER', 'partially_paid', 30, 'query payment items test'
			from proj cross join u
			returning id
		), item_a as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_a.id, 1, 10, 10, 'partial', 'Sheet1', 'R1' from o cross join product_a
			returning id
		), item_b as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, product_b.id, 1, 20, 20, 'paid', 'Sheet1', 'R2' from o cross join product_b
			returning id
		)
		select (select id::text from u), (select id::text from item_a), (select id::text from item_b)
	`, prefix).Scan(&userID, &itemAID, &itemBID)
	if err != nil {
		t.Fatalf("create query payment fixture: %v", err)
	}

	insertPayment := func(status string, paidAt string, key string, allocations map[string]string) {
		t.Helper()
		var paymentID string
		total := "0"
		switch len(allocations) {
		case 1:
			for _, amount := range allocations {
				total = amount
			}
		default:
			total = "24.00"
		}
		if err := pool.QueryRow(ctx, `
			insert into payments (user_id, submitted_amount, fee_amount, payable_amount, payment_method, status, submitted_at, approved_at, paid_at, idempotency_key)
			values ($1::uuid, $2::numeric(12,2), 0, $2::numeric(12,2), 'alipay', $3, now(), now(), $4::timestamptz, $5)
			returning id::text
		`, userID, total, status, paidAt, prefix+key).Scan(&paymentID); err != nil {
			t.Fatalf("insert payment %s: %v", key, err)
		}
		for itemID, amount := range allocations {
			if _, err := pool.Exec(ctx, `
				insert into payment_items (payment_id, order_item_id, applied_amount)
				values ($1::uuid, $2::uuid, $3::numeric(12,2))
			`, paymentID, itemID, amount); err != nil {
				t.Fatalf("insert payment item %s: %v", key, err)
			}
		}
	}
	insertPayment("approved", "2026-07-12T02:00:00Z", "-approved", map[string]string{itemAID: "4.00", itemBID: "20.00"})
	insertPayment("voided", "2026-07-13T02:00:00Z", "-voided", map[string]string{itemBID: "5.00"})
	return userID
}

// newQueryTestPool returns a pool for this test's own throwaway database.
// It no longer loads backend/.env or reads DATABASE_URL, which pointed at the
// production database.
func newQueryTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.New(t, "query")
}

func cleanupQueryFixture(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`delete from payment_items where payment_id in (select p.id from payments p join users u on u.id = p.user_id where u.cn_code like $1)`,
		`delete from payments where user_id in (select id from users where cn_code like $1)`,
		`delete from order_items where order_id in (select o.id from orders o join users u on u.id = o.user_id where u.cn_code like $1)`,
		`delete from orders where user_id in (select id from users where cn_code like $1)`,
		`delete from products where project_id in (select id from projects where code like $1)`,
		`delete from projects where code like $1`,
		`delete from users where cn_code like $1`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, prefix+"%"); err != nil {
			t.Fatalf("cleanup query fixture: %v", err)
		}
	}
}
