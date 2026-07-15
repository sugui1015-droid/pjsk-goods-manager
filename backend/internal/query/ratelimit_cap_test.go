package query

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

var limiterEpoch = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

// helper: drive a pair to the blocked state at time `now`.
func blockPair(l *loginLimiter, ip, cn string, now time.Time) {
	for i := 0; i < l.maxFailures; i++ {
		l.allow(ip, cn, now)
		l.recordFailure(ip, cn, now)
	}
}

func TestLimiterExpiredFailureKeysAreCleaned(t *testing.T) {
	l := newLoginLimiter()
	now := limiterEpoch

	l.allow("1.1.1.1", "cn1", now)
	l.recordFailure("1.1.1.1", "cn1", now) // one failure, not blocked
	if len(l.failures) != 1 {
		t.Fatalf("expected 1 failure key, got %d", len(l.failures))
	}
	// After failureWindow with no block, a later access cleans it.
	later := now.Add(l.failureWindow + time.Minute)
	l.allow("2.2.2.2", "cn2", later)
	if _, ok := l.failures["1.1.1.1|cn1"]; ok {
		t.Fatal("expired unblocked failure key should have been cleaned")
	}
}

func TestLimiterBlockedKeyNotClearedByTTLBeforeBlockEnds(t *testing.T) {
	l := newLoginLimiter()
	now := limiterEpoch
	blockPair(l, "1.1.1.1", "cn1", now)

	// Halfway through the block, a cleanup pass must keep the blocked key.
	mid := now.Add(l.blockDuration / 2)
	l.allow("9.9.9.9", "other", mid)
	if _, ok := l.failures["1.1.1.1|cn1"]; !ok {
		t.Fatal("blocked key must not be cleaned before the block ends")
	}
	if l.allow("1.1.1.1", "cn1", mid) {
		t.Fatal("pair must still be blocked mid-block")
	}
}

func TestLimiterFailuresKeyCapEvictsUnblockedLRU(t *testing.T) {
	l := newLoginLimiter()
	l.maxTrackedKeys = 3
	base := limiterEpoch

	// Three unblocked failure keys, each seen at a distinct time.
	l.allow("10.0.0.1", "a", base)
	l.recordFailure("10.0.0.1", "a", base) // oldest lastSeen
	l.allow("10.0.0.2", "b", base.Add(time.Second))
	l.recordFailure("10.0.0.2", "b", base.Add(time.Second))
	l.allow("10.0.0.3", "c", base.Add(2*time.Second))
	l.recordFailure("10.0.0.3", "c", base.Add(2*time.Second))
	if len(l.failures) != 3 {
		t.Fatalf("setup: expected 3 failure keys, got %d", len(l.failures))
	}

	// A fourth new failure key at cap must evict the least-recently-seen
	// unblocked key ("a"), not exceed the cap.
	l.recordFailure("10.0.0.4", "d", base.Add(3*time.Second))
	if len(l.failures) > 3 {
		t.Fatalf("failure map exceeded the cap: %d", len(l.failures))
	}
	if _, ok := l.failures["10.0.0.1|a"]; ok {
		t.Fatal("least-recently-seen unblocked key should have been evicted")
	}
	if _, ok := l.failures["10.0.0.4|d"]; !ok {
		t.Fatal("the new key should have been inserted after eviction")
	}
}

func TestLimiterCapacityNeverEvictsBlockedKey(t *testing.T) {
	l := newLoginLimiter()
	l.maxTrackedKeys = 2
	base := limiterEpoch

	// Two blocked keys fill the map.
	blockPair(l, "10.0.0.1", "a", base)
	blockPair(l, "10.0.0.2", "b", base.Add(time.Second))
	if len(l.failures) != 2 {
		t.Fatalf("setup: expected 2 blocked keys, got %d", len(l.failures))
	}

	// A new failure while both keys are actively blocked must NOT evict a
	// blocked key and must NOT exceed the cap: the new key is simply not
	// created (safe degradation). Both blocks remain in force.
	l.recordFailure("10.0.0.3", "c", base.Add(2*time.Second))
	if len(l.failures) != 2 {
		t.Fatalf("blocked keys must not be evicted; map size = %d", len(l.failures))
	}
	if l.allow("10.0.0.1", "a", base.Add(2*time.Second)) {
		t.Fatal("first blocked pair must remain blocked")
	}
	if l.allow("10.0.0.2", "b", base.Add(2*time.Second)) {
		t.Fatal("second blocked pair must remain blocked")
	}
}

func TestLimiterAcceptsNewKeysAfterBlocksExpire(t *testing.T) {
	l := newLoginLimiter()
	l.maxTrackedKeys = 2
	base := limiterEpoch
	blockPair(l, "10.0.0.1", "a", base)
	blockPair(l, "10.0.0.2", "b", base)

	// After the blocks expire, capacity frees up and new keys are accepted.
	after := base.Add(l.blockDuration + l.failureWindow + time.Minute)
	l.recordFailure("10.0.0.3", "c", after)
	if _, ok := l.failures["10.0.0.3|c"]; !ok {
		t.Fatal("a new key should be accepted once old blocks have expired")
	}
	if len(l.failures) > 2 {
		t.Fatalf("map exceeded cap after expiry: %d", len(l.failures))
	}
}

func TestLimiterAttemptsKeyCapIsBounded(t *testing.T) {
	l := newLoginLimiter()
	l.maxTrackedKeys = 5
	now := limiterEpoch

	for i := 0; i < 50; i++ {
		l.allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256), "cn", now.Add(time.Duration(i)*time.Millisecond))
	}
	if len(l.attempts) > 5 {
		t.Fatalf("attempts map exceeded the cap: %d", len(l.attempts))
	}
}

