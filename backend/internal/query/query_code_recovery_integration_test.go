package query

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/querycoderecovery"
	"pjsk/backend/internal/recoveryemail"
	"pjsk/backend/internal/recoveryemailverification"
	"pjsk/backend/internal/users"
)

func TestQueryCodeRecoveryHTTPIntegration(t *testing.T) {
	pool := newQueryTestPool(t)
	prefix := "TEST_QUERY_CODE_RECOVERY_HTTP_" + time.Now().Format("20060102150405.000000000")
	ctx := context.Background()
	cleanupQueryCodeRecoveryHTTP(t, pool, prefix, nil)

	var adminID string
	if err := pool.QueryRow(ctx, `insert into admins (username,password_hash,display_name,role,status) values ($1,$2,'Recovery HTTP admin','admin','active') returning id::text`, prefix+"_ADMIN", "TEST_QUERY_CODE_RECOVERY_HASH").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	oldQueryCode := "FixtureCode-123"
	oldHash, err := bcrypt.GenerateFromPassword([]byte(oldQueryCode), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cn := prefix + "_USER"
	var userID string
	if err := pool.QueryRow(ctx, `insert into users (cn_code,display_name,query_code_hash,status) values ($1,'Recovery HTTP user',$2,'active') returning id::text`, cn, string(oldHash)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	encryptionKey, lookupKey, recoveryKey := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	for _, key := range [][]byte{encryptionKey, lookupKey, recoveryKey} {
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
	}
	protector, err := recoveryemail.NewProtector(encryptionKey, lookupKey)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := protector.Protect("http-recovery@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.NewPostgresStore(pool).PutRecoveryEmail(ctx, userID, adminID, "query recovery HTTP fixture", protected); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update user_recovery_emails set status='verified',verified_at=now(),updated_at=now() where user_id=$1::uuid and invalidated_at is null`, userID); err != nil {
		t.Fatal(err)
	}
	manager, err := querycoderecovery.NewManager(recoveryKey)
	if err != nil {
		t.Fatal(err)
	}
	ips := []string{"10.50.0.1", "10.50.0.2"}
	t.Cleanup(func() { cleanupQueryCodeRecoveryHTTP(t, pool, prefix, recoveryEventHashes(t, manager, ips)) })
	sender := recoveryemailverification.NewFakeSender()
	handler := NewHandler(NewPostgresStore(pool), time.Hour, false)
	handler.ConfigureQueryCodeRecovery(querycoderecovery.NewService(querycoderecovery.NewPostgresStore(pool, manager), protector, sender, manager))

	eligibleBody, _ := json.Marshal(map[string]string{"cn": cn})
	eligibleRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery/request", bytes.NewReader(eligibleBody))
	eligibleRequest.RemoteAddr = ips[0] + ":40000"
	eligibleResponse := httptest.NewRecorder()
	handler.RequestQueryCodeRecovery(eligibleResponse, eligibleRequest)
	if eligibleResponse.Code != http.StatusOK || len(sender.Deliveries()) != 1 {
		t.Fatal("eligible HTTP request did not complete through fake delivery")
	}

	missingBody, _ := json.Marshal(map[string]string{"cn": prefix + "_MISSING"})
	missingRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery/request", bytes.NewReader(missingBody))
	missingRequest.RemoteAddr = ips[1] + ":40000"
	missingResponse := httptest.NewRecorder()
	handler.RequestQueryCodeRecovery(missingResponse, missingRequest)
	if missingResponse.Code != eligibleResponse.Code || missingResponse.Body.String() != eligibleResponse.Body.String() {
		t.Fatal("public HTTP response exposed account eligibility")
	}
	if strings.Contains(strings.ToLower(eligibleResponse.Body.String()), "email") || strings.Contains(eligibleResponse.Body.String(), "@") {
		t.Fatal("public request response exposed email information")
	}

	verifyBody, _ := json.Marshal(map[string]string{"cn": cn, "code": sender.Deliveries()[0].Code})
	verifyRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery/verify", bytes.NewReader(verifyBody))
	verifyResponse := httptest.NewRecorder()
	handler.VerifyQueryCodeRecovery(verifyResponse, verifyRequest)
	if verifyResponse.Code != http.StatusOK {
		t.Fatal("valid HTTP verification did not succeed")
	}
	var verified queryCodeRecoveryResponse
	if err := json.Unmarshal(verifyResponse.Body.Bytes(), &verified); err != nil || !querycoderecovery.ValidToken(verified.ResetToken) {
		t.Fatal("HTTP verification did not return one valid reset capability")
	}

	loginBefore := loginRecoveryHTTP(t, handler, cn, oldQueryCode, ips[0], http.StatusOK)
	if loginBefore == nil {
		t.Fatal("fixture query login did not create a session")
	}
	newQueryCode := "RecoveredCode-456"
	resetBody, _ := json.Marshal(map[string]string{"reset_token": verified.ResetToken, "new_query_code": newQueryCode, "confirm_query_code": newQueryCode})
	resetRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery/reset", bytes.NewReader(resetBody))
	resetResponse := httptest.NewRecorder()
	handler.ResetRecoveredQueryCode(resetResponse, resetRequest)
	if resetResponse.Code != http.StatusOK || strings.Contains(resetResponse.Body.String(), newQueryCode) || strings.Contains(resetResponse.Body.String(), verified.ResetToken) {
		t.Fatal("HTTP reset response was unsuccessful or reflected sensitive input")
	}
	foundOldSession := httptest.NewRequest(http.MethodGet, "/api/query/orders", nil)
	foundOldSession.AddCookie(loginBefore)
	oldSessionResponse := httptest.NewRecorder()
	handler.Orders(oldSessionResponse, foundOldSession)
	if oldSessionResponse.Code != http.StatusUnauthorized {
		t.Fatal("pre-reset query session remained valid")
	}
	loginRecoveryHTTP(t, handler, cn, oldQueryCode, ips[0], http.StatusUnauthorized)
	loginRecoveryHTTP(t, handler, cn, newQueryCode, ips[0], http.StatusOK)
}

func loginRecoveryHTTP(t *testing.T, handler *Handler, cn string, code string, ip string, wantStatus int) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"cn": cn, "query_code": code})
	request := httptest.NewRequest(http.MethodPost, "/api/query/login", bytes.NewReader(body))
	request.RemoteAddr = ip + ":41000"
	response := httptest.NewRecorder()
	handler.Login(response, request)
	if response.Code != wantStatus {
		t.Fatalf("query login status = %d, want %d", response.Code, wantStatus)
	}
	if wantStatus != http.StatusOK {
		return nil
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	return nil
}

