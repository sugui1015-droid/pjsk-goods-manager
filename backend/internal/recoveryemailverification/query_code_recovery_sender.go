package recoveryemailverification

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
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
	s.deliveries = append(s.deliveries, FakeDelivery{To: to, Code: code, ExpiresAt: expiresAt})
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

	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	dialer := &net.Dialer{Timeout: s.config.Timeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("SMTP service unavailable")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else {
		_ = connection.SetDeadline(time.Now().Add(s.config.Timeout))
	}

	tlsConfig := &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}
	var client *smtp.Client
	if s.config.TLSMode == "tls" {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			_ = connection.Close()
			return errors.New("SMTP service unavailable")
		}
		client, err = smtp.NewClient(tlsConnection, s.config.Host)
	} else {
		client, err = smtp.NewClient(connection, s.config.Host)
		if err == nil {
			err = client.StartTLS(tlsConfig)
		}
	}
	if err != nil {
		_ = connection.Close()
		return errors.New("SMTP service unavailable")
	}
	defer client.Close()

	if s.config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return errors.New("SMTP service unavailable")
		}
	}
	if err := client.Mail(s.config.From); err != nil {
		return errors.New("SMTP service unavailable")
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return errors.New("SMTP service unavailable")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP service unavailable")
	}
	buffered := bufio.NewWriter(writer)
	if _, err := buffered.Write(message); err != nil {
		_ = writer.Close()
		return errors.New("SMTP service unavailable")
	}
	if err := buffered.Flush(); err != nil {
		_ = writer.Close()
		return errors.New("SMTP service unavailable")
	}
	if err := writer.Close(); err != nil {
		return errors.New("SMTP service unavailable")
	}
	if err := client.Quit(); err != nil {
		return errors.New("SMTP service unavailable")
	}
	return nil
}

func buildQueryCodeRecoveryMessage(config SMTPConfig, to string, code string, expiresAt time.Time) ([]byte, error) {
	if !ValidCode(code) || expiresAt.IsZero() {
		return nil, errors.New("invalid query code recovery message")
	}
	from := (&mail.Address{Name: config.FromName, Address: config.From}).String()
	subject := mime.QEncoding.Encode("UTF-8", "PJSK 查询码重置验证码")
	body := fmt.Sprintf("您的查询码重置验证码为：%s\r\n\r\n验证码将在 %s 前有效。\r\n此验证码仅用于重置查询码，不会自动登录。\r\n如非本人操作，请忽略此邮件，请勿向他人透露验证码。\r\n", code, expiresAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	message := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + body
	return []byte(message), nil
}
