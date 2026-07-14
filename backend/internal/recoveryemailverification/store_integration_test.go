package recoveryemailverification

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"pjsk/backend/internal/recoveryemail"
	"pjsk/backend/internal/users"
)

type verificationFixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	prefix    string
	adminID   string
	protector *recoveryemail.Protector
	manager   *Manager
}

func TestPostgresRecoveryEmailVerificationLifecycle(t *testing.T) {
	fixture := newVerificationFixture(t)
	userID := fixture.addUser("LIFECYCLE")
	fixture.addEmail(userID, "lifecycle@example.com")
	sender := NewFakeSender()
	service := fixture.service(sender)

	sent, err := service.Send(context.Background(), userID)
	if err != nil || sent.MaskedEmail == "" || sent.ExpiresAt.IsZero() || len(sender.Deliveries()) != 1 {
		t.Fatalf("send result = %+v, deliveries=%d, error=%v", sent, len(sender.Deliveries()), err)
	}
	if _, err := service.Send(context.Background(), userID); RetryAfterSeconds(err) < 1 {
		t.Fatalf("immediate resend error = %v", err)
	}
	code := sender.Deliveries()[0].Code
	wrong := differentCode(code)
	if _, err := service.Verify(context.Background(), userID, wrong); !errors.Is(err, ErrCodeMismatch) {
		t.Fatalf("wrong code error = %v", err)
	}
	verified, err := service.Verify(context.Background(), userID, code)
	if err != nil || verified.MaskedEmail == "" || verified.VerifiedAt.IsZero() {
		t.Fatalf("verify result = %+v, error=%v", verified, err)
	}
	if _, err := service.Verify(context.Background(), userID, code); !errors.Is(err, ErrAlreadyVerified) {
		t.Fatalf("repeat verify error = %v", err)
	}
	fixture.assertCount(`select count(*)::int from user_recovery_emails where user_id=$1::uuid and status='verified' and verified_at is not null`, 1, userID)
	fixture.assertCount(`select count(*)::int from recovery_email_verification_codes where user_id=$1::uuid and status='used' and used_at is not null`, 1, userID)
	fixture.assertCount(`select count(*)::int from account_security_audit_logs where target_user_id=$1::uuid and action in ('recovery_email_verification_sent','recovery_email_verified')`, 2, userID)
	fixture.assertSafeStorage(userID)
}

