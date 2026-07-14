package query

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/recoveryemail"
)

type queryRecoveryEmailReader struct {
	record RecoveryEmailRecordForTest
	userID string
	err    error
}

type RecoveryEmailRecordForTest = recoveryemail.Record

func (s *queryRecoveryEmailReader) GetRecoveryEmail(_ context.Context, userID string) (recoveryemail.Record, error) {
	s.userID = userID
	return s.record, s.err
}

func TestQueryRecoveryEmailRequiresSession(t *testing.T) {
	handler := NewHandler(&changeCodeStore{}, time.Hour, false)
	handler.ConfigureRecoveryEmail(&queryRecoveryEmailReader{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/query/recovery-email", nil)
	response := httptest.NewRecorder()
	handler.RecoveryEmail(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestQueryRecoveryEmailReturnsOwnSafeState(t *testing.T) {
	protector := queryRecoveryProtector(t)
	protected, err := protector.Protect("User.Account@EXAMPLE.COM")
	if err != nil {
		t.Fatal(err)
	}
	reader := &queryRecoveryEmailReader{record: recoveryemail.Record{
		HasRecoveryEmail: true,
		EncryptedEmail:   protected.Encrypted,
		Status:           "pending",
		UpdatedAt:        "2026-07-14T00:00:00Z",
	}}
	handler := NewHandler(&changeCodeStore{user: User{ID: "current-user", CNCode: "CURRENT", Status: "active"}}, time.Hour, false)
	handler.ConfigureRecoveryEmail(reader, protector)
	request := httptest.NewRequest(http.MethodGet, "/api/query/recovery-email?user_id=other-user&cn=OTHER", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "query-session-placeholder"})
	response := httptest.NewRecorder()
	handler.RecoveryEmail(response, request)
	if response.Code != http.StatusOK || reader.userID != "current-user" {
		t.Fatalf("status/user scope = %d/%q", response.Code, reader.userID)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"masked_email":"U***@example.com"`) || !strings.Contains(body, recoveryEmailFoundationMessage) {
		t.Fatalf("safe response is incomplete: %s", body)
	}
	for _, forbidden := range []string{"User.Account@", "encrypted_email", "email_lookup_hash", "user_id", "admin"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response contains forbidden value %q", forbidden)
		}
	}
}

func TestQueryRecoveryEmailReturnsStableEmptyStateWithoutKeys(t *testing.T) {
	reader := &queryRecoveryEmailReader{record: recoveryemail.Record{HasRecoveryEmail: false}}
	handler := NewHandler(&changeCodeStore{user: User{ID: "current-user", Status: "active"}}, time.Hour, false)
	handler.ConfigureRecoveryEmail(reader, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/query/recovery-email", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "query-session-placeholder"})
	response := httptest.NewRecorder()
	handler.RecoveryEmail(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"has_recovery_email":false`) {
		t.Fatalf("empty state = %d/%s", response.Code, response.Body.String())
	}
}

func queryRecoveryProtector(t *testing.T) *recoveryemail.Protector {
	t.Helper()
	encryptionKey := make([]byte, 32)
	hmacKey := make([]byte, 48)
	if _, err := rand.Read(encryptionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(hmacKey); err != nil {
		t.Fatal(err)
	}
	protector, err := recoveryemail.NewProtector(encryptionKey, hmacKey)
	if err != nil {
		t.Fatal(err)
	}
	return protector
}