func recoveryEventHashes(t *testing.T, manager *querycoderecovery.Manager, ips []string) []string {
	t.Helper()
	hashes := make([]string, 0, len(ips))
	for _, ip := range ips {
		hash, err := manager.IdentifierHash("ip", ip)
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	return hashes
}

func cleanupQueryCodeRecoveryHTTP(t *testing.T, pool *pgxpool.Pool, prefix string, ipHashes []string) {
	t.Helper()
	ctx := context.Background()
	pattern := prefix + "%"
	var orders, payments, merges int
	if err := pool.QueryRow(ctx, `select count(*)::int from orders where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from payments where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from cn_merge_logs where source_user_id in (select id from users where cn_code like $1) or target_user_id in (select id from users where cn_code like $1)`, pattern).Scan(&merges); err != nil {
		t.Fatal(err)
	}
	if orders != 0 || payments != 0 || merges != 0 {
		t.Fatalf("refusing HTTP recovery cleanup: orders=%d payments=%d merges=%d", orders, payments, merges)
	}
	statements := []string{
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
		if _, err := pool.Exec(ctx, statement, pattern); err != nil {
			t.Fatal(err)
		}
	}
	for _, hash := range ipHashes {
		if _, err := pool.Exec(ctx, `delete from query_code_recovery_request_events where ip_hash=$1`, hash); err != nil {
			t.Fatal(err)
		}
	}
}