func TestLimiterSmallCapValues(t *testing.T) {
	for _, cap := range []int{1, 2} {
		l := newLoginLimiter()
		l.maxTrackedKeys = cap
		now := limiterEpoch
		for i := 0; i < 10; i++ {
			ip := fmt.Sprintf("10.1.1.%d", i)
			l.allow(ip, "cn", now.Add(time.Duration(i)*time.Second))
			l.recordFailure(ip, "cn", now.Add(time.Duration(i)*time.Second))
		}
		if len(l.failures) > cap {
			t.Fatalf("cap=%d: failures map size %d exceeds cap", cap, len(l.failures))
		}
		if len(l.attempts) > cap {
			t.Fatalf("cap=%d: attempts map size %d exceeds cap", cap, len(l.attempts))
		}
	}
}

func TestLimiterZeroOrNegativeCapFallsBackToDefault(t *testing.T) {
	for _, cap := range []int{0, -5} {
		l := newLoginLimiter()
		l.maxTrackedKeys = cap
		if l.effectiveMaxKeys() != defaultMaxTrackedKeys {
			t.Fatalf("cap=%d should fall back to default %d, got %d", cap, defaultMaxTrackedKeys, l.effectiveMaxKeys())
		}
		// And it must still be bounded (not unbounded) — write a few keys.
		now := limiterEpoch
		for i := 0; i < 10; i++ {
			l.allow(fmt.Sprintf("10.2.2.%d", i), "cn", now)
		}
		if len(l.attempts) > defaultMaxTrackedKeys {
			t.Fatalf("cap=%d produced an unbounded map", cap)
		}
	}
}

func TestLimiterManyDistinctKeysStayBounded(t *testing.T) {
	l := newLoginLimiter()
	l.maxTrackedKeys = 100
	base := limiterEpoch
	for i := 0; i < 5000; i++ {
		ip := fmt.Sprintf("172.16.%d.%d", (i/256)%256, i%256)
		ts := base.Add(time.Duration(i) * time.Millisecond)
		l.allow(ip, fmt.Sprintf("cn-%d", i), ts)
		l.recordFailure(ip, fmt.Sprintf("cn-%d", i), ts)
	}
	if len(l.failures) > 100 {
		t.Fatalf("failures map grew past the cap: %d", len(l.failures))
	}
	if len(l.attempts) > 100 {
		t.Fatalf("attempts map grew past the cap: %d", len(l.attempts))
	}
}

func TestLimiterLastSeenUpdatesOnAccess(t *testing.T) {
	l := newLoginLimiter()
	now := limiterEpoch
	l.allow("1.1.1.1", "cn1", now)
	l.recordFailure("1.1.1.1", "cn1", now)
	first := l.failures["1.1.1.1|cn1"].lastSeenAt

	later := now.Add(time.Minute)
	l.allow("1.1.1.1", "cn1", later) // access updates lastSeenAt
	updated := l.failures["1.1.1.1|cn1"].lastSeenAt
	if !updated.After(first) {
		t.Fatalf("lastSeenAt should advance on access: first=%v updated=%v", first, updated)
	}
}

func TestLimiterCleanupEmptyMapNoPanic(t *testing.T) {
	l := newLoginLimiter()
	l.cleanupLocked(limiterEpoch)
	l.ensureAttemptsCapacityLocked(limiterEpoch)
	l.ensureFailuresCapacityLocked(limiterEpoch)
	if len(l.attempts) != 0 || len(l.failures) != 0 {
		t.Fatal("operations on an empty limiter should not create keys")
	}
}

func TestLimiterConcurrentSameAndDifferentKeys(t *testing.T) {
	l := newLoginLimiter()
	l.maxTrackedKeys = 200
	now := limiterEpoch

	var wg sync.WaitGroup
	// Same key hammered by many goroutines.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.allow("1.1.1.1", "hot", now)
			l.recordFailure("1.1.1.1", "hot", now)
		}()
	}
	// Distinct keys plus concurrent successes and cleanups.
	for i := 0; i < 300; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := fmt.Sprintf("10.9.%d.%d", (n/256)%256, n%256)
			l.allow(ip, "cn", now.Add(time.Duration(n)*time.Millisecond))
			l.recordFailure(ip, "cn", now.Add(time.Duration(n)*time.Millisecond))
			if n%3 == 0 {
				l.recordSuccess(ip, "cn")
			}
		}(i)
	}
	wg.Wait()

	l.mu.Lock()
	fa := len(l.failures)
	at := len(l.attempts)
	l.mu.Unlock()
	if fa > 200 {
		t.Fatalf("failures map exceeded cap under concurrency: %d", fa)
	}
	if at > 200 {
		t.Fatalf("attempts map exceeded cap under concurrency: %d", at)
	}
	// The hot pair should be blocked after >= maxFailures failures.
	if l.allow("1.1.1.1", "hot", now) {
		t.Fatal("hot pair should be blocked after concurrent failures")
	}
}

func TestLimiterBlockedUntilAfterExpiryOrdering(t *testing.T) {
	l := newLoginLimiter()
	now := limiterEpoch
	blockPair(l, "1.1.1.1", "cn1", now)
	st := l.failures["1.1.1.1|cn1"]
	// The key is expirable only once now is past blockedUntil; the cleanup
	// condition (windowStart+failureWindow) must not fire before the block ends.
	if !st.blockedUntil.After(now) {
		t.Fatal("blockedUntil should be in the future right after blocking")
	}
	justBeforeUnblock := st.blockedUntil.Add(-time.Second)
	l.allow("2.2.2.2", "cn2", justBeforeUnblock)
	if _, ok := l.failures["1.1.1.1|cn1"]; !ok {
		t.Fatal("key must survive cleanup until the block actually ends")
	}
}
