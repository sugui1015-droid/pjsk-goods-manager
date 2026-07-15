package payments

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"pjsk/backend/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
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
		PaymentMethod:  "alipay",
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
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T13:05",
		IdempotencyKey: fixture.prefix + "-multi-1",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 4}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment first: %v", err)
	}
	if _, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
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
		PaymentMethod:  "alipay",
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

func TestPostgresUnpaidPaymentItemsReturnOnlyRemainingItems(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 20)

	if _, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T13:20",
		IdempotencyKey: fixture.prefix + "-unpaid-filter",
		Items: []CreatePaymentItemRequest{
			{OrderItemID: data.ItemAID, Amount: 10},
			{OrderItemID: data.ItemBID, Amount: 5},
		},
	}, fixture.adminID); err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}

	response, err := fixture.store.GetCNUnpaidPayment(context.Background(), data.CN)
	if err != nil {
		t.Fatalf("GetCNUnpaidPayment: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("len(response.Items) = %d, want 1: %#v", len(response.Items), response.Items)
	}
	item := response.Items[0]
	if item.ID != data.ItemBID || item.OrderItemID != data.ItemBID {
		t.Fatalf("item ids = %q/%q, want %q", item.ID, item.OrderItemID, data.ItemBID)
	}
	if item.PaymentStatus != "partial" {
		t.Fatalf("payment status = %q, want partial", item.PaymentStatus)
	}
	if item.Amount != 20 || item.PaidAmount != 5 || item.RemainingAmount != 15 {
		t.Fatalf("amounts = %.2f/%.2f/%.2f, want 20/5/15", item.Amount, item.PaidAmount, item.RemainingAmount)
	}
	if response.Summary.ItemCount != 2 || response.Summary.PaidCount != 1 || response.Summary.PartialCount != 1 || response.Summary.TotalAmount != 30 || response.Summary.PaidAmount != 15 || response.Summary.RemainingAmount != 15 {
		t.Fatalf("summary = %#v, want full CN summary total 30 paid 15 remaining 15", response.Summary)
	}
}

func TestPostgresUnpaidPaymentItemsReturnUnpaidItems(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 20)

	response, err := fixture.store.GetCNUnpaidPayment(context.Background(), data.CN)
	if err != nil {
		t.Fatalf("GetCNUnpaidPayment: %v", err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("len(response.Items) = %d, want 2", len(response.Items))
	}
	for _, item := range response.Items {
		if item.PaymentStatus != "unpaid" || item.RemainingAmount <= 0 {
			t.Fatalf("item = %#v, want unpaid with remaining amount", item)
		}
	}
}

func TestPostgresPartialPaymentInstallmentsAndVoidRecovery(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 100, 0)

	first, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T14:00",
		Note:           "partial installment test",
		IdempotencyKey: fixture.prefix + "-installment-1",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 40}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment first: %v", err)
	}
	if first.Duplicate || first.PaymentID == "" {
		t.Fatalf("first payment response = %#v", first)
	}

	unpaid, err := fixture.store.GetCNUnpaidPayment(context.Background(), data.CN)
	if err != nil {
		t.Fatalf("GetCNUnpaidPayment after partial: %v", err)
	}
	if len(unpaid.Items) != 1 {
		t.Fatalf("len(unpaid.Items) = %d, want 1: %#v", len(unpaid.Items), unpaid.Items)
	}
	item := unpaid.Items[0]
	if item.ID != data.ItemAID || item.Amount != 100 || item.PaidAmount != 40 || item.RemainingAmount != 60 || item.PaymentStatus != "partial" {
		t.Fatalf("partial unpaid item = %#v, want amount 100 paid 40 remaining 60 partial", item)
	}
	fixture.assertOrderAndItems(t, data.OrderNo, "partially_paid", map[string]string{data.ItemAID: "partial"})

	second, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T14:05",
		Note:           "final installment test",
		IdempotencyKey: fixture.prefix + "-installment-2",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 60}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment second: %v", err)
	}
	if second.Duplicate || second.PaymentID == "" {
		t.Fatalf("second payment response = %#v", second)
	}

	unpaid, err = fixture.store.GetCNUnpaidPayment(context.Background(), data.CN)
	if err != nil {
		t.Fatalf("GetCNUnpaidPayment after paid: %v", err)
	}
	if len(unpaid.Items) != 0 {
		t.Fatalf("len(unpaid.Items) = %d, want 0: %#v", len(unpaid.Items), unpaid.Items)
	}
	fixture.assertPaidAmount(t, data.CN, 100)
	fixture.assertOrderAndItems(t, data.OrderNo, "paid", map[string]string{data.ItemAID: "paid"})

	if _, err := fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: second.PaymentID, Reason: "restore installment debt"}, fixture.adminID); err != nil {
		t.Fatalf("VoidPayment second: %v", err)
	}
	unpaid, err = fixture.store.GetCNUnpaidPayment(context.Background(), data.CN)
	if err != nil {
		t.Fatalf("GetCNUnpaidPayment after void: %v", err)
	}
	if len(unpaid.Items) != 1 {
		t.Fatalf("len(unpaid.Items) = %d, want 1 after void: %#v", len(unpaid.Items), unpaid.Items)
	}
	item = unpaid.Items[0]
	if item.ID != data.ItemAID || item.Amount != 100 || item.PaidAmount != 40 || item.RemainingAmount != 60 || item.PaymentStatus != "partial" {
		t.Fatalf("restored unpaid item = %#v, want amount 100 paid 40 remaining 60 partial", item)
	}
	fixture.assertPaidAmount(t, data.CN, 40)
	fixture.assertOrderAndItems(t, data.OrderNo, "partially_paid", map[string]string{data.ItemAID: "partial"})
}

