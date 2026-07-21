package querycoderecovery

import (
	"context"
	"time"

	"pjsk/backend/internal/querycode"
)

type Protector interface {
	RevealEncrypted([]byte) (string, error)
}

type Sender interface {
	SendQueryCodeRecovery(context.Context, string, string, time.Time) error
}

type Service struct {
	store     Store
	protector Protector
	sender    Sender
	manager   *Manager
}

func NewService(store Store, protector Protector, sender Sender, manager *Manager) *Service {
	return &Service{store: store, protector: protector, sender: sender, manager: manager}
}

func (s *Service) Available() bool {
	return s != nil && s.store != nil && s.protector != nil && s.sender != nil && s.manager != nil
}

func (s *Service) Request(ctx context.Context, cn string, ip string) error {
	if !s.Available() {
		return ErrUnavailable
	}
	prepared, err := s.store.PrepareRequest(ctx, cn, ip)
	if err != nil {
		return err
	}
	to, err := s.protector.RevealEncrypted(prepared.EncryptedEmail)
	if err != nil {
		s.markDeliveryFailed(ctx, prepared.ID, prepared.UserID)
		return ErrDeliveryFailed
	}
	if err := s.sender.SendQueryCodeRecovery(ctx, to, prepared.Code, prepared.ExpiresAt); err != nil {
		s.markDeliveryFailed(ctx, prepared.ID, prepared.UserID)
		return ErrDeliveryFailed
	}
	if err := s.store.ConfirmSent(ctx, prepared); err != nil {
		s.markDeliveryFailed(ctx, prepared.ID, prepared.UserID)
		return err
	}
	return nil
}

func (s *Service) Verify(ctx context.Context, cn string, code string) (VerifiedCode, error) {
	if !s.Available() {
		return VerifiedCode{}, ErrUnavailable
	}
	if NormalizeCN(cn) == "" || !ValidCode(code) {
		return VerifiedCode{}, ErrInvalidCode
	}
	return s.store.VerifyCode(ctx, cn, code)
}

// Reset returns the CN of the account on success so the caller can clear
// that account's login block; it returns an empty CN on every failure path.
func (s *Service) Reset(ctx context.Context, token string, newQueryCode string, confirmQueryCode string) (string, error) {
	if !s.Available() {
		return "", ErrUnavailable
	}
	if !ValidToken(token) {
		return "", ErrInvalidToken
	}
	if newQueryCode == "" || newQueryCode != confirmQueryCode || querycode.Validate(newQueryCode) != nil {
		return "", querycode.ErrInvalid
	}
	tokenHash, err := s.manager.HashToken(token)
	if err != nil {
		return "", ErrInvalidToken
	}
	return s.store.ResetQueryCode(ctx, tokenHash, newQueryCode)
}

func (s *Service) markDeliveryFailed(ctx context.Context, codeID string, userID string) {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = s.store.MarkDeliveryFailed(cleanupContext, codeID, userID)
}
