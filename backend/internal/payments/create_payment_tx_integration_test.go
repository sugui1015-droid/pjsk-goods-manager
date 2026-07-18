package payments

import (
	"context"
	"testing"
)

// TestCreatePaymentTxCommitsWhenCallerCommits proves the extracted transaction
// core behaves exactly like CreatePayment when the caller owns the transaction:
// the payment and its items land, the fee is the canonical integer-cent value,
// and the paid total updates. This is the regression that guards the refactor
// which lets the payment-proof approval reuse this same core.
func TestCreatePaymentTxCommitsWhenCallerCommits(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 100.01, 0)

	ctx := context.Background()
	tx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	resp, err := CreatePaymentTx(ctx, tx, CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "wechat",
		PaidAt:         "2026-07-18T10:00",
		IdempotencyKey: fixture.prefix + "-tx-commit",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 100.01}},
	}, fixture.adminID)
	if err != nil {
		t.Fatalf("CreatePaymentTx: %v", err)
	}
	if resp.PaymentID == "" || resp.Duplicate {
		t.Fatalf("response = %#v, want a fresh payment id", resp)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	paymentsCount, itemsCount := fixture.countPaymentRows(t, data.CN)
	if paymentsCount != 1 || itemsCount != 1 {
		t.Fatalf("rows = %d/%d, want 1/1 after commit", paymentsCount, itemsCount)
	}
	fixture.assertPaidAmount(t, data.CN, 100.01)

	// Fee is the canonical WeChat integer-cent ceiling: ceil(10001/1000) = 11c.
	detail, err := fixture.store.GetPaymentDetail(ctx, resp.PaymentID)
	if err != nil {
		t.Fatalf("GetPaymentDetail: %v", err)
	}
	if detail.Payment.FeeAmount != 0.11 || detail.Payment.PayableAmount != 100.12 {
		t.Fatalf("fee/payable = %.2f/%.2f, want 0.11/100.12", detail.Payment.FeeAmount, detail.Payment.PayableAmount)
	}
}

// TestCreatePaymentTxRollbackLeavesNoPayment proves atomicity from the caller's
// side: if the caller rolls the transaction back after CreatePaymentTx returns,
// no payment, no items and no paid amount survive. This is exactly what protects
// the proof-approval flow — if marking the submission approved fails, the whole
// payment is discarded, so a proof can never be approved without a payment nor a
// payment created without linking the proof.
func TestCreatePaymentTxRollbackLeavesNoPayment(t *testing.T) {
	fixture := newPaymentDBFixture(t)
	data := fixture.createPaymentCase(t, 50, 0)

	ctx := context.Background()
	tx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := CreatePaymentTx(ctx, tx, CreatePaymentRequest{
		CN:             data.CN,
		PaymentMethod:  "alipay",
		PaidAt:         "2026-07-18T10:05",
		IdempotencyKey: fixture.prefix + "-tx-rollback",
		Items:          []CreatePaymentItemRequest{{OrderItemID: data.ItemAID, Amount: 50}},
	}, fixture.adminID); err != nil {
		t.Fatalf("CreatePaymentTx: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	paymentsCount, itemsCount := fixture.countPaymentRows(t, data.CN)
	if paymentsCount != 0 || itemsCount != 0 {
		t.Fatalf("rows = %d/%d, want 0/0 after rollback", paymentsCount, itemsCount)
	}
	fixture.assertPaidAmount(t, data.CN, 0)
}
