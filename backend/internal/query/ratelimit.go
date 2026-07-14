package query

import (
	"sync"
	"time"
)

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
// deployment. Entries expire lazily on access.
type loginLimiter struct {
	mu sync.Mutex

	attemptWindow time.Duration
	maxAttempts   int
	attempts      map[string]*windowCounter

	failureWindow time.Duration
	maxFailures   int
	blockDuration time.Duration
	failures      map[string]*failureState
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
		attemptWindow: time.Minute,
		maxAttempts:   20,
		attempts:      map[string]*windowCounter{},
		failureWindow: 10 * time.Minute,
		maxFailures:   5,
		blockDuration: 10 * time.Minute,
		failures:      map[string]*failureState{},
	}
}

// allow reports whether this login attempt may proceed. It must be called
// once per login request, before any database work.
func (l *loginLimiter) allow(ip string, cn string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now)

	counter := l.attempts[ip]
	if counter == nil || now.Sub(counter.windowStart) >= l.attemptWindow {
		counter = &windowCounter{windowStart: now}
		l.attempts[ip] = counter
	}
	if counter.count >= l.maxAttempts {
		return false
	}
	counter.count++

	state := l.failures[failureKey(ip, cn)]
	if state != nil && now.Before(state.blockedUntil) {
		return false
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
	if state == nil || now.Sub(state.windowStart) >= l.failureWindow {
		state = &failureState{windowStart: now}
		l.failures[key] = state
	}
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

func failureKey(ip string, cn string) string {
	return ip + "|" + cn
}
