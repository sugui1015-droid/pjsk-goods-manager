package querycoderecovery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/recoveryemail"
	"pjsk/backend/internal/recoveryemailverification"
	"pjsk/backend/internal/testdb"
	"pjsk/backend/internal/users"
)

type recoveryFixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	prefix    string
	adminID   string
	protector *recoveryemail.Protector
	manager   *Manager
	store     *PostgresStore
	startedAt time.Time
	ips       []string
}

func TestPostgresQueryCodeRecoveryLifecycle(t *testing.T) {
	f := newRecoveryFixture(t)
	cn, userID, oldCode := f.addEligibleUser("LIFECYCLE", "lifecycle@example.com")
	f.addQuerySessions(userID, 2)
	sender := recoveryemailverification.NewFakeSender()
	service := NewService(f.store, f.protector, sender, f.manager)
	ip := f.ip("lifecycle")
	if err := service.Request(context.Background(), cn, ip); err != nil || len(sender.Deliveries()) != 1 {
		t.Fatal("eligible recovery request did not produce one fake delivery")
	}
	delivery := sender.Deliveries()[0]
	if _, err := service.Verify(context.Background(), cn, differentRecoveryCode(delivery.Code)); !errors.Is(err, ErrCodeMismatch) {
		t.Fatal("wrong recovery code was not rejected")
	}
	verified, err := service.Verify(context.Background(), cn, delivery.Code)
	if err != nil || !ValidToken(verified.ResetToken) || verified.ExpiresAt.IsZero() {
		t.Fatal("valid recovery code did not issue a short-lived reset capability")
	}
	newCode := "RecoveredCode-456"
	if err := service.Reset(context.Background(), verified.ResetToken, newCode, newCode); err != nil {
		t.Fatal("valid reset capability did not reset the query code")
	}
	if err := service.Reset(context.Background(), verified.ResetToken, "AnotherCode-789", "AnotherCode-789"); !errors.Is(err, ErrRejected) {
		t.Fatal("used reset capability was replayable")
	}
	var storedHash string
	if err := f.pool.QueryRow(context.Background(), `select query_code_hash from users where id=$1::uuid`, userID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(newCode)) != nil || bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(oldCode)) == nil {
		t.Fatal("query-code hash did not move exclusively to the new value")
	}
	f.assertCount(`select count(*)::int from query_sessions where user_id=$1::uuid`, 0, userID)
	f.assertCount(`select count(*)::int from query_code_recovery_codes where user_id=$1::uuid and status='used' and used_at is not null`, 1, userID)
	f.assertCount(`select count(*)::int from query_code_recovery_sessions where user_id=$1::uuid and status='used' and used_at is not null`, 1, userID)
	f.assertCount(`select count(*)::int from account_security_audit_logs where target_user_id=$1::uuid and action in ('query_code_recovery_requested','query_code_recovery_code_verified','query_code_recovery_completed')`, 3, userID)
	f.assertSafeStorage(userID)
}

