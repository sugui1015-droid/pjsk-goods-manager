package recoveryemailverification

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFakeSenderRecordsDeliveryAndFailure(t *testing.T) {
	sender := NewFakeSender()
	expiresAt := time.Now().Add(DefaultTTL)
	if err := sender.SendRecoveryVerification(context.Background(), "user@example.com", "123456", expiresAt); err != nil {
		t.Fatal(err)
	}
	deliveries := sender.Deliveries()
	if len(deliveries) != 1 || deliveries[0].Purpose != RecoveryEmailVerificationPurpose || deliveries[0].To != "user@example.com" || deliveries[0].Code != "123456" || !deliveries[0].ExpiresAt.Equal(expiresAt) {
		t.Fatal("fake sender did not record the purpose-specific delivery")
	}
	sender.SetError(errors.New("fake failure"))
	if err := sender.SendRecoveryVerification(context.Background(), "user@example.com", "654321", expiresAt); err == nil {
		t.Fatal("fake sender ignored configured failure")
	}
	if len(sender.Deliveries()) != 1 {
		t.Fatal("failed fake delivery was recorded")
	}
}

func TestSMTPSenderConfigurationAndChineseMessage(t *testing.T) {
	valid := SMTPConfig{Host: "smtp.example.com", Port: 587, From: "no-reply@example.com", FromName: "PJSK", TLSMode: "starttls"}
	if _, err := NewSMTPSender(valid); err != nil {
		t.Fatalf("valid SMTP config: %v", err)
	}
	invalid := []SMTPConfig{
		{},
		{Host: "smtp.example.com", Port: 0, From: "no-reply@example.com", TLSMode: "starttls"},
		{Host: "smtp.example.com", Port: 587, From: "bad address", TLSMode: "starttls"},
		{Host: "smtp.example.com", Port: 587, From: "no-reply@example.com", TLSMode: "plain"},
		{Host: "smtp.example.com", Port: 587, From: "no-reply@example.com", TLSMode: "tls", Username: "user"},
		{Host: "smtp.example.com", Port: 587, From: "no-reply@example.com", TLSMode: "tls", Username: "bad\r\nuser", Password: "unused"},
		{Host: "smtp.example.com", Port: 587, From: "no-reply@example.com", TLSMode: "tls", FromName: "bad\r\nname"},
		{Host: "smtp.example.com", Port: 587, From: "no-reply@example.com", TLSMode: "tls", FromName: strings.Repeat("界", 43)},
	}
	for _, config := range invalid {
		if _, err := NewSMTPSender(config); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("invalid SMTP config error = %v", err)
		}
	}

	tlsConfig := smtpTLSConfig("smtp.example.com")
	if tlsConfig.ServerName != "smtp.example.com" || tlsConfig.MinVersion != tls.VersionTLS12 || tlsConfig.InsecureSkipVerify {
		t.Fatal("SMTP TLS config does not enforce verified TLS 1.2 or newer")
	}

	expiresAt := time.Date(2026, 7, 14, 12, 30, 0, 0, time.UTC)
	message, err := buildVerificationMessage(valid, "user@example.com", "123456", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	body := string(message)
	for _, required := range []string{"123456", "2026-07-14 12:30:00 UTC", "请勿向他人透露验证码", "不会自动登录"} {
		if !strings.Contains(body, required) {
			t.Fatal("verification message is missing required content")
		}
	}
	for _, forbidden := range []string{"query_code", "查询码", "Cookie", "password"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("verification message contains forbidden content")
		}
	}
}

func TestSMTPSenderReturnsControlledConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	sender, err := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, From: "no-reply@example.com", TLSMode: "starttls", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.SendRecoveryVerification(context.Background(), "user@example.com", "123456", time.Now().Add(time.Minute))
	if !errors.Is(err, errSMTPUnavailable) || strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatal("SMTP connection failure was not returned as a controlled error")
	}
}

func TestSMTPSenderCancellationInterruptsSMTPHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()

	sender, err := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, From: "no-reply@example.com", TLSMode: "starttls", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- sender.SendRecoveryVerification(ctx, "user@example.com", "123456", time.Now().Add(time.Minute))
	}()
	connection := <-accepted
	defer connection.Close()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled SMTP delivery error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP delivery did not stop promptly after context cancellation")
	}
}

func TestSMTPSenderRequiresSTARTTLSAndDoesNotRetry(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var connections atomic.Int32
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		connections.Add(1)
		defer connection.Close()
		_, _ = connection.Write([]byte("220 localhost ESMTP\r\n"))
		reader := bufio.NewReader(connection)
		_, _ = reader.ReadString('\n')
		_, _ = connection.Write([]byte("250-localhost\r\n250 HELP\r\n"))
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	sender, err := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, From: "no-reply@example.com", TLSMode: "starttls", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.SendRecoveryVerification(context.Background(), "user@example.com", "123456", time.Now().Add(time.Minute))
	<-serverDone
	if !errors.Is(err, errSMTPUnavailable) || connections.Load() != 1 {
		t.Fatal("STARTTLS absence was not rejected with one controlled delivery attempt")
	}
}

func TestSMTPSenderTLSFailureIsControlledAndDoesNotRetry(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var connections atomic.Int32
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		connections.Add(1)
		defer connection.Close()
		_, _ = connection.Write([]byte("not a TLS server"))
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	sender, err := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, From: "no-reply@example.com", TLSMode: "tls", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = sender.SendRecoveryVerification(context.Background(), "user@example.com", "123456", time.Now().Add(time.Minute))
	<-serverDone
	if !errors.Is(err, errSMTPUnavailable) || connections.Load() != 1 {
		t.Fatal("TLS failure was not returned as one controlled delivery attempt")
	}
}

func TestSMTPSenderCommandTimeoutIsControlled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	sender, err := NewSMTPSender(SMTPConfig{Host: "127.0.0.1", Port: port, From: "no-reply@example.com", TLSMode: "starttls", Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- sender.SendRecoveryVerification(context.Background(), "user@example.com", "123456", time.Now().Add(time.Minute))
	}()
	connection := <-accepted
	defer connection.Close()
	select {
	case err := <-result:
		if !errors.Is(err, errSMTPUnavailable) || strings.Contains(err.Error(), "123456") || strings.Contains(err.Error(), "user@example.com") {
			t.Fatal("SMTP timeout did not return a redacted controlled error")
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP command timeout did not cover the server greeting")
	}
}
