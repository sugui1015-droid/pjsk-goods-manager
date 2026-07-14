package recoveryemailverification

import (
	"context"
	"errors"
	"strings"
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
	if len(deliveries) != 1 || deliveries[0].To != "user@example.com" || deliveries[0].Code != "123456" || !deliveries[0].ExpiresAt.Equal(expiresAt) {
		t.Fatal("fake sender did not record the delivery")
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
	}
	for _, config := range invalid {
		if _, err := NewSMTPSender(config); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("invalid SMTP config error = %v", err)
		}
	}

	expiresAt := time.Date(2026, 7, 14, 12, 30, 0, 0, time.UTC)
	message, err := buildVerificationMessage(valid, "user@example.com", "123456", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	body := string(message)
	for _, required := range []string{"123456", "2026-07-14 12:30:00 UTC", "请勿向他人透露验证码"} {
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
