package paymentsubmission

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"pjsk/backend/internal/paymentqr"
	"pjsk/backend/internal/payments"
	"pjsk/backend/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

type submissionFixture struct {
	pool     *pgxpool.Pool
	store    *PostgresStore
	payStore *payments.PostgresStore
	adminID  string
	prefix   string
}

type submissionCase struct {
	CN     string
	UserID string
	ItemID string
}

func newSubmissionFixture(t *testing.T) submissionFixture {
	t.Helper()
	pool := testdb.New(t, "paymentsubmission")
	payStore := payments.NewPostgresStore(pool)
	fixture := submissionFixture{
		pool:     pool,
		store:    NewPostgresStore(pool, payStore),
		payStore: payStore,
		prefix:   fmt.Sprintf("PAYSUB_TEST_%d", time.Now().UnixNano()),
	}
	fixture.adminID = fixture.createAdmin(t)
	t.Cleanup(func() { fixture.cleanup(t) })
	return fixture
}

func (f submissionFixture) createAdmin(t *testing.T) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(), `
		insert into admins (username, password_hash, status)
		values ($1, 'test-hash', 'active')
		returning id::text
	`, f.prefix+"_admin").Scan(&id); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	return id
}

// createUser builds a user with one unpaid order item of the given amount.
func (f submissionFixture) createUser(t *testing.T, suffix string, itemAmount float64) submissionCase {
	t.Helper()
	cn := f.prefix + "_CN_" + suffix
	code := f.prefix + "_P_" + suffix
	var c submissionCase
	c.CN = cn
	err := f.pool.QueryRow(context.Background(), `
		with u as (
			insert into users (cn_code, display_name, status)
			values ($1, 'proof test user', 'active')
			returning id
		), p as (
			insert into projects (code, name, status)
			values ($2, $2, 'active')
			returning id
		), prod as (
			insert into products (project_id, sku, name, category, unit_price, status, sort_order)
			select id, $2 || '_A', 'Proof Item A', 'test', $3, 'active', 1 from p
			returning id
		), o as (
			insert into orders (project_id, user_id, order_no, status, total_amount, note)
			select p.id, u.id, $2 || '_ORDER', 'submitted', $3, 'proof test'
			from p cross join u
			returning id
		), item_a as (
			insert into order_items (order_id, product_id, quantity, unit_price, amount, payment_status, source_sheet, source_row_key)
			select o.id, prod.id, 1, $3, $3, 'unpaid', 'proof_test', 'A1'
			from o cross join prod
			returning id
		)
		select (select id::text from u), (select id::text from item_a)
	`, cn, code, itemAmount).Scan(&c.UserID, &c.ItemID)
	if err != nil {
		t.Fatalf("create user case: %v", err)
	}
	return c
}

func validatedProof(t *testing.T) CreateInput {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	img.Set(1, 1, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	validated, err := paymentqr.ValidateImageWithLimit(buf.Bytes(), MaxImageBytes)
	if err != nil {
		t.Fatalf("validate image: %v", err)
	}
	return CreateInput{
		ImageData:        buf.Bytes(),
		MimeType:         validated.MimeType,
		SHA256:           validated.SHA256,
		ByteSize:         validated.ByteSize,
		OriginalFilename: "proof.png",
	}
}

func (f submissionFixture) submit(t *testing.T, c submissionCase, method string) UserSubmission {
	t.Helper()
	in := validatedProof(t)
	in.UserID = c.UserID
	in.CNCode = c.CN
	in.PaymentMethod = method
	sub, err := f.store.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return sub
}

func (f submissionFixture) countSubmissions(t *testing.T, cn string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `select count(*) from payment_submissions where cn_code = $1`, cn).Scan(&n); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	return n
}

func (f submissionFixture) countPayments(t *testing.T, cn string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `
		select count(*) from payments p join users u on u.id = p.user_id where u.cn_code = $1
	`, cn).Scan(&n); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	return n
}