func TestPostgresRejectsOverPaymentWithoutResidualRows(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 100, 0)
	if _, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T14:10",
		IdempotencyKey: fixture.prefix + "-overpay-base",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 40}},
	}, fixture.adminID); err != nil {
		t.Fatalf("CreatePayment base: %v", err)
	}
	paymentsBefore, itemsBefore := fixture.countPaymentRows(t, data.CN)

	_, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T14:11",
		IdempotencyKey: fixture.prefix + "-overpay",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 60.01}},
	}, fixture.adminID)
	if !errors.Is(err, ErrOverPayment) {
		t.Fatalf("CreatePayment overpay error = %v, want ErrOverPayment", err)
	}
	paymentsAfter, itemsAfter := fixture.countPaymentRows(t, data.CN)
	if paymentsAfter != paymentsBefore || itemsAfter != itemsBefore {
		t.Fatalf("rows after overpay = %d/%d, want %d/%d", paymentsAfter, itemsAfter, paymentsBefore, itemsBefore)
	}
	fixture.assertPaidAmount(t, data.CN, 40)
	fixture.assertOrderAndItems(t, data.OrderNo, "partially_paid", map[string]string{data.ItemAID: "partial"})
}

func TestPostgresDuplicatePaymentDoesNotDuplicateRows(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 100, 0)
	request := CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T14:15",
		IdempotencyKey: fixture.prefix + "-duplicate",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 40}},
	}
	first, err := fixture.store.CreatePayment(context.Background(), request, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment first: %v", err)
	}
	second, err := fixture.store.CreatePayment(context.Background(), request, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment duplicate: %v", err)
	}
	if !second.Duplicate || second.PaymentID != first.PaymentID {
		t.Fatalf("duplicate response = %#v, want duplicate payment %s", second, first.PaymentID)
	}
	payments, items := fixture.countPaymentRows(t, data.CN)
	if payments != 1 || items != 1 {
		t.Fatalf("payment rows = %d/%d, want 1/1", payments, items)
	}
	fixture.assertPaidAmount(t, data.CN, 40)
}

func TestPostgresPaymentDetailReturnsAuditAmountsAndMethod(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	wechatData := fixture.createPaymentCase(t, 100.01, 0)
	wechat, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             wechatData.CN,
		PaymentMethod:  " WeChat ",
		PaidAt:         "2026-07-12T15:00",
		Note:           "wechat detail audit",
		IdempotencyKey: fixture.prefix + "-wechat-detail",
		Items:          []CreatePaymentItemRequest{{OrderItemID: wechatData.ItemAID, Amount: 100.01}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment wechat: %v", err)
	}
	wechatDetail, err := fixture.store.GetPaymentDetail(context.Background(), wechat.PaymentID)
	if err != nil {
		t.Fatalf("GetPaymentDetail wechat: %v", err)
	}
	payment := wechatDetail.Payment
	if payment.PaymentMethod != "wechat" || payment.Amount != 100.01 || payment.PrincipalAmount != 100.01 || payment.FeeAmount != 0.11 || payment.PayableAmount != 100.12 || payment.TotalAmount != 100.12 {
		t.Fatalf("wechat detail payment = %#v, want method wechat principal 100.01 fee 0.11 total 100.12", payment)
	}
	if payment.CNCode != wechatData.CN || payment.Status != "approved" || payment.CreatedBy == "" || payment.Note != "wechat detail audit" || payment.PaymentItemCount != 1 {
		t.Fatalf("wechat audit fields = %#v", payment)
	}
	if len(payment.Items) != 1 || payment.Items[0].OrderItemID != wechatData.ItemAID || payment.Items[0].AppliedAmount != 100.01 {
		t.Fatalf("wechat detail items = %#v", payment.Items)
	}

	alipayFixture := newPaymentDBFixture(t)
	alipayData := alipayFixture.createPaymentCase(t, 10, 0)
	alipay, err := alipayFixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             alipayData.CN,
		PaymentMethod:  "Alipay",
		PaidAt:         "2026-07-12T15:05",
		IdempotencyKey: fixture.prefix + "-alipay-detail",
		Items:          []CreatePaymentItemRequest{{OrderItemID: alipayData.ItemAID, Amount: 10}},
	}, alipayFixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment alipay: %v", err)
	}
	alipayDetail, err := alipayFixture.store.GetPaymentDetail(context.Background(), alipay.PaymentID)
	if err != nil {
		t.Fatalf("GetPaymentDetail alipay: %v", err)
	}
	if alipayDetail.Payment.PaymentMethod != "alipay" || alipayDetail.Payment.FeeAmount != 0 || alipayDetail.Payment.TotalAmount != 10 {
		t.Fatalf("alipay detail payment = %#v, want method alipay fee 0 total 10", alipayDetail.Payment)
	}
}

