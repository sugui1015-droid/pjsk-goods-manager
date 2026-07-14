package recoveryemailverification

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestQueryCodeRecoverySenderHasDistinctPurpose(t *testing.T) {
	sender := NewFakeSender()
	if err := sender.SendQueryCodeRecovery(context.Background(), "safe@example.com", "123456", time.Now().Add(time.Minute)); err != nil {
		t.Fatal("fake query-code recovery sender failed")
	}
	if len(sender.Deliveries()) != 1 {
		t.Fatal("fake sender did not record the purpose-specific delivery")
	}
	message, err := buildQueryCodeRecoveryMessage(SMTPConfig{From: "noreply@example.com", FromName: "PJSK"}, "safe@example.com", "123456", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	text := string(message)
	if !strings.Contains(text, "查询码重置") || strings.Contains(text, "找回邮箱验证码") {
		t.Fatal("query-code recovery email did not preserve purpose isolation")
	}
	for _, forbidden := range []string{"query_code", "Cookie", "session", "password"} {
		if strings.Contains(text, forbidden) {
			t.Fatal("query-code recovery email contained a forbidden field")
		}
	}
}
