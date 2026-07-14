package recoveryemailverification

import (
	"context"
	"errors"
	"sync"
	"time"
)

type FakeDelivery struct {
	To        string
	Code      string
	ExpiresAt time.Time
}

// FakeSender records deliveries in memory for tests. It is deliberately not
// an empty success stub: callers can assert the recipient, code and expiry.
type FakeSender struct {
	mu         sync.Mutex
	deliveries []FakeDelivery
	err        error
}

func NewFakeSender() *FakeSender {
	return &FakeSender{}
}

func (s *FakeSender) SendRecoveryVerification(ctx context.Context, to string, code string, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if to == "" || !ValidCode(code) || expiresAt.IsZero() {
		return errors.New("invalid fake recovery email delivery")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.deliveries = append(s.deliveries, FakeDelivery{To: to, Code: code, ExpiresAt: expiresAt})
	return nil
}

func (s *FakeSender) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *FakeSender) Deliveries() []FakeDelivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]FakeDelivery(nil), s.deliveries...)
}