func (f submissionFixture) paidAmount(t *testing.T, cn string) float64 {
	t.Helper()
	resp, err := f.payStore.GetCNPayment(context.Background(), cn)
	if err != nil {
		t.Fatalf("GetCNPayment: %v", err)
	}
	return resp.Summary.PaidAmount
}

func (f submissionFixture) submissionStatus(t *testing.T, id string) (string, string) {
	t.Helper()
	var status, linked string
	if err := f.pool.QueryRow(context.Background(), `
		select status, coalesce(linked_payment_id::text, '') from payment_submissions where id = $1::uuid
	`, id).Scan(&status, &linked); err != nil {
		t.Fatalf("read submission status: %v", err)
	}
	return status, linked
}

// --- Financial invariants -----------------------------------------------------

func TestSubmitOnlyDoesNotChangePaidAmount(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "solo", 120)

	sub := fixture.submit(t, c, "wechat")
	if sub.Status != StatusSubmitted {
		t.Fatalf("status = %q, want submitted", sub.Status)
	}
	// Principal is the outstanding the user was shown; wechat fee = ceil(12000/1000)=12c.
	if sub.PrincipalAmount != 120 || sub.FeeAmount != 0.12 || sub.PayableAmount != 120.12 {
		t.Fatalf("amounts = %.2f/%.2f/%.2f, want 120/0.12/120.12", sub.PrincipalAmount, sub.FeeAmount, sub.PayableAmount)
	}
	if got := fixture.countSubmissions(t, c.CN); got != 1 {
		t.Fatalf("submissions = %d, want 1", got)
	}
	// The financial invariant: a submission moves neither payments nor paid total.
	if got := fixture.countPayments(t, c.CN); got != 0 {
		t.Fatalf("payments = %d, want 0 (a proof must not create a payment)", got)
	}
	if got := fixture.paidAmount(t, c.CN); got != 0 {
		t.Fatalf("paid = %.2f, want 0 (a proof must not increase the paid total)", got)
	}
}

func TestApproveCreatesOnePaymentAndLinks(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "approve", 120)
	sub := fixture.submit(t, c, "wechat")

	detail, err := fixture.store.Approve(context.Background(), sub.ID, fixture.adminID, ApproveInput{
		PaidAt: "2026-07-18T12:00",
		Items:  []ApproveItem{{OrderItemID: c.ItemID, Amount: 120}},
	})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if detail.Status != StatusApproved || detail.LinkedPaymentID == "" {
		t.Fatalf("detail = %#v, want approved + linked payment", detail)
	}
	if got := fixture.countPayments(t, c.CN); got != 1 {
		t.Fatalf("payments = %d, want exactly 1", got)
	}
	if got := fixture.paidAmount(t, c.CN); got != 120 {
		t.Fatalf("paid = %.2f, want 120 after approval", got)
	}
	// payment_items must carry the exact allocation.
	var applied float64
	if err := fixture.pool.QueryRow(context.Background(), `
		select pi.applied_amount::float8
		from payment_items pi
		join payments p on p.id = pi.payment_id
		join users u on u.id = p.user_id
		where u.cn_code = $1 and pi.order_item_id = $2::uuid
	`, c.CN, c.ItemID).Scan(&applied); err != nil {
		t.Fatalf("read applied amount: %v", err)
	}
	if applied != 120 {
		t.Fatalf("applied = %.2f, want 120", applied)
	}
	// linked_payment_id points at the created payment.
	status, linked := fixture.submissionStatus(t, sub.ID)
	if status != StatusApproved || linked != detail.LinkedPaymentID {
		t.Fatalf("submission status/linked = %q/%q, want approved/%q", status, linked, detail.LinkedPaymentID)
	}
}

