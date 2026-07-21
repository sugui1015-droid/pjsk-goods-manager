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

// release must relieve — not reset — the per-IP attempt gate. A user who just
// set a new query code has to get through immediately, but the gate must
// still be there for the request after the grace slots are used up.
func TestReleaseGrantsBoundedPerIPGrace(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Now()
	const ip = "203.0.113.1"

	for i := 0; i < limiter.maxAttempts; i++ {
		limiter.allow(ip, "cn001", now)
	}
	if limiter.allow(ip, "cn001", now) {
		t.Fatal("per-IP gate did not close after maxAttempts")
	}

	limiter.release(ip, "cn001", now)

	for i := 0; i < releaseGraceAttempts; i++ {
		if !limiter.allow(ip, "cn001", now) {
			t.Fatalf("grace attempt %d was refused", i)
		}
	}
	if limiter.allow(ip, "cn001", now) {
		t.Fatal("per-IP gate was fully reset instead of granted bounded grace")
	}
}

// release must not hand grace to an IP that never had a counter, nor leak
// across IPs.
func TestReleaseDoesNotAffectOtherIPs(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Now()

	for i := 0; i < limiter.maxFailures; i++ {
		limiter.recordFailure("198.51.100.9", "cn001", now)
	}
	limiter.release("203.0.113.1", "cn001", now)

	if limiter.allow("198.51.100.9", "cn001", now) {
		t.Fatal("release on one IP cleared another IP's block for the same CN")
	}
}
