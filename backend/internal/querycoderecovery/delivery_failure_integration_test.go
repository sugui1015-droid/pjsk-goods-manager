package querycoderecovery

import (
	"context"
	"errors"
	"testing"

	"pjsk/backend/internal/recoveryemailverification"
)

func TestQueryCodeRecoveryDeliveryFailureIsInvalidatedAndAudited(t *testing.T) {
	f := newRecoveryFixture(t)
	cn, userID, _ := f.addEligibleUser("DELIVERY_FAILURE", "delivery-failure@example.com")
	sender := recoveryemailverification.NewFakeSender()
	sender.SetError(errors.New("delivery unavailable"))
	service := NewService(f.store, f.protector, sender, f.manager)
	if err := service.Request(context.Background(), cn, f.ip("delivery-failure")); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatal("delivery failure was not safely collapsed")
	}
	f.assertCount(`select count(*)::int from query_code_recovery_codes where user_id=$1::uuid and status='delivery_failed' and invalidated_at is not null`, 1, userID)
	f.assertCount(`select count(*)::int from account_security_audit_logs where target_user_id=$1::uuid and action='query_code_recovery_failed' and result='failure'`, 1, userID)
	f.assertSafeStorage(userID)
}
