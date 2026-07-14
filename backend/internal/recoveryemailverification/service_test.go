package recoveryemailverification

import (
	"context"
	"errors"
	"testing"
	"time"
)

type serviceStore struct {
	prepared   PreparedSend
	verified   VerifiedState
	prepareErr error
	confirmErr error
	verifyErr  error
	failed     int
}

func (s *serviceStore) PrepareSend(context.Context, string) (PreparedSend, error) {
	return s.prepared, s.prepareErr
}

func (s *serviceStore) ConfirmSent(context.Context, PreparedSend, string) error {
	return s.confirmErr
}

func (s *serviceStore) MarkDeliveryFailed(context.Context, string, string) error {
	s.failed++
	return nil
}

func (s *serviceStore) Verify(context.Context, string, string) (VerifiedState, error) {
	return s.verified, s.verifyErr
}

type serviceProtector struct {
	email   string
	masked  string
	revealE error
	maskE   error
}

func (p serviceProtector) RevealEncrypted([]byte) (string, error) { return p.email, p.revealE }
func (p serviceProtector) MaskEncrypted([]byte) (string, error)   { return p.masked, p.maskE }

func TestServiceSendSuccess(t *testing.T) {
	policy := DefaultPolicy()
	expires := time.Now().Add(policy.TTL)
	store := &serviceStore{prepared: PreparedSend{
		ID: "code-id", UserID: "user-id", EncryptedEmail: []byte("cipher"),
		Code: "123456", ExpiresAt: expires,
	}}
	sender := NewFakeSender()
	service := NewService(store, serviceProtector{email: "verify@example.com", masked: "v***@example.com"}, sender, policy)

	result, err := service.Send(context.Background(), "user-id")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.MaskedEmail != "v***@example.com" || result.ExpiresAt != expires || result.RetryAfterSeconds != 60 {
		t.Fatalf("Send() result = %+v", result)
	}
	if len(sender.Deliveries()) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(sender.Deliveries()))
	}
	if store.failed != 0 {
		t.Fatalf("delivery failure marks = %d, want 0", store.failed)
	}
}

func TestServiceSendFailureInvalidatesPreparedCode(t *testing.T) {
	cases := []struct {
		name      string
		protector serviceProtector
		senderErr error
		confirm   error
		want      error
	}{
		{name: "decrypt", protector: serviceProtector{revealE: errors.New("decrypt")}, want: ErrUnavailable},
		{name: "mask", protector: serviceProtector{email: "verify@example.com", maskE: errors.New("mask")}, want: ErrUnavailable},
		{name: "delivery", protector: serviceProtector{email: "verify@example.com", masked: "v***@example.com"}, senderErr: errors.New("delivery"), want: ErrDeliveryFailed},
		{name: "confirm", protector: serviceProtector{email: "verify@example.com", masked: "v***@example.com"}, confirm: errors.New("confirm"), want: errors.New("confirm")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &serviceStore{prepared: PreparedSend{ID: "code-id", UserID: "user-id", Code: "123456", ExpiresAt: time.Now().Add(time.Minute)}, confirmErr: tc.confirm}
			sender := NewFakeSender()
			sender.SetError(tc.senderErr)
			service := NewService(store, tc.protector, sender, DefaultPolicy())
			_, err := service.Send(context.Background(), "user-id")
			if tc.name == "confirm" {
				if err == nil || err.Error() != tc.want.Error() {
					t.Fatalf("Send() error = %v, want %v", err, tc.want)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("Send() error = %v, want %v", err, tc.want)
			}
			if store.failed != 1 {
				t.Fatalf("delivery failure marks = %d, want 1", store.failed)
			}
		})
	}
}

func TestServiceVerifyValidationAndMasking(t *testing.T) {
	store := &serviceStore{verified: VerifiedState{EncryptedEmail: []byte("cipher"), VerifiedAt: time.Now()}}
	service := NewService(store, serviceProtector{masked: "v***@example.com"}, NewFakeSender(), DefaultPolicy())
	if _, err := service.Verify(context.Background(), "user-id", "bad"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("invalid code error = %v", err)
	}
	result, err := service.Verify(context.Background(), "user-id", "123456")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.MaskedEmail != "v***@example.com" || result.VerifiedAt.IsZero() {
		t.Fatalf("Verify() result = %+v", result)
	}
}
