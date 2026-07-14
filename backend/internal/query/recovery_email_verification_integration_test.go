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

	"pjsk/backend/internal/recoveryemailverification"
	"pjsk/backend/internal/users"
)

func TestRecoveryEmailVerificationHTTPIntegration(t *testing.T) {
	pool := newQueryTestPool(t)
	prefix := "TEST_RECOVERY_VERIFY_HTTP_" + time.Now().Format("20060102150405.000000000")
	cleanupQueryRecoveryEmailFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupQueryRecoveryEmailFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertQueryRecoveryAdmin(t, pool, prefix)
	userID := insertQueryRecoveryUser(t, pool, prefix+"_USER")
	protector := queryRecoveryProtector(t)
	protected, err := protector.Protect("http-verification@example.com")
	if err != nil {
		t.Fatal(err)
	}
	userStore := users.NewPostgresStore(pool)
	if _, err := userStore.PutRecoveryEmail(ctx, userID, adminID, "verification HTTP integration", protected); err != nil {
		t.Fatal(err)
	}
	verificationKey := make([]byte, 32)
	if _, err := rand.Read(verificationKey); err != nil {
		t.Fatal(err)
	}
	manager, err := recoveryemailverification.NewManager(verificationKey)
	if err != nil {
		t.Fatal(err)
	}
	sender := recoveryemailverification.NewFakeSender()
	service := recoveryemailverification.NewService(
		recoveryemailverification.NewPostgresStore(pool, manager), protector, sender, manager.Policy(),
	)
	handler := NewHandler(NewPostgresStore(pool), time.Hour, false)
	handler.ConfigureRecoveryEmail(userStore, protector)
	handler.ConfigureRecoveryEmailVerification(service)
	cookie := loginQueryRecoveryUser(t, handler, prefix+"_USER")

	sendRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery-email/send-verification?user_id=other", nil)
	sendRequest.AddCookie(cookie)
	sendResponse := httptest.NewRecorder()
	handler.SendRecoveryEmailVerification(sendResponse, sendRequest)
	if sendResponse.Code != http.StatusOK || len(sender.Deliveries()) != 1 {
		t.Fatalf("send response = %d, deliveries=%d", sendResponse.Code, len(sender.Deliveries()))
	}
	delivery := sender.Deliveries()[0]
	if delivery.To != "http-verification@example.com" {
		t.Fatal("fake sender did not receive the current recovery email")
	}
	for _, forbidden := range []string{delivery.Code, delivery.To, "code_hash", "encrypted_email", "email_lookup_hash", userID} {
		if strings.Contains(sendResponse.Body.String(), forbidden) {
			t.Fatalf("send response contains forbidden content")
		}
	}

	verifyBody, err := json.Marshal(map[string]string{"code": delivery.Code})
	if err != nil {
		t.Fatal(err)
	}
	verifyRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery-email/verify", bytes.NewReader(verifyBody))
	verifyRequest.AddCookie(cookie)
	verifyResponse := httptest.NewRecorder()
	handler.VerifyRecoveryEmail(verifyResponse, verifyRequest)
	if verifyResponse.Code != http.StatusOK || !strings.Contains(verifyResponse.Body.String(), `"status":"verified"`) {
		t.Fatalf("verify response status = %d", verifyResponse.Code)
	}
	if strings.Contains(verifyResponse.Body.String(), delivery.Code) || strings.Contains(verifyResponse.Body.String(), delivery.To) {
		t.Fatal("verify response contains sensitive content")
	}

	stateResponse := queryRecoveryEmailRequest(t, handler, cookie, "/api/query/recovery-email")
	if stateResponse.Code != http.StatusOK || !strings.Contains(stateResponse.Body.String(), `"status":"verified"`) {
		t.Fatalf("verified state response = %d", stateResponse.Code)
	}

	unavailable := NewHandler(NewPostgresStore(pool), time.Hour, false)
	unavailableRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery-email/send-verification", nil)
	unavailableRequest.AddCookie(cookie)
	unavailableResponse := httptest.NewRecorder()
	unavailable.SendRecoveryEmailVerification(unavailableResponse, unavailableRequest)
	if unavailableResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured service status = %d", unavailableResponse.Code)
	}

	expiredUserID := insertQueryRecoveryUser(t, pool, prefix+"_EXPIRED")
	expiredCookie := loginQueryRecoveryUser(t, handler, prefix+"_EXPIRED")
	if _, err := pool.Exec(ctx, `update query_sessions set expires_at=now()-interval '1 minute' where user_id=$1::uuid`, expiredUserID); err != nil {
		t.Fatal(err)
	}
	expiredRequest := httptest.NewRequest(http.MethodPost, "/api/query/recovery-email/send-verification", nil)
	expiredRequest.AddCookie(expiredCookie)
	expiredResponse := httptest.NewRecorder()
	handler.SendRecoveryEmailVerification(expiredResponse, expiredRequest)
	if expiredResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d", expiredResponse.Code)
	}
}
