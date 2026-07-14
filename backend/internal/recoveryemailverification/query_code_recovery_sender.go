package recoveryemailverification

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"time"
)

// SendQueryCodeRecovery is intentionally separate from
// SendRecoveryVerification so callers cannot accidentally use the signed-in
// email-verification purpose for an anonymous query-code reset.
func (s *FakeSender) SendQueryCodeRecovery(ctx context.Context, to string, code string, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if to == "" || !ValidCode(code) || expiresAt.IsZero() {
		return errors.New("invalid fake query code recovery delivery")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.deliveries = append(s.deliveries, FakeDelivery{Purpose: QueryCodeRecoveryPurpose, To: to, Code: code, ExpiresAt: expiresAt})
	return nil
}

func (s *SMTPSender) SendQueryCodeRecovery(ctx context.Context, to string, code string, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil || recipient.Address != to || !ValidCode(code) {
		return errors.New("invalid query code recovery delivery")
	}
	message, err := buildQueryCodeRecoveryMessage(s.config, recipient.Address, code, expiresAt)
	if err != nil {
		return err
	}
	return s.sendMessage(ctx, recipient.Address, message)
}

func buildQueryCodeRecoveryMessage(config SMTPConfig, to string, code string, expiresAt time.Time) ([]byte, error) {
	if !ValidCode(code) || expiresAt.IsZero() {
		return nil, errors.New("invalid query code recovery message")
	}
	from := (&mail.Address{Name: config.FromName, Address: config.From}).String()
	subject := mime.QEncoding.Encode("UTF-8", "PJSK 查询码重置验证码")
	body := fmt.Sprintf("您的查询码重置验证码为：%s\r\n\r\n验证码将在 %s 前有效。\r\n此验证码仅用于设置新查询码，不需要提供旧查询码，也不会自动登录。\r\n如非本人操作，请忽略此邮件，请勿向他人透露验证码。\r\n", code, expiresAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	message := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + body
	return []byte(message), nil
}