func TestPostgresRecoveryEmailVerificationLimitsAndInvalidation(t *testing.T) {
	fixture := newVerificationFixture(t)
	ctx := context.Background()

	resendUser := fixture.addUser("RESEND")
	fixture.addEmail(resendUser, "resend@example.org")
	resendSender := NewFakeSender()
	resendService := fixture.service(resendSender)
	if _, err := resendService.Send(ctx, resendUser); err != nil {
		t.Fatal(err)
	}
	firstCode := resendSender.Deliveries()[0].Code
	fixture.ageLatest(resendUser)
	if _, err := resendService.Send(ctx, resendUser); err != nil {
		t.Fatal(err)
	}
	secondCode := resendSender.Deliveries()[1].Code
	if firstCode == secondCode {
		t.Fatal("resend unexpectedly produced the same test code")
	}
	fixture.assertCount(`select count(*)::int from recovery_email_verification_codes where user_id=$1::uuid and status='invalidated'`, 1, resendUser)
	if _, err := resendService.Verify(ctx, resendUser, firstCode); !errors.Is(err, ErrCodeMismatch) {
		t.Fatalf("old code error = %v", err)
	}
	for attempt := 2; attempt <= DefaultMaxAttempts; attempt++ {
		_, err := resendService.Verify(ctx, resendUser, differentCode(secondCode))
		if attempt < DefaultMaxAttempts && !errors.Is(err, ErrCodeMismatch) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
		if attempt == DefaultMaxAttempts && !errors.Is(err, ErrAttemptsExhausted) {
			t.Fatalf("lock error = %v", err)
		}
	}

	expiredUser := fixture.addUser("EXPIRED")
	fixture.addEmail(expiredUser, "expired@example.net")
	expiredSender := NewFakeSender()
	expiredService := fixture.service(expiredSender)
	if _, err := expiredService.Send(ctx, expiredUser); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `update recovery_email_verification_codes set expires_at=now()-interval '1 second' where user_id=$1::uuid`, expiredUser); err != nil {
		t.Fatal(err)
	}
	if _, err := expiredService.Verify(ctx, expiredUser, expiredSender.Deliveries()[0].Code); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("expired error = %v", err)
	}

	replacedUser := fixture.addUser("REPLACED")
	fixture.addEmail(replacedUser, "before-replace@example.com")
	replacedSender := NewFakeSender()
	replacedService := fixture.service(replacedSender)
	if _, err := replacedService.Send(ctx, replacedUser); err != nil {
		t.Fatal(err)
	}
	oldCode := replacedSender.Deliveries()[0].Code
	fixture.addEmail(replacedUser, "after-replace@example.org")
	if _, err := replacedService.Verify(ctx, replacedUser, oldCode); !errors.Is(err, ErrNoActiveCode) {
		t.Fatalf("replaced email old code error = %v", err)
	}

	unboundUser := fixture.addUser("UNBOUND")
	fixture.addEmail(unboundUser, "unbind@example.net")
	unboundSender := NewFakeSender()
	unboundService := fixture.service(unboundSender)
	if _, err := unboundService.Send(ctx, unboundUser); err != nil {
		t.Fatal(err)
	}
	oldCode = unboundSender.Deliveries()[0].Code
	if changed, err := users.NewPostgresStore(fixture.pool).UnbindRecoveryEmail(ctx, unboundUser, fixture.adminID, "verification test unbind"); err != nil || !changed {
		t.Fatalf("unbind changed=%t error=%v", changed, err)
	}
	if _, err := unboundService.Verify(ctx, unboundUser, oldCode); !errors.Is(err, ErrNoRecoveryEmail) {
		t.Fatalf("unbound email old code error = %v", err)
	}

	failureUser := fixture.addUser("DELIVERY_FAILURE")
	fixture.addEmail(failureUser, "delivery-failure@example.com")
	failureSender := NewFakeSender()
	failureSender.SetError(errors.New("test delivery failure"))
	if _, err := fixture.service(failureSender).Send(ctx, failureUser); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("delivery failure error = %v", err)
	}
	fixture.assertCount(`select count(*)::int from recovery_email_verification_codes where user_id=$1::uuid and status='delivery_failed'`, 1, failureUser)

	windowUser := fixture.addUser("WINDOW")
	fixture.addEmail(windowUser, "window@example.org")
	windowService := fixture.service(NewFakeSender())
	for count := 0; count < DefaultWindowLimit; count++ {
		if _, err := windowService.Send(ctx, windowUser); err != nil {
			t.Fatalf("window send %d: %v", count+1, err)
		}
		fixture.ageLatest(windowUser)
	}
	if _, err := windowService.Send(ctx, windowUser); RetryAfterSeconds(err) < 1 {
		t.Fatalf("window limit error = %v", err)
	}
	fixture.assertSafeStorage(resendUser)
}

