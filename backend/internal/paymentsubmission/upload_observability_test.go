package paymentsubmission

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall"
	"testing"
)

// A malformed idempotency key must degrade to "no key" — a plain, non-
// deduplicated insert — rather than an error. Blocking a real payment proof
// because a client sent a weird string would be a far worse failure than
// occasionally not deduplicating.
func TestNormalizeRequestIDAcceptsSafeKeysAndDropsTheRest(t *testing.T) {
	accepted := []struct {
		name  string
		value string
		want  string
	}{
		{"uuid", "3f2504e0-4f89-11d3-9a0c-0305e82c3301", "3f2504e0-4f89-11d3-9a0c-0305e82c3301"},
		{"hex fallback", "9f86d081884c7d659a2feaa0", "9f86d081884c7d659a2feaa0"},
		{"underscores", "retry_key_0001", "retry_key_0001"},
		{"surrounding space trimmed", "  retry-key-0001  ", "retry-key-0001"},
		{"minimum length", "abcd1234", "abcd1234"},
	}
	for _, test := range accepted {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeRequestID(test.value); got != test.want {
				t.Fatalf("normalizeRequestID(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}

	rejected := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"too short", "abc123"},
		{"too long", string(make([]byte, 101))},
		{"spaces inside", "retry key 0001"},
		{"sql-ish punctuation", "key';drop table--"},
		{"null byte", "key\x0000001"},
		{"newline", "key0001\nkey0002"},
		{"unicode", "钥匙0001"},
	}
	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeRequestID(test.value); got != "" {
				t.Fatalf("normalizeRequestID(%q) = %q, want \"\"", test.value, got)
			}
		})
	}
}

// An absent key must leave the column NULL so the partial unique index ignores
// the row entirely; otherwise every keyless submission would collide with the
// previous one on the empty string.
func TestNullableRequestIDKeepsAbsentKeysNull(t *testing.T) {
	if got := nullableRequestID(""); got != nil {
		t.Fatalf("nullableRequestID(\"\") = %v, want nil", got)
	}
	if got := nullableRequestID("retry-key-0001"); got != "retry-key-0001" {
		t.Fatalf("nullableRequestID = %v, want the key itself", got)
	}
}

// Production could only see Caddy's status=0 for an abandoned 188-second
// upload, which is indistinguishable from a server error in an access log.
// These categories are what make the next report actionable.
func TestClientAbortCategoryDistinguishesDisconnectsFromRealFailures(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want string
	}{
		{"context canceled", context.Background(), context.Canceled, "client disconnected"},
		{"request context canceled", canceledCtx, errors.New("read error"), "client disconnected"},
		{"connection reset", context.Background(), syscall.ECONNRESET, "client disconnected"},
		{"broken pipe", context.Background(), syscall.EPIPE, "client disconnected"},
		{"wrapped reset text", context.Background(), errors.New("read tcp: connection reset by peer"), "client disconnected"},
		{"unexpected eof", context.Background(), io.ErrUnexpectedEOF, "request body read canceled"},
		{"eof", context.Background(), io.EOF, "request body read canceled"},
		{"nil error is not an abort", context.Background(), nil, ""},
		{"genuine failure is not an abort", context.Background(), errors.New("invalid multipart boundary"), ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clientAbortCategory(test.ctx, test.err); got != test.want {
				t.Fatalf("clientAbortCategory = %q, want %q", got, test.want)
			}
		})
	}
}

// An over-limit body is a real rejection the user must be told about, not a
// disconnect to swallow: misclassifying it would leave the client with no
// response at all.
func TestOversizeBodyIsNotTreatedAsAClientDisconnect(t *testing.T) {
	err := &http.MaxBytesError{Limit: MaxImageBytes}
	if got := clientAbortCategory(context.Background(), err); got != "" {
		t.Fatalf("clientAbortCategory(MaxBytesError) = %q, want \"\" so the 400 is still sent", got)
	}
	if got := clientAbortCategory(context.Background(), fmt.Errorf("parse: %w", err)); got != "" {
		t.Fatalf("wrapped MaxBytesError = %q, want \"\"", got)
	}
}

// The idempotency integration tests first failed 5/5 with "付款方式无效", never
// reaching the behaviour they exist to check: validatedProof deliberately
// leaves PaymentMethod at its zero value, and Create rejects "" up front. This
// pins both halves of that contract — and needs no database, so the trap is
// caught here rather than rediscovered against a live one.
func TestValidatedProofNeedsAnExplicitPaymentMethod(t *testing.T) {
	bare := validatedProof(t)
	if bare.PaymentMethod != "" {
		t.Fatalf("validatedProof now sets PaymentMethod=%q; update the helpers that compensate for it", bare.PaymentMethod)
	}
	if normalized := normalizeMethod(bare.PaymentMethod); normalized == MethodAlipay || normalized == MethodWechat {
		t.Fatal("the zero-value payment method normalized to a valid one; the guard below would be meaningless")
	}

	filled := proofInput(t, submissionCase{UserID: "user", CN: "CN"}, "retry-key-0001")
	if filled.PaymentMethod != MethodAlipay {
		t.Fatalf("proofInput PaymentMethod = %q, want the production MethodAlipay constant", filled.PaymentMethod)
	}
	if normalizeMethod(filled.PaymentMethod) != MethodAlipay {
		t.Fatal("proofInput produced a method the production normalizer does not accept")
	}
	if filled.RequestID != "retry-key-0001" || filled.UserID != "user" || filled.CNCode != "CN" {
		t.Fatal("proofInput did not carry the caller's user and request id through")
	}
	if len(filled.ImageData) == 0 || filled.SHA256 == "" {
		t.Fatal("proofInput lost the validated image payload")
	}
}
