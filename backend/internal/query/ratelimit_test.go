package query

import (
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterRepeatedFailures(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	for i := 0; i < limiter.maxFailures; i++ {
		if !limiter.allow("1.2.3.4", "succ", now) {
			t.Fatalf("attempt %d should be allowed before block", i+1)
		}
		limiter.recordFailure("1.2.3.4", "succ", now)
	}

	if limiter.allow("1.2.3.4", "succ", now) {
		t.Fatal("pair should be blocked after max failures")
	}
	if !limiter.allow("1.2.3.4", "other-cn", now) {
		t.Fatal("different CN from same IP should still be allowed")
	}
	if !limiter.allow("5.6.7.8", "succ", now) {
		t.Fatal("same CN from different IP should still be allowed")
	}

	afterBlock := now.Add(limiter.blockDuration + time.Second)
	if !limiter.allow("1.2.3.4", "succ", afterBlock) {
		t.Fatal("block should expire after blockDuration")
	}
}

func TestLoginLimiterSuccessClearsFailures(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	for i := 0; i < limiter.maxFailures-1; i++ {
		limiter.allow("1.2.3.4", "succ", now)
		limiter.recordFailure("1.2.3.4", "succ", now)
	}
	limiter.recordSuccess("1.2.3.4", "succ")
	limiter.allow("1.2.3.4", "succ", now)
	limiter.recordFailure("1.2.3.4", "succ", now)

	if !limiter.allow("1.2.3.4", "succ", now) {
		t.Fatal("failure count should restart after a successful login")
	}
}

func TestLoginLimiterCapsAttemptsPerIP(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	for i := 0; i < limiter.maxAttempts; i++ {
		if !limiter.allow("1.2.3.4", "cn-"+string(rune('a'+i%26)), now) {
			t.Fatalf("attempt %d within cap should be allowed", i+1)
		}
	}
	if limiter.allow("1.2.3.4", "another", now) {
		t.Fatal("attempts above per-IP cap should be rejected")
	}
	if !limiter.allow("9.9.9.9", "another", now) {
		t.Fatal("other IPs should be unaffected by the cap")
	}

	nextWindow := now.Add(limiter.attemptWindow)
	if !limiter.allow("1.2.3.4", "another", nextWindow) {
		t.Fatal("cap should reset in the next window")
	}
}