func TestRejectKeepsPaidAndAllowsResubmit(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "reject", 120)
	first := fixture.submit(t, c, "alipay")

	if _, err := fixture.store.Reject(context.Background(), first.ID, fixture.adminID, "图片不清晰"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got := fixture.paidAmount(t, c.CN); got != 0 {
		t.Fatalf("paid = %.2f, want 0 (reject must not touch paid)", got)
	}
	status, _ := fixture.submissionStatus(t, first.ID)
	if status != StatusRejected {
		t.Fatalf("status = %q, want rejected", status)
	}
	var reason string
	if err := fixture.pool.QueryRow(context.Background(), `select coalesce(reject_reason,'') from payment_submissions where id = $1::uuid`, first.ID).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != "图片不清晰" {
		t.Fatalf("reason = %q, want 图片不清晰", reason)
	}

	// Re-submitting creates a NEW row; the old rejected record is preserved.
	second := fixture.submit(t, c, "wechat")
	if second.ID == first.ID {
		t.Fatal("resubmission must create a new row, not overwrite the old one")
	}
	if got := fixture.countSubmissions(t, c.CN); got != 2 {
		t.Fatalf("submissions = %d, want 2 (old rejected + new submitted)", got)
	}
	oldStatus, _ := fixture.submissionStatus(t, first.ID)
	if oldStatus != StatusRejected {
		t.Fatalf("old submission status = %q, want it preserved as rejected", oldStatus)
	}
}

func TestDuplicateApproveDoesNotCreateSecondPayment(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "dupapprove", 120)
	sub := fixture.submit(t, c, "alipay")
	input := ApproveInput{Items: []ApproveItem{{OrderItemID: c.ItemID, Amount: 120}}}

	if _, err := fixture.store.Approve(context.Background(), sub.ID, fixture.adminID, input); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	// A second approval of the same (now approved) proof must be refused, and it
	// must not create a second payment.
	_, err := fixture.store.Approve(context.Background(), sub.ID, fixture.adminID, input)
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("second approve error = %v, want ErrNotPending", err)
	}
	if got := fixture.countPayments(t, c.CN); got != 1 {
		t.Fatalf("payments = %d, want 1 (no duplicate)", got)
	}
}

func TestConcurrentApproveOnlyOneSucceeds(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "concurrent", 120)
	sub := fixture.submit(t, c, "alipay")
	input := ApproveInput{Items: []ApproveItem{{OrderItemID: c.ItemID, Amount: 120}}}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fixture.store.Approve(context.Background(), sub.ID, fixture.adminID, input)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	successes, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrNotPending):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 || conflicts != workers-1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/%d", successes, conflicts, workers-1)
	}
	if got := fixture.countPayments(t, c.CN); got != 1 {
		t.Fatalf("payments = %d, want exactly 1 under concurrency", got)
	}
	if got := fixture.paidAmount(t, c.CN); got != 120 {
		t.Fatalf("paid = %.2f, want 120", got)
	}
}

func TestVoidAfterApproveRevertsPaidButKeepsProofHistory(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "void", 120)
	sub := fixture.submit(t, c, "alipay")

	detail, err := fixture.store.Approve(context.Background(), sub.ID, fixture.adminID, ApproveInput{
		Items: []ApproveItem{{OrderItemID: c.ItemID, Amount: 120}},
	})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got := fixture.paidAmount(t, c.CN); got != 120 {
		t.Fatalf("paid before void = %.2f, want 120", got)
	}

	// Voiding the real payment rolls back the paid effect...
	if _, err := fixture.payStore.VoidPayment(context.Background(), payments.VoidPaymentRequest{
		PaymentID: detail.LinkedPaymentID, Reason: "退款",
	}, fixture.adminID); err != nil {
		t.Fatalf("VoidPayment: %v", err)
	}
	if got := fixture.paidAmount(t, c.CN); got != 0 {
		t.Fatalf("paid after void = %.2f, want 0", got)
	}
	// ...but the proof stays approved and still linked: voiding a payment must not
	// revive, reopen, or rewrite the historical proof.
	status, linked := fixture.submissionStatus(t, sub.ID)
	if status != StatusApproved || linked != detail.LinkedPaymentID {
		t.Fatalf("submission = %q/%q, want it kept approved+linked (history preserved)", status, linked)
	}
}