func TestPostgresQueryCodeRecoveryPurposeStateAndRateLimits(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	cn, userID, _ := f.addEligibleUser("BOUNDARIES", "boundaries@example.org")
	sender := recoveryemailverification.NewFakeSender()
	service := NewService(f.store, f.protector, sender, f.manager)
	ip := f.ip("boundaries")

	if err := service.Request(ctx, cn, ip); err != nil {
		t.Fatal(err)
	}
	first := sender.Deliveries()[0]
	if err := service.Request(ctx, cn, ip); !errors.Is(err, ErrRateLimited) {
		t.Fatal("immediate resend was not persistently rate limited")
	}
	f.ageLatestCode(userID)
	if err := service.Request(ctx, cn, ip); err != nil {
		t.Fatal(err)
	}
	second := sender.Deliveries()[1]
	if _, err := service.Verify(ctx, cn, first.Code); !errors.Is(err, ErrCodeMismatch) && !errors.Is(err, ErrRejected) {
		t.Fatal("resend did not invalidate the previous recovery code")
	}

	var emailID string
	if err := f.pool.QueryRow(ctx, `select id::text from user_recovery_emails where user_id=$1::uuid and invalidated_at is null`, userID).Scan(&emailID); err != nil {
		t.Fatal(err)
	}
	verificationManager, err := recoveryemailverification.NewManager(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	verificationHash, err := verificationManager.Hash(emailID, second.Code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		insert into recovery_email_verification_codes (user_id,recovery_email_id,code_hash,status,expires_at,sent_at)
		values ($1::uuid,$2::uuid,$3,'active',now()+interval '10 minutes',now())
	`, userID, emailID, verificationHash); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(ctx, cn, second.Code); err != nil {
		t.Fatal("purpose-specific recovery code was affected by the email-verification table")
	}

	otherCN, _, _ := f.addEligibleUser("OTHER", "other@example.net")
	otherSender := recoveryemailverification.NewFakeSender()
	otherService := NewService(f.store, f.protector, otherSender, f.manager)
	if err := otherService.Request(ctx, otherCN, f.ip("other")); err != nil {
		t.Fatal(err)
	}
	if _, err := otherService.Verify(ctx, otherCN, second.Code); !errors.Is(err, ErrCodeMismatch) {
		t.Fatal("recovery code crossed a CN boundary")
	}

	limitedStore := NewPostgresStore(f.pool, f.manager)
	limitedStore.policy.IPWindowLimit = 1
	missingCN := f.prefix + "_MISSING"
	missingIP := f.ip("missing")
	if _, err := limitedStore.PrepareRequest(ctx, missingCN, missingIP); !errors.Is(err, ErrNotEligible) {
		t.Fatal("missing account did not use the internal not-eligible path")
	}
	if _, err := limitedStore.PrepareRequest(ctx, missingCN, missingIP); !errors.Is(err, ErrRateLimited) {
		t.Fatal("anonymous IP limit was not persisted for missing accounts")
	}
}

func TestPostgresQueryCodeRecoveryConcurrentResetAndStateChanges(t *testing.T) {
	f := newRecoveryFixture(t)
	cn, userID, _ := f.addEligibleUser("CONCURRENT", "concurrent@example.com")
	token := f.issueToken(cn, f.ip("concurrent"))
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, newCode := range []string{"ConcurrentCode-111", "ConcurrentCode-222"} {
		wait.Add(1)
		go func(code string) {
			defer wait.Done()
			results <- NewService(f.store, f.protector, recoveryemailverification.NewFakeSender(), f.manager).Reset(context.Background(), token, code, code)
		}(newCode)
	}
	wait.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRejected) {
			rejected++
		} else {
			t.Fatalf("unexpected concurrent reset error: %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("concurrent reset results success=%d rejected=%d", success, rejected)
	}
	f.assertCount(`select count(*)::int from account_security_audit_logs where target_user_id=$1::uuid and action='query_code_recovery_completed'`, 1, userID)

	stateCN, stateUserID, _ := f.addEligibleUser("STATE", "state@example.org")
	stateToken := f.issueToken(stateCN, f.ip("state"))
	if _, err := f.pool.Exec(context.Background(), `update users set status='disabled' where id=$1::uuid`, stateUserID); err != nil {
		t.Fatal(err)
	}
	if err := NewService(f.store, f.protector, recoveryemailverification.NewFakeSender(), f.manager).Reset(context.Background(), stateToken, "StateCode-333", "StateCode-333"); !errors.Is(err, ErrRejected) {
		t.Fatal("disabled user completed a reset")
	}
}

func TestPostgresQueryCodeRecoveryAuditFailureRollsBack(t *testing.T) {
	f := newRecoveryFixture(t)
	cn, userID, oldCode := f.addEligibleUser("ROLLBACK", "rollback@example.net")
	f.addQuerySessions(userID, 1)
	token := f.issueToken(cn, f.ip("rollback"))
	functionName := "reject_query_recovery_audit_" + time.Now().Format("150405000000")
	triggerName := functionName + "_trigger"
	_, err := f.pool.Exec(context.Background(), fmt.Sprintf(`
		create function %s() returns trigger language plpgsql as $$
		begin
			if new.action = 'query_code_recovery_completed' then raise exception 'audit rejected'; end if;
			return new;
		end $$;
		create trigger %s before insert on account_security_audit_logs for each row execute function %s();
	`, functionName, triggerName, functionName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), fmt.Sprintf(`drop trigger if exists %s on account_security_audit_logs; drop function if exists %s()`, triggerName, functionName))
	})
	service := NewService(f.store, f.protector, recoveryemailverification.NewFakeSender(), f.manager)
	if err := service.Reset(context.Background(), token, "RollbackCode-444", "RollbackCode-444"); err == nil {
		t.Fatal("audit failure did not abort reset")
	}
	var storedHash string
	if err := f.pool.QueryRow(context.Background(), `select query_code_hash from users where id=$1::uuid`, userID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(oldCode)) != nil {
		t.Fatal("query code changed despite transaction rollback")
	}
	f.assertCount(`select count(*)::int from query_sessions where user_id=$1::uuid`, 1, userID)
	f.assertCount(`select count(*)::int from query_code_recovery_sessions where user_id=$1::uuid and status='active'`, 1, userID)
}

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	// Own throwaway database; no backend/.env, no DATABASE_URL.
	pool := testdb.New(t, "querycoderecovery")
	var err error
	f := &recoveryFixture{t: t, pool: pool, prefix: "TEST_QUERY_CODE_RECOVERY_" + time.Now().Format("20060102150405.000000000"), startedAt: time.Now().Add(-time.Second)}
	f.cleanup()
	t.Cleanup(f.cleanup)
	f.adminID = f.addAdmin()
	encryptionKey, lookupKey, hmacKey := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	for _, key := range [][]byte{encryptionKey, lookupKey, hmacKey} {
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
	}
	f.protector, err = recoveryemail.NewProtector(encryptionKey, lookupKey)
	if err != nil {
		t.Fatal(err)
	}
	f.manager, err = NewManager(hmacKey)
	if err != nil {
		t.Fatal(err)
	}
	f.store = NewPostgresStore(pool, f.manager)
	return f
}

func (f *recoveryFixture) addAdmin() string {
	var id string
	if err := f.pool.QueryRow(context.Background(), `insert into admins (username,password_hash,display_name,role,status) values ($1,$2,'Recovery test admin','admin','active') returning id::text`, f.prefix+"_ADMIN", "TEST_QUERY_CODE_RECOVERY_HASH").Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *recoveryFixture) addEligibleUser(suffix string, address string) (string, string, string) {
	cn := f.prefix + "_" + suffix
	queryCode := "FixtureCode-123"
	hash, err := bcrypt.GenerateFromPassword([]byte(queryCode), bcrypt.MinCost)
	if err != nil {
		f.t.Fatal(err)
	}
	var userID string
	if err := f.pool.QueryRow(context.Background(), `insert into users (cn_code,display_name,query_code_hash,status) values ($1,'Recovery test user',$2,'active') returning id::text`, cn, string(hash)).Scan(&userID); err != nil {
		f.t.Fatal(err)
	}
	protected, err := f.protector.Protect(address)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := users.NewPostgresStore(f.pool).PutRecoveryEmail(context.Background(), userID, f.adminID, "query code recovery integration fixture", protected); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `update user_recovery_emails set status='verified',verified_at=now(),updated_at=now() where user_id=$1::uuid and invalidated_at is null`, userID); err != nil {
		f.t.Fatal(err)
	}
	return cn, userID, queryCode
}

func (f *recoveryFixture) issueToken(cn string, ip string) string {
	sender := recoveryemailverification.NewFakeSender()
	service := NewService(f.store, f.protector, sender, f.manager)
	if err := service.Request(context.Background(), cn, ip); err != nil || len(sender.Deliveries()) != 1 {
		f.t.Fatal("could not prepare recovery reset capability")
	}
	result, err := service.Verify(context.Background(), cn, sender.Deliveries()[0].Code)
	if err != nil {
		f.t.Fatal("could not verify recovery fixture")
	}
	return result.ResetToken
}

func (f *recoveryFixture) addQuerySessions(userID string, count int) {
	for index := 0; index < count; index++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", f.prefix, index)))
		if _, err := f.pool.Exec(context.Background(), `insert into query_sessions (user_id,token_hash,expires_at) values ($1::uuid,$2,now()+interval '1 hour')`, userID, hex.EncodeToString(sum[:])); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *recoveryFixture) ageLatestCode(userID string) {
	if _, err := f.pool.Exec(context.Background(), `update query_code_recovery_codes set created_at=created_at-interval '61 seconds' where id=(select id from query_code_recovery_codes where user_id=$1::uuid order by created_at desc limit 1)`, userID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *recoveryFixture) ip(suffix string) string {
	ip := "test-ip-" + f.prefix + "-" + suffix
	f.ips = append(f.ips, ip)
	return ip
}

func (f *recoveryFixture) assertCount(query string, want int, args ...any) {
	var got int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&got); err != nil {
		f.t.Fatal(err)
	}
	if got != want {
		f.t.Fatalf("count = %d, want %d", got, want)
	}
}

func (f *recoveryFixture) assertSafeStorage(userID string) {
	f.assertCount(`select count(*)::int from query_code_recovery_codes where user_id=$1::uuid and (length(code_hash)<>64 or code_hash !~ '^[0-9a-f]{64}$' or purpose<>'query_code_recovery')`, 0, userID)
	f.assertCount(`select count(*)::int from query_code_recovery_sessions where user_id=$1::uuid and (length(token_hash)<>64 or token_hash !~ '^[0-9a-f]{64}$' or purpose<>'query_code_recovery')`, 0, userID)
	f.assertCount(`select count(*)::int from account_security_audit_logs where target_user_id=$1::uuid and metadata ?| array['code','code_hash','reset_token','token_hash','query_code','cookie','session_token','encrypted_email','email_lookup_hash']`, 0, userID)
}

func (f *recoveryFixture) cleanup() {
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
		f.t.Fatalf("refusing query recovery cleanup: orders=%d payments=%d merges=%d", orders, payments, merges)
	}
	statements := []string{
		`delete from recovery_email_verification_codes where user_id in (select id from users where cn_code like $1)`,
		`delete from query_code_recovery_sessions where user_id in (select id from users where cn_code like $1)`,
		`delete from query_code_recovery_codes where user_id in (select id from users where cn_code like $1)`,
		`delete from account_security_audit_logs where target_user_id in (select id from users where cn_code like $1)`,
		`delete from query_sessions where user_id in (select id from users where cn_code like $1)`,
		`delete from user_recovery_emails where user_id in (select id from users where cn_code like $1)`,
		`delete from users where cn_code like $1`,
		`delete from admin_sessions where admin_id in (select id from admins where username like $1)`,
		`delete from admins where username like $1`,
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement, pattern); err != nil {
			f.t.Fatalf("cleanup query recovery fixture: %v", err)
		}
	}
	for _, ip := range f.ips {
		if f.manager == nil {
			continue
		}
		hash, err := f.manager.IdentifierHash("ip", ip)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `delete from query_code_recovery_request_events where ip_hash=$1`, hash); err != nil {
			f.t.Fatal(err)
		}
	}
	f.assertCount(`select count(*)::int from users where cn_code like $1`, 0, pattern)
}

func differentRecoveryCode(code string) string {
	if code[0] == '0' {
		return "1" + code[1:]
	}
	return "0" + code[1:]
}
