package admin

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

var limiterEpoch = time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

func TestLoginLimiterPerIPWindow(t *testing.T) {
	limiter := newLoginLimiter()
	now := limiterEpoch

	for i := 0; i < limiter.maxAttempts; i++ {
		if !limiter.allow("203.0.113.1", "admin", now) {
			t.Fatalf("attempt %d within the window was denied", i+1)
		}
	}
	if limiter.allow("203.0.113.1", "admin", now) {
		t.Fatal("attempt beyond the per-IP cap was allowed")
	}
	if !limiter.allow("203.0.113.2", "admin", now) {
		t.Fatal("a different IP was affected by another IP's cap")
	}
	if !limiter.allow("203.0.113.1", "admin", now.Add(limiter.attemptWindow)) {
		t.Fatal("the per-IP window did not reset after expiry")
	}
}

func TestLoginLimiterFailureBlockAndRecovery(t *testing.T) {
	limiter := newLoginLimiter()
	now := limiterEpoch

	for i := 0; i < limiter.maxFailures-1; i++ {
		limiter.recordFailure("203.0.113.1", "admin", now)
		if !limiter.allow("203.0.113.1", "admin", now) {
			t.Fatalf("blocked after only %d failures", i+1)
		}
	}
	limiter.recordFailure("203.0.113.1", "admin", now)
	if limiter.allow("203.0.113.1", "admin", now) {
		t.Fatalf("not blocked after %d failures", limiter.maxFailures)
	}
	if !limiter.allow("203.0.113.1", "other-admin", now) {
		t.Fatal("a different username on the same IP was blocked")
	}
	if !limiter.allow("203.0.113.9", "admin", now) {
		t.Fatal("the same username from a different IP was blocked")
	}
	if limiter.allow("203.0.113.1", "admin", now.Add(limiter.blockDuration-time.Second)) {
		t.Fatal("block lifted early")
	}
	if !limiter.allow("203.0.113.1", "admin", now.Add(limiter.blockDuration+time.Second)) {
		t.Fatal("block did not lift after blockDuration")
	}
}

func TestLoginLimiterSuccessClearsOnlyOwnPair(t *testing.T) {
	limiter := newLoginLimiter()
	now := limiterEpoch

	for i := 0; i < limiter.maxFailures-1; i++ {
		limiter.recordFailure("203.0.113.1", "admin", now)
		limiter.recordFailure("203.0.113.2", "admin", now)
	}
	limiter.recordSuccess("203.0.113.1", "admin")

	limiter.recordFailure("203.0.113.1", "admin", now)
	if !limiter.allow("203.0.113.1", "admin", now) {
		t.Fatal("success did not clear the pair's failure count")
	}
	limiter.recordFailure("203.0.113.2", "admin", now)
	if limiter.allow("203.0.113.2", "admin", now) {
		t.Fatal("success on one IP cleared another IP's failures")
	}
}

func TestLoginLimiterLazyCleanupRemovesExpiredKeys(t *testing.T) {
	limiter := newLoginLimiter()
	now := limiterEpoch

	for i := 0; i < 50; i++ {
		ip := fmt.Sprintf("203.0.113.%d", i)
		limiter.allow(ip, "admin", now)
		limiter.recordFailure(ip, "admin", now)
	}
	if len(limiter.attempts) != 50 || len(limiter.failures) != 50 {
		t.Fatalf("setup: attempts=%d failures=%d", len(limiter.attempts), len(limiter.failures))
	}

	later := now.Add(limiter.failureWindow + limiter.blockDuration + time.Minute)
	limiter.allow("198.51.100.1", "admin", later)
	if len(limiter.attempts) != 1 {
		t.Fatalf("expired attempt keys were not cleaned: %d", len(limiter.attempts))
	}
	if len(limiter.failures) != 0 {
		t.Fatalf("expired failure keys were not cleaned: %d", len(limiter.failures))
	}
}

func TestLoginLimiterKeyCapFailsClosed(t *testing.T) {
	limiter := newLoginLimiter()
	limiter.maxTrackedKeys = 3
	now := limiterEpoch

	for i := 0; i < 3; i++ {
		if !limiter.allow(fmt.Sprintf("203.0.113.%d", i), "admin", now) {
			t.Fatalf("key %d within the cap was denied", i)
		}
	}
	if limiter.allow("198.51.100.9", "admin", now) {
		t.Fatal("a new IP beyond the key cap was allowed (must fail closed)")
	}
	if !limiter.allow("203.0.113.1", "admin", now) {
		t.Fatal("an already-tracked IP was denied while the map is full")
	}

	// recordFailure at capacity must not grow the failure map.
	for i := 0; i < 3; i++ {
		limiter.recordFailure(fmt.Sprintf("203.0.113.%d", i), "admin", now)
	}
	limiter.recordFailure("198.51.100.9", "admin", now)
	if len(limiter.failures) != 3 {
		t.Fatalf("failure map grew past the cap: %d", len(limiter.failures))
	}
}

func TestLoginLimiterConcurrentCounting(t *testing.T) {
	limiter := newLoginLimiter()
	now := limiterEpoch

	var wg sync.WaitGroup
	allowed := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- limiter.allow("203.0.113.1", "admin", now)
		}()
	}
	wg.Wait()
	close(allowed)

	granted := 0
	for ok := range allowed {
		if ok {
			granted++
		}
	}
	if granted != limiter.maxAttempts {
		t.Fatalf("concurrent grants = %d, want exactly %d", granted, limiter.maxAttempts)
	}

	// Concurrent failures and successes across pairs must not race or panic
	// and must block exactly the abused pair.
	var mixed sync.WaitGroup
	for i := 0; i < 20; i++ {
		mixed.Add(2)
		go func() {
			defer mixed.Done()
			limiter.recordFailure("198.51.100.1", "victim", now)
		}()
		go func() {
			defer mixed.Done()
			limiter.recordSuccess("198.51.100.2", "other")
		}()
	}
	mixed.Wait()
	if limiter.allow("198.51.100.1", "victim", now) {
		t.Fatal("pair with 20 concurrent failures was not blocked")
	}
	if !limiter.allow("198.51.100.2", "other", now) {
		t.Fatal("unrelated pair was blocked by concurrent activity")
	}
}

func TestNormalizeLimiterUsernameMatchesLoginSemantics(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{in: "  Admin  ", want: "admin"},
		{in: "ADMIN", want: "admin"},
		{in: "admin", want: "admin"},
	} {
		if got := normalizeLimiterUsername(test.in); got != test.want {
			t.Fatalf("normalize(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
