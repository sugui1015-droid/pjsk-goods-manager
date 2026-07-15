package admin

import (
	"strings"
	"sync"
	"time"
)

// loginLimiter throttles admin login attempts so repeated guesses cannot
// brute-force an administrator password. It mirrors the query package's
// limiter semantics:
//
//   - per client IP: at most maxAttempts login calls per attemptWindow,
//     regardless of outcome;
//   - per IP+username: after maxFailures failed logins within failureWindow
//     the pair is blocked for blockDuration; a successful login clears it.
//
// State is in-memory only — acceptable for a single-process deployment; a
// restart resets it, and multiple instances would each count independently.
// Unlike the query limiter it also caps the number of tracked keys: when a
// map is full and the incoming key is unknown, the attempt is denied
// (fail-closed) instead of growing without bound under distributed IP spray.
type loginLimiter struct {
	mu sync.Mutex

	attemptWindow time.Duration
	maxAttempts   int
	attempts      map[string]*windowCounter

	failureWindow time.Duration
	maxFailures   int
	blockDuration time.Duration
	failures      map[string]*failureState

	rateLimitAuditWindow time.Duration
	rateLimitAudits      map[string]time.Time

	maxTrackedKeys int
}

type windowCounter struct {
	windowStart time.Time
	count       int
}

type failureState struct {
	windowStart  time.Time
	count        int
	blockedUntil time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		attemptWindow:        time.Minute,
		maxAttempts:          20,
		attempts:             map[string]*windowCounter{},
		failureWindow:        10 * time.Minute,
		maxFailures:          5,
		blockDuration:        10 * time.Minute,
		failures:             map[string]*failureState{},
		rateLimitAuditWindow: time.Minute,
		rateLimitAudits:      map[string]time.Time{},
		maxTrackedKeys:       10000,
	}
}

// normalizeLimiterUsername mirrors the login lookup semantics
// (lower(btrim(username))) so the rate-limit key and the account matched by
// the database are always the same identity.
func normalizeLimiterUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// allow reports whether this login attempt may proceed. It must be called
// once per login request, before any database work.
func (l *loginLimiter) allow(ip string, username string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)

	counter := l.attempts[ip]
	if counter == nil {
		if len(l.attempts) >= l.maxTrackedKeys {
			return false
		}
		counter = &windowCounter{windowStart: now}
		l.attempts[ip] = counter
	} else if now.Sub(counter.windowStart) >= l.attemptWindow {
		counter.windowStart = now
		counter.count = 0
	}
	if counter.count >= l.maxAttempts {
		return false
	}
	counter.count++

	state := l.failures[failureKey(ip, username)]
	if state != nil && now.Before(state.blockedUntil) {
		return false
	}
	return true
}

// recordFailure notes a failed login for the IP+username pair and blocks the
// pair once it accumulates too many failures inside the window. When the
// failure map is at capacity, new pairs are not tracked — allow() already
// fail-closes on a full attempts map, so this cannot be used to bypass
// limits, it only avoids unbounded growth.
func (l *loginLimiter) recordFailure(ip string, username string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := failureKey(ip, username)
	state := l.failures[key]
	if state == nil {
		if len(l.failures) >= l.maxTrackedKeys {
			return
		}
		state = &failureState{windowStart: now}
		l.failures[key] = state
	} else if now.Sub(state.windowStart) >= l.failureWindow {
		state.windowStart = now
		state.count = 0
	}
	state.count++
	if state.count >= l.maxFailures {
		state.blockedUntil = now.Add(l.blockDuration)
		state.count = 0
		state.windowStart = now
	}
}

// recordSuccess clears failure tracking for the IP+username pair.
func (l *loginLimiter) recordSuccess(ip string, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := failureKey(ip, username)
	delete(l.failures, key)
	delete(l.rateLimitAudits, key)
}

// shouldAuditRateLimited returns true at most once per IP+username pair per
// audit window. The limiter can reject many requests while a block is active;
// without this separate gate, a tight retry loop would create noisy audit rows
// without adding investigative value.
func (l *loginLimiter) shouldAuditRateLimited(ip string, username string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)

	key := failureKey(ip, username)
	if last, ok := l.rateLimitAudits[key]; ok && now.Sub(last) < l.rateLimitAuditWindow {
		return false
	}
	if _, ok := l.rateLimitAudits[key]; !ok && len(l.rateLimitAudits) >= l.maxTrackedKeys {
		return false
	}
	l.rateLimitAudits[key] = now
	return true
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
	for key, last := range l.rateLimitAudits {
		if now.Sub(last) >= l.rateLimitAuditWindow {
			delete(l.rateLimitAudits, key)
		}
	}
}

func failureKey(ip string, username string) string {
	return ip + "|" + username
}
