package paymentsubmission

import (
	"context"
	"sync"
	"testing"
)

// Proof uploads are slow over a phone uplink (production: 10.3 s for 1.25 MB,
// with attempts abandoned at 24 s and 188 s). Users retry. Every retry that
// reached the server used to create a second 收肾记录 for the same payment, which
// an admin then had to reconcile by hand. These tests pin the deduplication.

// proofInput builds a Create input for one user with an explicit idempotency
// key, reusing the existing validatedProof helper (real PNG bytes run through
// the real validator) and the production MethodAlipay constant.
//
// The payment method is set HERE and nowhere else in this file on purpose:
// validatedProof deliberately leaves PaymentMethod at its zero value, and
// Create rejects "" with ErrInvalidMethod ("付款方式无效") before any idempotency
// code runs. The first version of these tests forgot to set it and every one of
// them failed on that check, never reaching the behaviour under test.
func proofInput(t *testing.T, user submissionCase, requestID string) CreateInput {
	t.Helper()
	input := validatedProof(t)
	input.UserID = user.UserID
	input.CNCode = user.CN
	input.PaymentMethod = MethodAlipay
	input.RequestID = requestID
	return input
}

func TestRetryWithSameRequestIDDoesNotCreateASecondSubmission(t *testing.T) {
	fixture := newSubmissionFixture(t)
	user := fixture.createUser(t, "IDEMP", 100)
	input := proofInput(t, user, "retry-key-0001")

	first, err := fixture.store.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.Deduplicated {
		t.Fatal("the first submission was reported as a duplicate")
	}

	second, err := fixture.store.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("retry created a new submission: %s vs %s", second.ID, first.ID)
	}
	if !second.Deduplicated {
		t.Fatal("retry was not reported as deduplicated")
	}
	if second.PayableAmount != first.PayableAmount || second.SubmittedAt != first.SubmittedAt {
		t.Fatal("retry did not replay the original result verbatim")
	}
	fixture.assertSubmissionCount(t, user.UserID, 1)
}

// Two retries landing at once (the user double-taps, or a stalled request is
// retried while the original is still in flight) must still yield one row.
func TestConcurrentRetriesWithSameRequestIDCreateOneSubmission(t *testing.T) {
	fixture := newSubmissionFixture(t)
	user := fixture.createUser(t, "IDEMPRACE", 100)
	input := proofInput(t, user, "race-key-0001")

	var wait sync.WaitGroup
	results := make(chan UserSubmission, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			submission, err := fixture.store.Create(context.Background(), input)
			if err != nil {
				errs <- err
				return
			}
			results <- submission
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent create: %v", err)
	}

	ids := map[string]bool{}
	for submission := range results {
		ids[submission.ID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("concurrent retries produced %d distinct submissions, want 1", len(ids))
	}
	fixture.assertSubmissionCount(t, user.UserID, 1)
}

// Deduplication is scoped to one user. Two people uploading the same screenshot
// — a shared group payment, say — are two genuine proofs, and hashing the image
// globally would have silently merged them. validatedProof returns identical
// bytes every call, so both users really do submit the same image here.
func TestSameRequestIDFromDifferentUsersStaysSeparate(t *testing.T) {
	fixture := newSubmissionFixture(t)
	first := fixture.createUser(t, "IDEMPA", 100)
	second := fixture.createUser(t, "IDEMPB", 100)
	const sharedKey = "collision-key-0001"

	firstSubmission, err := fixture.store.Create(context.Background(), proofInput(t, first, sharedKey))
	if err != nil {
		t.Fatalf("first user create: %v", err)
	}
	secondSubmission, err := fixture.store.Create(context.Background(), proofInput(t, second, sharedKey))
	if err != nil {
		t.Fatalf("second user create: %v", err)
	}

	if secondSubmission.ID == firstSubmission.ID {
		t.Fatal("two users' submissions were merged by a shared request id")
	}
	if secondSubmission.Deduplicated {
		t.Fatal("another user's submission was reported as a duplicate")
	}
	fixture.assertSubmissionCount(t, first.UserID, 1)
	fixture.assertSubmissionCount(t, second.UserID, 1)
}

// The same user legitimately submitting the same screenshot twice — different
// orders, or a genuine second payment — must NOT be collapsed. Only an explicit
// retry (same request id) is deduplicated.
func TestSameImageWithDifferentRequestIDsCreatesTwoSubmissions(t *testing.T) {
	fixture := newSubmissionFixture(t)
	user := fixture.createUser(t, "IDEMPTWICE", 100)

	first, err := fixture.store.Create(context.Background(), proofInput(t, user, "first-key-0001"))
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := fixture.store.Create(context.Background(), proofInput(t, user, "second-key-0002"))
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("the same image was deduplicated across two distinct submissions")
	}
	fixture.assertSubmissionCount(t, user.UserID, 2)
}

// A missing or malformed key degrades to a plain insert: a broken client must
// never be blocked from filing a real payment proof. Every one of these lands
// as request_id NULL, which the partial unique index ignores entirely.
func TestAbsentOrMalformedRequestIDStillCreatesSubmissions(t *testing.T) {
	fixture := newSubmissionFixture(t)
	user := fixture.createUser(t, "IDEMPNONE", 100)

	requestIDs := []string{"", "   ", "short", "bad key with spaces", "nul\x00byte"}
	for _, requestID := range requestIDs {
		if _, err := fixture.store.Create(context.Background(), proofInput(t, user, requestID)); err != nil {
			t.Fatalf("create with request id %q: %v", requestID, err)
		}
	}
	fixture.assertSubmissionCount(t, user.UserID, len(requestIDs))

	var nulls int
	if err := fixture.pool.QueryRow(context.Background(),
		`select count(*)::int from payment_submissions where user_id = $1::uuid and request_id is null`,
		user.UserID).Scan(&nulls); err != nil {
		t.Fatalf("count null request ids: %v", err)
	}
	if nulls != len(requestIDs) {
		t.Fatalf("rows with null request_id = %d, want %d", nulls, len(requestIDs))
	}
}

func (f submissionFixture) assertSubmissionCount(t *testing.T, userID string, want int) {
	t.Helper()
	var got int
	if err := f.pool.QueryRow(context.Background(),
		`select count(*)::int from payment_submissions where user_id = $1::uuid`, userID).Scan(&got); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if got != want {
		t.Fatalf("submission count = %d, want %d", got, want)
	}
}
