package recoveryemailverification

import (
	"context"
	"errors"
	"time"
)

type Protector interface {
	RevealEncrypted([]byte) (string, error)
	MaskEncrypted([]byte) (string, error)
}

type Service struct {
	store     Store
	protector Protector
	sender    Sender
	policy    Policy
}

type SendResult struct {
	MaskedEmail       string
	ExpiresAt         time.Time
	RetryAfterSeconds int
}

type VerifyResult struct {
	MaskedEmail string
	VerifiedAt  time.Time
}

func NewService(store Store, protector Protector, sender Sender, policy Policy) *Service {
	return &Service{store: store, protector: protector, sender: sender, policy: policy}
}

func (s *Service) Available() bool {
	return s != nil && s.store != nil && s.protector != nil && s.sender != nil
}

func (s *Service) Send(ctx context.Context, userID string) (SendResult, error) {
	if !s.Available() {
		return SendResult{}, ErrUnavailable
	}
	prepared, err := s.store.PrepareSend(ctx, userID)
	if err != nil {
		return SendResult{}, err
	}
	to, err := s.protector.RevealEncrypted(prepared.EncryptedEmail)
	if err != nil {
		s.markDeliveryFailed(ctx, prepared.ID, prepared.UserID)
		return SendResult{}, ErrUnavailable
	}
	masked, err := s.protector.MaskEncrypted(prepared.EncryptedEmail)
	if err != nil {
		s.markDeliveryFailed(ctx, prepared.ID, prepared.UserID)
		return SendResult{}, ErrUnavailable
	}
	if err := s.sender.SendRecoveryVerification(ctx, to, prepared.Code, prepared.ExpiresAt); err != nil {
		s.markDeliveryFailed(ctx, prepared.ID, prepared.UserID)
		return SendResult{}, ErrDeliveryFailed
	}
	if err := s.store.ConfirmSent(ctx, prepared, masked); err != nil {
		s.markDeliveryFailed(ctx, prepared.ID, prepared.UserID)
		return SendResult{}, err
	}
	return SendResult{
		MaskedEmail:       masked,
		ExpiresAt:         prepared.ExpiresAt,
		RetryAfterSeconds: int(s.policy.Cooldown.Seconds()),
	}, nil
}

func (s *Service) markDeliveryFailed(ctx context.Context, codeID string, userID string) {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = s.store.MarkDeliveryFailed(cleanupContext, codeID, userID)
}
func (s *Service) Verify(ctx context.Context, userID string, code string) (VerifyResult, error) {
	if s == nil || s.store == nil || s.protector == nil {
		return VerifyResult{}, ErrUnavailable
	}
	if !ValidCode(code) {
		return VerifyResult{}, ErrInvalidCode
	}
	state, err := s.store.Verify(ctx, userID, code)
	if err != nil {
		return VerifyResult{}, err
	}
	masked, err := s.protector.MaskEncrypted(state.EncryptedEmail)
	if err != nil {
		return VerifyResult{}, ErrUnavailable
	}
	return VerifyResult{MaskedEmail: masked, VerifiedAt: state.VerifiedAt}, nil
}

func IsStateConflict(err error) bool {
	return errors.Is(err, ErrNoRecoveryEmail) || errors.Is(err, ErrEmailDisabled) ||
		errors.Is(err, ErrAlreadyVerified) || errors.Is(err, ErrNoActiveCode) ||
		errors.Is(err, ErrCodeExpired) || errors.Is(err, ErrCodeUsed) ||
		errors.Is(err, ErrCodeInvalidated)
}
