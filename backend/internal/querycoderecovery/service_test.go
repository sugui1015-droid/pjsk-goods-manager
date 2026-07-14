package querycoderecovery

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type serviceStoreStub struct {
	prepared   PreparedSend
	prepareErr error
	confirmErr error
	verify     VerifiedCode
	verifyErr  error
	resetErr   error
	failedID   string
	resetHash  string
}

func (s *serviceStoreStub) PrepareRequest(context.Context, string, string) (PreparedSend, error) {
	return s.prepared, s.prepareErr
}
func (s *serviceStoreStub) ConfirmSent(context.Context, PreparedSend) error { return s.confirmErr }
func (s *serviceStoreStub) MarkDeliveryFailed(_ context.Context, id string, _ string) error {
	s.failedID = id
	return nil
}
func (s *serviceStoreStub) VerifyCode(context.Context, string, string) (VerifiedCode, error) {
	return s.verify, s.verifyErr
}
func (s *serviceStoreStub) ResetQueryCode(_ context.Context, tokenHash string, _ string) error {
	s.resetHash = tokenHash
	return s.resetErr
}

type protectorStub struct {
	address string
	err     error
}

func (p protectorStub) RevealEncrypted([]byte) (string, error) { return p.address, p.err }

type senderStub struct {
	err   error
	calls int
}

func (s *senderStub) SendQueryCodeRecovery(context.Context, string, string, time.Time) error {
	s.calls++
	return s.err
}

func TestServiceRequestDeliveryBoundary(t *testing.T) {
	manager, err := NewManager(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := &serviceStoreStub{prepared: PreparedSend{ID: "code-id", UserID: "user-id", EncryptedEmail: []byte("encrypted"), Code: "123456", ExpiresAt: time.Now().Add(time.Minute)}}
	sender := &senderStub{}
	service := NewService(store, protectorStub{address: "safe@example.com"}, sender, manager)
	if err := service.Request(context.Background(), "CN", "127.0.0.1"); err != nil || sender.calls != 1 || store.failedID != "" {
		t.Fatal("successful delivery did not complete its two-step boundary")
	}

	store = &serviceStoreStub{prepared: store.prepared}
	sender = &senderStub{err: errors.New("delivery unavailable")}
	service = NewService(store, protectorStub{address: "safe@example.com"}, sender, manager)
	if err := service.Request(context.Background(), "CN", "127.0.0.1"); !errors.Is(err, ErrDeliveryFailed) || store.failedID == "" {
		t.Fatal("failed delivery was not safely invalidated")
	}
}

func TestServiceVerifyAndResetTokenHashing(t *testing.T) {
	manager, err := NewManager(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	store := &serviceStoreStub{verify: VerifiedCode{ResetToken: token, ExpiresAt: time.Now().Add(time.Minute)}}
	service := NewService(store, protectorStub{address: "safe@example.com"}, &senderStub{}, manager)
	if _, err := service.Verify(context.Background(), "CN", "123456"); err != nil {
		t.Fatal("valid verification was rejected")
	}
	if err := service.Reset(context.Background(), token, "NewCode-123", "NewCode-123"); err != nil || store.resetHash == "" || store.resetHash == token {
		t.Fatal("reset token was not converted to a keyed storage hash")
	}
	if err := service.Reset(context.Background(), token, "NewCode-123", "OtherCode-123"); !errors.Is(err, ErrInvalidCode) && err == nil {
		t.Fatal("mismatched query-code confirmation was accepted")
	}
}
