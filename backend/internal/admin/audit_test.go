package admin

import (
	"strings"
	"testing"
	"time"
)

func TestBuildAdminAuthAuditEventSanitizesAndTruncates(t *testing.T) {
	adminID := "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327"
	longAgent := strings.Repeat("a", 300) + "\nsecret"
	event := buildAdminAuthAuditEvent(
		&httpRequestSummary{UserAgent: longAgent},
		AdminAuthEventLoginSucceeded,
		&adminID,
		"  Admin  ",
		" 203.0.113.10\r\nspoofed ",
		AdminAuthResultSuccess,
		AdminAuthReasonNone,
		time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
	)

	if event.UsernameNormalized != "admin" {
		t.Fatalf("username = %q, want admin", event.UsernameNormalized)
	}
	if strings.Contains(event.ClientIP, "\r") || strings.Contains(event.ClientIP, "\n") {
		t.Fatalf("client ip was not sanitized: %q", event.ClientIP)
	}
	if event.UserAgentSummary == nil {
		t.Fatal("expected user agent summary")
	}
	if runeCount(*event.UserAgentSummary) != 256 {
		t.Fatalf("user agent length = %d, want 256", runeCount(*event.UserAgentSummary))
	}
	if strings.Contains(*event.UserAgentSummary, "secret") {
		t.Fatal("user agent summary included text after truncation/control characters")
	}
	if err := validateAdminAuthAuditEvent(event); err != nil {
		t.Fatalf("event should be valid: %v", err)
	}
}

func TestValidateAdminAuthAuditEventRejectsInvalidCombinations(t *testing.T) {
	valid := AdminAuthAuditEvent{
		EventType:          AdminAuthEventLoginFailed,
		OccurredAt:         time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
		UsernameNormalized: "admin",
		ClientIP:           "203.0.113.10",
		Result:             AdminAuthResultFailure,
		ReasonCode:         AdminAuthReasonInvalidCredentials,
	}
	if err := validateAdminAuthAuditEvent(valid); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}

	tests := []AdminAuthAuditEvent{
		{EventType: "unknown", UsernameNormalized: "admin", ClientIP: "203.0.113.10", Result: AdminAuthResultFailure, ReasonCode: AdminAuthReasonInvalidCredentials},
		{EventType: AdminAuthEventLoginFailed, UsernameNormalized: "admin", ClientIP: "203.0.113.10", Result: AdminAuthResultSuccess, ReasonCode: AdminAuthReasonInvalidCredentials},
		{EventType: AdminAuthEventLoginFailed, UsernameNormalized: "admin", ClientIP: "203.0.113.10", Result: AdminAuthResultFailure, ReasonCode: AdminAuthReasonNone},
		{EventType: AdminAuthEventLoginFailed, UsernameNormalized: "", ClientIP: "203.0.113.10", Result: AdminAuthResultFailure, ReasonCode: AdminAuthReasonInvalidCredentials},
		{EventType: AdminAuthEventLoginFailed, UsernameNormalized: "admin", ClientIP: "", Result: AdminAuthResultFailure, ReasonCode: AdminAuthReasonInvalidCredentials},
	}
	for i, event := range tests {
		if err := validateAdminAuthAuditEvent(event); err == nil {
			t.Fatalf("invalid event %d was accepted: %+v", i, event)
		}
	}
}