func TestPostgresRecoveryEmailVerificationConcurrencyAndRollback(t *testing.T) {
	fixture := newVerificationFixture(t)
	ctx := context.Background()

	rollbackUser := fixture.addUser("ROLLBACK")
	fixture.addEmail(rollbackUser, "rollback@example.com")
	rollbackSender := NewFakeSender()
	rollbackService := fixture.service(rollbackSender)
	if _, err := rollbackService.Send(ctx, rollbackUser); err != nil {
		t.Fatal(err)
	}
	code := rollbackSender.Deliveries()[0].Code
	functionName := "test_recovery_verify_audit_failure"
	triggerName := "test_recovery_verify_audit_failure_trigger"
	fixture.dropAuditFailureTrigger(functionName, triggerName)
	t.Cleanup(func() { fixture.dropAuditFailureTrigger(functionName, triggerName) })
	statement := fmt.Sprintf(`
		create function %s() returns trigger language plpgsql as $$
		begin
			if new.action='recovery_email_verified' and new.target_user_id='%s'::uuid then
				raise exception 'forced verification audit failure';
			end if;
			return new;
		end $$;
		create trigger %s before insert on account_security_audit_logs
		for each row execute function %s()
	`, functionName, rollbackUser, triggerName, functionName)
	if _, err := fixture.pool.Exec(ctx, statement); err != nil {
		t.Fatal(err)
	}
	if _, err := rollbackService.Verify(ctx, rollbackUser, code); err == nil {
		t.Fatal("forced audit failure unexpectedly verified")
	}
	fixture.assertCount(`select count(*)::int from user_recovery_emails where user_id=$1::uuid and status='pending'`, 1, rollbackUser)
	fixture.assertCount(`select count(*)::int from recovery_email_verification_codes where user_id=$1::uuid and status='active' and used_at is null`, 1, rollbackUser)
	fixture.dropAuditFailureTrigger(functionName, triggerName)
	if _, err := rollbackService.Verify(ctx, rollbackUser, code); err != nil {
		t.Fatalf("verify after rollback = %v", err)
	}

	concurrentUser := fixture.addUser("CONCURRENT")
	fixture.addEmail(concurrentUser, "concurrent@example.net")
	concurrentSender := NewFakeSender()
	concurrentService := fixture.service(concurrentSender)
	if _, err := concurrentService.Send(ctx, concurrentUser); err != nil {
		t.Fatal(err)
	}
	code = concurrentSender.Deliveries()[0].Code
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for attempt := 0; attempt < 2; attempt++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := concurrentService.Verify(ctx, concurrentUser, code)
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrAlreadyVerified) {
			t.Fatalf("concurrent verify error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent successes = %d, want 1", successes)
	}
	fixture.assertCount(`select count(*)::int from account_security_audit_logs where target_user_id=$1::uuid and action='recovery_email_verified'`, 1, concurrentUser)
}

func newVerificationFixture(t *testing.T) *verificationFixture {
	t.Helper()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	prefix := "TEST_RECOVERY_VERIFY_" + time.Now().Format("20060102150405.000000000")
	fixture := &verificationFixture{t: t, pool: pool, prefix: prefix}
	fixture.cleanup()
	t.Cleanup(fixture.cleanup)
	fixture.adminID = fixture.addAdmin()
	encryptionKey := make([]byte, 32)
	lookupKey := make([]byte, 32)
	verificationKey := make([]byte, 32)
	for _, key := range [][]byte{encryptionKey, lookupKey, verificationKey} {
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
	}
	fixture.protector, err = recoveryemail.NewProtector(encryptionKey, lookupKey)
	if err != nil {
		t.Fatal(err)
	}
	fixture.manager, err = NewManager(verificationKey)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *verificationFixture) addAdmin() string {
	f.t.Helper()
	var id string
	err := f.pool.QueryRow(context.Background(), `insert into admins (username,password_hash,display_name,role,status) values ($1,$2,'Verification test admin','admin','active') returning id::text`, f.prefix+"_ADMIN", "TEST_RECOVERY_VERIFY_HASH").Scan(&id)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *verificationFixture) addUser(suffix string) string {
	f.t.Helper()
	var id string
	err := f.pool.QueryRow(context.Background(), `insert into users (cn_code,display_name,query_code_hash,status) values ($1,'Verification test user',$2,'active') returning id::text`, f.prefix+"_"+suffix, "TEST_RECOVERY_VERIFY_HASH").Scan(&id)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *verificationFixture) addEmail(userID string, address string) {
	f.t.Helper()
	protected, err := f.protector.Protect(address)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := users.NewPostgresStore(f.pool).PutRecoveryEmail(context.Background(), userID, f.adminID, "verification integration test", protected); err != nil {
		f.t.Fatal(err)
	}
}

