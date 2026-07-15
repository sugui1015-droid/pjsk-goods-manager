package admin

import (
	"errors"
	"strings"
	"time"
)

type AdminAuthEventType string

type AdminAuthAuditResult string

type AdminAuthReasonCode string

const (
	AdminAuthEventLoginSucceeded   AdminAuthEventType = "admin_login_succeeded"
	AdminAuthEventLoginFailed      AdminAuthEventType = "admin_login_failed"
	AdminAuthEventLoginRateLimited AdminAuthEventType = "admin_login_rate_limited"
	AdminAuthEventLogoutSucceeded  AdminAuthEventType = "admin_logout_succeeded"

	AdminAuthResultSuccess AdminAuthAuditResult = "success"
	AdminAuthResultFailure AdminAuthAuditResult = "failure"

	AdminAuthReasonNone               AdminAuthReasonCode = "none"
	AdminAuthReasonInvalidCredentials AdminAuthReasonCode = "invalid_credentials"
	AdminAuthReasonAccountDisabled    AdminAuthReasonCode = "account_disabled"
	AdminAuthReasonRateLimited        AdminAuthReasonCode = "rate_limited"
	AdminAuthReasonDatabaseError      AdminAuthReasonCode = "database_error"
	AdminAuthReasonAuditWriteError    AdminAuthReasonCode = "audit_write_error"
)

var ErrInvalidAdminAuthAuditEvent = errors.New("invalid admin auth audit event")

type AdminAuthAuditEvent struct {
	EventType          AdminAuthEventType
	OccurredAt         time.Time
	AdminID            *string
	UsernameNormalized string
	ClientIP           string
	Result             AdminAuthAuditResult
	ReasonCode         AdminAuthReasonCode
	UserAgentSummary   *string
}

func buildAdminAuthAuditEvent(
	r *httpRequestSummary,
	eventType AdminAuthEventType,
	adminID *string,
	username string,
	clientIP string,
	result AdminAuthAuditResult,
	reason AdminAuthReasonCode,
	occurredAt time.Time,
) AdminAuthAuditEvent {
	var userAgent *string
	if r != nil {
		userAgent = summarizeUserAgent(r.UserAgent)
	}
	return AdminAuthAuditEvent{
		EventType:          eventType,
		OccurredAt:         occurredAt,
		AdminID:            adminID,
		UsernameNormalized: normalizeAuditText(normalizeLimiterUsername(username), 128),
		ClientIP:           normalizeAuditText(clientIP, 128),
		Result:             result,
		ReasonCode:         reason,
		UserAgentSummary:   userAgent,
	}
}

type httpRequestSummary struct {
	UserAgent string
}

func summarizeUserAgent(value string) *string {
	cleaned := normalizeAuditText(value, 256)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func normalizeAuditText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxRunes <= 0 {
		return ""
	}
	var builder strings.Builder
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if builder.Len() == 0 {
			builder.Grow(len(value))
		}
		if runeCount(builder.String()) >= maxRunes {
			break
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func runeCount(value string) int {
	count := 0
	for range value {
		count++
	}
	return count
}

func validateAdminAuthAuditEvent(event AdminAuthAuditEvent) error {
	if !validAdminAuthEventType(event.EventType) || !validAdminAuthResult(event.Result) || !validAdminAuthReason(event.ReasonCode) {
		return ErrInvalidAdminAuthAuditEvent
	}
	if (event.Result == AdminAuthResultSuccess && event.ReasonCode != AdminAuthReasonNone) ||
		(event.Result == AdminAuthResultFailure && event.ReasonCode == AdminAuthReasonNone) {
		return ErrInvalidAdminAuthAuditEvent
	}
	if event.UsernameNormalized == "" || runeCount(event.UsernameNormalized) > 128 {
		return ErrInvalidAdminAuthAuditEvent
	}
	if event.ClientIP == "" || runeCount(event.ClientIP) > 128 {
		return ErrInvalidAdminAuthAuditEvent
	}
	if event.UserAgentSummary != nil && runeCount(*event.UserAgentSummary) > 256 {
		return ErrInvalidAdminAuthAuditEvent
	}
	return nil
}

func validAdminAuthEventType(value AdminAuthEventType) bool {
	switch value {
	case AdminAuthEventLoginSucceeded, AdminAuthEventLoginFailed, AdminAuthEventLoginRateLimited, AdminAuthEventLogoutSucceeded:
		return true
	default:
		return false
	}
}

func validAdminAuthResult(value AdminAuthAuditResult) bool {
	return value == AdminAuthResultSuccess || value == AdminAuthResultFailure
}

func validAdminAuthReason(value AdminAuthReasonCode) bool {
	switch value {
	case AdminAuthReasonNone, AdminAuthReasonInvalidCredentials, AdminAuthReasonAccountDisabled, AdminAuthReasonRateLimited, AdminAuthReasonDatabaseError, AdminAuthReasonAuditWriteError:
		return true
	default:
		return false
	}
}
