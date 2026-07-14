package querycoderecovery

import (
	"context"
	"testing"

	"pjsk/backend/internal/users"
)

func TestQueryCodeRecoveryInvalidatedByUserAndEmailChanges(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	userStore := users.NewPostgresStore(f.pool)

	disabledCN, disabledID, _ := f.addEligibleUser("DISABLED_INVALIDATION", "disabled-invalidation@example.com")
	_ = f.issueToken(disabledCN, f.ip("disabled-invalidation"))
	if _, err := userStore.SetQueryAccessStatus(ctx, disabledID, "disabled"); err != nil {
		t.Fatal(err)
	}
	f.assertCount(`select count(*)::int from query_code_recovery_sessions where user_id=$1::uuid and status='invalidated' and invalidated_at is not null`, 1, disabledID)

	replaceCN, replaceID, _ := f.addEligibleUser("REPLACE_INVALIDATION", "replace-before@example.org")
	_ = f.issueToken(replaceCN, f.ip("replace-invalidation"))
	protected, err := f.protector.Protect("replace-after@example.org")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := userStore.PutRecoveryEmail(ctx, replaceID, f.adminID, "recovery invalidation integration", protected); err != nil {
		t.Fatal(err)
	}
	f.assertCount(`select count(*)::int from query_code_recovery_sessions where user_id=$1::uuid and status='invalidated' and invalidated_at is not null`, 1, replaceID)

	unbindCN, unbindID, _ := f.addEligibleUser("UNBIND_INVALIDATION", "unbind-before@example.net")
	_ = f.issueToken(unbindCN, f.ip("unbind-invalidation"))
	if changed, err := userStore.UnbindRecoveryEmail(ctx, unbindID, f.adminID, "recovery invalidation integration"); err != nil || !changed {
		t.Fatal("recovery-email unbind failed")
	}
	f.assertCount(`select count(*)::int from query_code_recovery_sessions where user_id=$1::uuid and status='invalidated' and invalidated_at is not null`, 1, unbindID)
}