func (f *verificationFixture) service(sender Sender) *Service {
	store := NewPostgresStore(f.pool, f.manager)
	return NewService(store, f.protector, sender, f.manager.Policy())
}

func (f *verificationFixture) ageLatest(userID string) {
	f.t.Helper()
	_, err := f.pool.Exec(context.Background(), `update recovery_email_verification_codes set created_at=created_at-interval '61 seconds' where id=(select id from recovery_email_verification_codes where user_id=$1::uuid order by created_at desc limit 1)`, userID)
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *verificationFixture) assertCount(query string, want int, args ...any) {
	f.t.Helper()
	var got int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&got); err != nil {
		f.t.Fatal(err)
	}
	if got != want {
		f.t.Fatalf("count = %d, want %d", got, want)
	}
}

func (f *verificationFixture) assertSafeStorage(userID string) {
	f.t.Helper()
	f.assertCount(`select count(*)::int from recovery_email_verification_codes where user_id=$1::uuid and (length(code_hash)<>64 or code_hash !~ '^[0-9a-f]{64}$')`, 0, userID)
	f.assertCount(`select count(*)::int from account_security_audit_logs where target_user_id=$1::uuid and metadata ?| array['code','code_hash','encrypted_email','email_lookup_hash','nonce','smtp_password','cookie','session_token']`, 0, userID)
}

func (f *verificationFixture) dropAuditFailureTrigger(functionName string, triggerName string) {
	f.t.Helper()
	_, err := f.pool.Exec(context.Background(), fmt.Sprintf(`drop trigger if exists %s on account_security_audit_logs; drop function if exists %s()`, triggerName, functionName))
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *verificationFixture) cleanup() {
	f.t.Helper()
	ctx := context.Background()
	pattern := f.prefix + "%"
	var orders, payments, merges int
	if err := f.pool.QueryRow(ctx, `select count(*)::int from orders where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&orders); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `select count(*)::int from payments where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&payments); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `select count(*)::int from cn_merge_logs where source_user_id in (select id from users where cn_code like $1) or target_user_id in (select id from users where cn_code like $1)`, pattern).Scan(&merges); err != nil {
		f.t.Fatal(err)
	}
	if orders != 0 || payments != 0 || merges != 0 {
		f.t.Fatalf("refusing verification fixture cleanup: orders=%d payments=%d merges=%d", orders, payments, merges)
	}
	statements := []string{
		`delete from recovery_email_verification_codes where user_id in (select id from users where cn_code like $1)`,
		`delete from account_security_audit_logs where target_user_id in (select id from users where cn_code like $1)`,
		`delete from query_sessions where user_id in (select id from users where cn_code like $1)`,
		`delete from user_recovery_emails where user_id in (select id from users where cn_code like $1)`,
		`delete from users where cn_code like $1`,
		`delete from admin_sessions where admin_id in (select id from admins where username like $1)`,
		`delete from admins where username like $1`,
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement, pattern); err != nil {
			f.t.Fatalf("cleanup verification fixture: %v", err)
		}
	}
	for label, query := range map[string]string{
		"users":  `select count(*)::int from users where cn_code like $1`,
		"admins": `select count(*)::int from admins where username like $1`,
	} {
		var count int
		if err := f.pool.QueryRow(ctx, query, pattern).Scan(&count); err != nil {
			f.t.Fatal(err)
		}
		if count != 0 {
			f.t.Fatalf("%s verification fixtures remain", label)
		}
	}
}

func differentCode(code string) string {
	if strings.HasPrefix(code, "0") {
		return "1" + code[1:]
	}
	return "0" + code[1:]
}
