package users

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type releaserStub struct {
	releasedCNs []string
}

func (s *releaserStub) ReleaseLoginLockForCN(cn string) {
	s.releasedCNs = append(s.releasedCNs, cn)
}

func postQueryCode(t *testing.T, handler *Handler, body string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/users/x/query-code", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.UpdateQueryCode(recorder, request, "11111111-1111-1111-1111-111111111111")
	return recorder.Code
}

// An admin resetting a query code has to clear the user's login-side rate
// limit too. Otherwise the user is handed a fresh code that the limiter still
// refuses — the exact dead end that made users re-request resets in a loop.
func TestAdminQueryCodeResetReleasesLoginLock(t *testing.T) {
	releaser := &releaserStub{}
	handler := NewHandler(&stubStore{})
	handler.ConfigureLoginLockReleaser(releaser)

	if code := postQueryCode(t, handler, `{"query_code":"NewCode-123"}`); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(releaser.releasedCNs) != 1 || releaser.releasedCNs[0] != "succ" {
		t.Fatalf("released CNs = %v, want exactly the updated user's CN", releaser.releasedCNs)
	}
}

// A rejected write must not clear anything: the release is justified by the
// new code existing, not by the request having been made.
func TestFailedAdminQueryCodeResetDoesNotReleaseLoginLock(t *testing.T) {
	releaser := &releaserStub{}
	handler := NewHandler(&stubStore{detailErr: ErrUserNotFound})
	handler.ConfigureLoginLockReleaser(releaser)

	if code := postQueryCode(t, handler, `{"query_code":"NewCode-123"}`); code == http.StatusOK {
		t.Fatal("a failing store still returned 200")
	}
	if len(releaser.releasedCNs) != 0 {
		t.Fatalf("released CNs = %v, want none", releaser.releasedCNs)
	}
}

// The hook is optional; an unconfigured handler must still work.
func TestAdminQueryCodeResetWorksWithoutReleaser(t *testing.T) {
	handler := NewHandler(&stubStore{})
	if code := postQueryCode(t, handler, `{"query_code":"NewCode-123"}`); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}

// The admin path must apply the same normalization as every other flow, so a
// pasted trailing space cannot store a code the user can never type.
func TestAdminQueryCodeResetNormalizesInput(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)
	if code := postQueryCode(t, handler, `{"query_code":"  NewCode-123  "}`); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if store.queryCodeHash == "" {
		t.Fatal("query code was not stored")
	}
	if !bcryptMatches(store.queryCodeHash, "NewCode-123") {
		t.Fatal("stored hash does not match the trimmed query code")
	}
	if bcryptMatches(store.queryCodeHash, "  NewCode-123  ") {
		t.Fatal("stored hash matches the untrimmed query code")
	}
}

func bcryptMatches(hash string, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}
