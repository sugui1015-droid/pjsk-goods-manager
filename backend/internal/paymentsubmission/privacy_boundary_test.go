package paymentsubmission

import (
	"encoding/json"
	"strings"
	"testing"
)

// technicalKeys must never appear in a regular-user JSON response, and must never
// appear in the admin *list* JSON (they belong only in the admin detail's
// collapsed "技术标识" section).
var technicalKeys = []string{
	"sha256", "sha", "image_data", "byte_size", "mime_type",
	"user_id", "linked_payment_id", "original_filename_safe",
	"reviewed_by_admin_id", "idempotency_key",
}

func TestUserSubmissionNeverSerializesTechnicalIdentifiers(t *testing.T) {
	// Fill everything with non-zero values so omitempty cannot hide a leak: the
	// only reason a technical key is absent is that it has no json tag at all.
	item := UserSubmission{
		ID:              "sub-1",
		PaymentMethod:   "wechat",
		PrincipalAmount: 120,
		FeeAmount:       0.12,
		PayableAmount:   120.12,
		Status:          StatusRejected,
		SubmittedAt:     "2026-07-18T00:00:00Z",
		ReviewedAt:      "2026-07-18T01:00:00Z",
		RejectReason:    "图片不清晰",
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, key := range technicalKeys {
		if strings.Contains(body, `"`+key+`"`) {
			t.Fatalf("user submission JSON must not contain %q: %s", key, body)
		}
	}
	// Business fields the user needs must still be present.
	for _, key := range []string{"payment_method", "principal_amount", "fee_amount", "payable_amount", "status", "submitted_at", "reject_reason"} {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Fatalf("user submission JSON missing business field %q: %s", key, body)
		}
	}
}

func TestAdminListItemHasNoTechnicalIdentifiers(t *testing.T) {
	item := AdminListItem{
		ID:              "sub-1",
		CNCode:          "测试CN01",
		DisplayName:     "验收用户",
		PaymentMethod:   "wechat",
		PrincipalAmount: 120,
		FeeAmount:       0.12,
		PayableAmount:   120.12,
		Status:          StatusApproved,
		SubmittedAt:     "2026-07-18T00:00:00Z",
		ReviewedAt:      "2026-07-18T01:00:00Z",
		ReviewedBy:      "qa_admin",
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, key := range technicalKeys {
		if strings.Contains(body, `"`+key+`"`) {
			t.Fatalf("admin list JSON must not contain %q (technical fields belong to detail only): %s", key, body)
		}
	}
}

func TestAdminDetailMayCarryTechnicalIdentifiers(t *testing.T) {
	// The detail view is where the technical section lives; it is allowed to
	// carry the sha, internal ids, etc. This documents the intended boundary.
	detail := AdminDetail{
		AdminListItem:    AdminListItem{ID: "sub-1", CNCode: "CN", Status: StatusApproved},
		MimeType:         "image/png",
		ByteSize:         1234,
		SHA256:           strings.Repeat("a", 64),
		OriginalFilename: "proof.png",
		UserID:           "user-1",
		LinkedPaymentID:  "pay-1",
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, key := range []string{"sha256", "user_id", "linked_payment_id"} {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Fatalf("admin detail JSON should expose %q for the technical section: %s", key, body)
		}
	}
}