func TestApproveRejectedSubmissionRefused(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "rejapprove", 120)
	sub := fixture.submit(t, c, "alipay")
	if _, err := fixture.store.Reject(context.Background(), sub.ID, fixture.adminID, "无效凭证"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	_, err := fixture.store.Approve(context.Background(), sub.ID, fixture.adminID, ApproveInput{
		Items: []ApproveItem{{OrderItemID: c.ItemID, Amount: 120}},
	})
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("approve of rejected proof error = %v, want ErrNotPending", err)
	}
	if got := fixture.countPayments(t, c.CN); got != 0 {
		t.Fatalf("payments = %d, want 0 (rejected proof cannot become a payment)", got)
	}
}

func TestUserCannotReadForeignImage(t *testing.T) {
	fixture := newSubmissionFixture(t)
	a := fixture.createUser(t, "usera", 120)
	b := fixture.createUser(t, "userb", 80)
	subA := fixture.submit(t, a, "alipay")
	subB := fixture.submit(t, b, "wechat")

	// The owner reads their own image fine.
	if _, err := fixture.store.UserImage(context.Background(), a.UserID, subA.ID); err != nil {
		t.Fatalf("owner UserImage: %v", err)
	}
	// User A asking for user B's submission gets ErrNotFound — no ownership leak.
	if _, err := fixture.store.UserImage(context.Background(), a.UserID, subB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user UserImage error = %v, want ErrNotFound", err)
	}
}

func TestApproveOverPaymentRejectedWithoutResidualRows(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "overpay", 120)
	sub := fixture.submit(t, c, "alipay")

	// Allocating more than the item's remaining balance must be refused and leave
	// no payment behind, and the proof must stay submitted (still reviewable).
	_, err := fixture.store.Approve(context.Background(), sub.ID, fixture.adminID, ApproveInput{
		Items: []ApproveItem{{OrderItemID: c.ItemID, Amount: 200}},
	})
	if !errors.Is(err, payments.ErrOverPayment) {
		t.Fatalf("approve over-payment error = %v, want ErrOverPayment", err)
	}
	if got := fixture.countPayments(t, c.CN); got != 0 {
		t.Fatalf("payments = %d, want 0 after a rejected over-payment", got)
	}
	status, _ := fixture.submissionStatus(t, sub.ID)
	if status != StatusSubmitted {
		t.Fatalf("submission status = %q, want it still submitted after a failed approve", status)
	}
}

func TestCreateRejectsInvalidMethodWithoutRow(t *testing.T) {
	fixture := newSubmissionFixture(t)
	c := fixture.createUser(t, "badmethod", 120)
	in := validatedProof(t)
	in.UserID = c.UserID
	in.CNCode = c.CN
	in.PaymentMethod = "bank"
	if _, err := fixture.store.Create(context.Background(), in); !errors.Is(err, ErrInvalidMethod) {
		t.Fatalf("Create with bank method error = %v, want ErrInvalidMethod", err)
	}
	if got := fixture.countSubmissions(t, c.CN); got != 0 {
		t.Fatalf("submissions = %d, want 0 (invalid method must leave no row)", got)
	}
}

func (f submissionFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	patterns := []string{
		`delete from payment_submissions where cn_code like $1`,
		`delete from payment_items where payment_id in (select p.id from payments p join users u on u.id = p.user_id where u.cn_code like $1)`,
		`delete from payments where user_id in (select id from users where cn_code like $1)`,
		`delete from order_items where order_id in (select o.id from orders o join users u on u.id = o.user_id where u.cn_code like $1)`,
		`delete from orders where user_id in (select id from users where cn_code like $1)`,
		`delete from products where project_id in (select id from projects where code like $1)`,
		`delete from projects where code like $1`,
		`delete from admins where username like $1`,
		`delete from users where cn_code like $1`,
	}
	for _, statement := range patterns {
		if _, err := f.pool.Exec(ctx, statement, f.prefix+"%"); err != nil {
			t.Errorf("cleanup %s: %v", statement, err)
		}
	}
}
