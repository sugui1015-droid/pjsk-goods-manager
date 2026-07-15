package query

import (
	"sync"
	"time"
)

// defaultMaxTrackedKeys bounds each of the limiter's in-memory maps so a
// long-running process cannot accumulate keys without limit under a spray of
// distinct IPs or CNs. It is generous enough for normal internal-LAN use.
const defaultMaxTrackedKeys = 10000

// loginLimiter throttles query-code login attempts so repeated guesses
// cannot hammer the database or brute-force a query code.
//
// Two layers:
//   - per client IP: at most maxAttempts login calls per attemptWindow,
//     regardless of outcome;
//   - per IP+CN: after maxFailures failed logins within failureWindow the
//     pair is blocked for blockDuration; a successful login clears it.
//
// Keys are tracked in memory only, which is acceptable for a single-process
// deployment. Entries expire lazily on access. Each map is additionally
// bounded by maxTrackedKeys: at capacity the limiter first reclaims expired
// keys, then evicts the least-recently-seen key that is NOT currently
// blocked. A currently-blocked failure key is never evicted for capacity, so
// an active attacker cannot be released early. If every failure key is
// actively blocked (a pathological, saturated state), a new failure key is
// simply not created — the per-IP attempt limit still applies, so this is a
// bounded, safe degradation rather than dropping rate limiting entirely.
type loginLimiter struct {
	mu sync.Mutex

	attemptWindow time.Duration
	maxAttempts   int
	attempts      map[string]*windowCounter

	failureWindow time.Duration
	maxFailures   int
	blockDuration time.Duration
	failures      map[string]*failureState

	maxTrackedKeys int
}

type windowCounter struct {
	windowStart time.Time
	count       int
	lastSeenAt  time.Time
}

type failureState struct {
	windowStart  time.Time
	count        int
	blockedUntil time.Time
	lastSeenAt   time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		attemptWindow:  time.Minute,
		maxAttempts:    20,
		attempts:       map[string]*windowCounter{},
		failureWindow:  10 * time.Minute,
		maxFailures:    5,
		blockDuration:  10 * time.Minute,
		failures:       map[string]*failureState{},
		maxTrackedKeys: defaultMaxTrackedKeys,
	}
}

// effectiveMaxKeys treats a non-positive configured cap as "use the default"
// so a misconfiguration can never mean "unbounded".
func (l *loginLimiter) effectiveMaxKeys() int {
	if l.maxTrackedKeys > 0 {
		return l.maxTrackedKeys
	}
	return defaultMaxTrackedKeys
}

// allow reports whether this login attempt may proceed. It must be called
// once per login request, before any database work.
func (l *loginLimiter) allow(ip string, cn string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)

	counter := l.attempts[ip]
	if counter == nil {
		if !l.ensureAttemptsCapacityLocked(now) {
			// Cannot make room without evicting an active key: fail closed.
			return false
		}
		counter = &windowCounter{windowStart: now, lastSeenAt: now}
		l.attempts[ip] = counter
	} else if now.Sub(counter.windowStart) >= l.attemptWindow {
		counter.windowStart = now
		counter.count = 0
	}
	counter.lastSeenAt = now
	if counter.count >= l.maxAttempts {
		return false
	}
	counter.count++

	state := l.failures[failureKey(ip, cn)]
	if state != nil {
		state.lastSeenAt = now
		if now.Before(state.blockedUntil) {
			return false
		}
	}
	return true
}

// recordFailure notes a failed login for the IP+CN pair and blocks the pair
// once it accumulates too many failures inside the window.
func (l *loginLimiter) recordFailure(ip string, cn string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := failureKey(ip, cn)
	state := l.failures[key]
	if state == nil {
		if !l.ensureFailuresCapacityLocked(now) {
			// Saturated with actively-blocked keys: do not create a new key.
			// The per-IP attempt limit still throttles this source.
			return
		}
		state = &failureState{windowStart: now, lastSeenAt: now}
		l.failures[key] = state
	} else if now.Sub(state.windowStart) >= l.failureWindow {
		state.windowStart = now
		state.count = 0
	}
	state.lastSeenAt = now
	state.count++
	if state.count >= l.maxFailures {
		state.blockedUntil = now.Add(l.blockDuration)
		state.count = 0
		state.windowStart = now
	}
}

// recordSuccess clears failure tracking for the IP+CN pair.
func (l *loginLimiter) recordSuccess(ip string, cn string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, failureKey(ip, cn))
}

func (l *loginLimiter) cleanupLocked(now time.Time) {
	for key, counter := range l.attempts {
		if now.Sub(counter.windowStart) >= l.attemptWindow {
			delete(l.attempts, key)
		}
	}
	for key, state := range l.failures {
		if now.Sub(state.windowStart) >= l.failureWindow && now.After(state.blockedUntil) {
			delete(l.failures, key)
		}
	}
}

// ensureAttemptsCapacityLocked makes room for one new attempts key. Attempts
// keys are pure per-IP rate counters with no "blocked" state, so the
// least-recently-seen one can always be evicted. Returns false only if the
// map is at capacity and somehow empty (never in practice).
func (l *loginLimiter) ensureAttemptsCapacityLocked(now time.Time) bool {
	max := l.effectiveMaxKeys()
	if len(l.attempts) < max {
		return true
	}
	l.cleanupLocked(now)
	if len(l.attempts) < max {
		return true
	}
	var oldestKey string
	var oldest time.Time
	found := false
	for key, counter := range l.attempts {
		if !found || counter.lastSeenAt.Before(oldest) {
			oldest = counter.lastSeenAt
			oldestKey = key
			found = true
		}
	}
	if !found {
		return false
	}
	delete(l.attempts, oldestKey)
	return true
}

// ensureFailuresCapacityLocked makes room for one new failure key. It first
// reclaims expired keys, then evicts the least-recently-seen key that is NOT
// currently blocked. A blocked key is never evicted for capacity. Returns
// false when every key is actively blocked (caller must not create a new key).
func (l *loginLimiter) ensureFailuresCapacityLocked(now time.Time) bool {
	max := l.effectiveMaxKeys()
	if len(l.failures) < max {
		return true
	}
	l.cleanupLocked(now)
	if len(l.failures) < max {
		return true
	}
	var oldestKey string
	var oldest time.Time
	found := false
	for key, state := range l.failures {
		if now.Before(state.blockedUntil) {
			continue // never evict an actively-blocked key
		}
		if !found || state.lastSeenAt.Before(oldest) {
			oldest = state.lastSeenAt
			oldestKey = key
			found = true
		}
	}
	if !found {
		return false // all keys are actively blocked
	}
	delete(l.failures, oldestKey)
	return true
}

func failureKey(ip string, cn string) string {
	return ip + "|" + cn
}