func TestPostgresVoidFailureDoesNotPartiallyWrite(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 0)
	created, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-12T15:10",
		IdempotencyKey: fixture.prefix + "-void-failure",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 10}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	paymentsBefore, itemsBefore := fixture.countPaymentRows(t, data.CN)

	_, err = fixture.store.VoidPayment(context.Background(), VoidPaymentRequest{PaymentID: created.PaymentID, Reason: "force rollback"}, "not-a-uuid")
	if err == nil {
		t.Fatal("VoidPayment with invalid admin id succeeded, want error")
	}
	paymentsAfter, itemsAfter := fixture.countPaymentRows(t, data.CN)
	if paymentsAfter != paymentsBefore || itemsAfter != itemsBefore {
		t.Fatalf("rows after failed void = %d/%d, want %d/%d", paymentsAfter, itemsAfter, paymentsBefore, itemsBefore)
	}
	detail, err := fixture.store.GetPaymentDetail(context.Background(), created.PaymentID)
	if err != nil {
		t.Fatalf("GetPaymentDetail after failed void: %v", err)
	}
	if detail.Payment.Status != "approved" || detail.Payment.VoidedAt != "" || detail.Payment.VoidedBy != "" || detail.Payment.VoidReason != "" {
		t.Fatalf("payment after failed void = %#v, want approved with no void audit fields", detail.Payment)
	}
	fixture.assertPaidAmount(t, data.CN, 10)
	fixture.assertOrderAndItems(t, data.OrderNo, "paid", map[string]string{data.ItemAID: "paid"})
}

func TestPostgresVoidPaymentTwiceReturnsConflictError(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 10, 0)
	created, err := fixture.store.CreatePayment(context.Background(), CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
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
		PaymentMethod:  "alipay",
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

// newPaymentDBFixture returns a fixture on this test's own throwaway database,
// with the schema built by the real migration runner.
//
// It used to load the real backend/.env and connect to DATABASE_URL — which
// pointed at the production database — and then run applyVoidMigration: a
// verbatim copy of migrations 0010 and 0011 that dropped and recreated the
// production payments constraints and ran an unscoped
// `update payments set fee_amount = 0, ...`. Both the copied DDL and that
// helper are gone on purpose: migrations own the schema.
func newPaymentDBFixture(t *testing.T) paymentDBFixture {
	t.Helper()
	pool := testdb.New(t, "payments")
	fixture := paymentDBFixture{
		pool:   pool,
		store:  NewPostgresStore(pool),
		prefix: fmt.Sprintf("PAYMENT_VOID_TEST_%d", time.Now().UnixNano()),
	}
	fixture.adminID = fixture.createAdmin(t)
	t.Cleanup(func() { fixture.cleanup(t) })
	return fixture
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

func (f paymentDBFixture) countPaymentRows(t *testing.T, cn string) (int, int) {
	t.Helper()
	var payments int
	if err := f.pool.QueryRow(context.Background(), `
		select count(*)
		from payments p
		join users u on u.id = p.user_id
		where u.cn_code = $1
	`, cn).Scan(&payments); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	var paymentItems int
	if err := f.pool.QueryRow(context.Background(), `
		select count(*)
		from payment_items pi
		join payments p on p.id = pi.payment_id
		join users u on u.id = p.user_id
		where u.cn_code = $1
	`, cn).Scan(&paymentItems); err != nil {
		t.Fatalf("count payment items: %v", err)
	}
	return payments, paymentItems
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
