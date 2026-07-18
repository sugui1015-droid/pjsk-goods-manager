package users

import (
	"encoding/json"
	"strings"
	"testing"
)

// 第 6 阶段：用户列表的隐私边界。
//
// 这些检查针对真实的序列化结果。查询码哈希、邮箱密文与盲索引一旦被误加进
// ListItem，就会立刻失败——而不是等到有人在浏览器里看见它。

// TestListItemNeverSerializesSecrets fills the DTO with non-zero values and
// asserts the encoded payload exposes only business fields: the query code and
// the recovery email survive as booleans, never as material an attacker could
// use.
func TestListItemNeverSerializesSecrets(t *testing.T) {
	item := ListItem{
		ID:                 "11111111-1111-1111-1111-111111111111",
		CNCode:             "CN001",
		DisplayName:        "测试用户",
		HasQueryCode:       true,
		HasRecoveryEmail:   true,
		Status:             "active",
		OrderCount:         2,
		TotalAmount:        145,
		PaidAmount:         25,
		RemainingAmount:    120,
		CreatedAt:          "2026-07-17T00:00:00Z",
		QueryCodeUpdatedAt: "2026-07-17T00:00:00Z",
		LastLoginAt:        "2026-07-17T00:00:00Z",
	}

	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	forbidden := []string{
		"query_code", "query_code_hash", "password", "encrypted_email",
		"email_lookup_hash", "email", "recovery_token", "bind_token",
		"session_id", "verification_code", "idempotency_key",
	}
	for _, key := range forbidden {
		if _, present := decoded[key]; present {
			t.Fatalf("用户列表下发了敏感字段 %q", key)
		}
	}

	// The two security columns must be plain booleans.
	for _, key := range []string{"has_query_code", "has_recovery_email"} {
		value, present := decoded[key]
		if !present {
			t.Fatalf("缺少业务字段 %q", key)
		}
		if _, ok := value.(bool); !ok {
			t.Fatalf("%q 应为布尔状态，实际是 %T", key, value)
		}
	}
}

// TestListResponseCarriesNoSecretMaterial encodes a whole list page and asserts
// the raw JSON text contains no hash or ciphertext, wherever it might nest.
func TestListResponseCarriesNoSecretMaterial(t *testing.T) {
	response := ListResponse{
		Items: []ListItem{{
			ID: "11111111-1111-1111-1111-111111111111", CNCode: "CN001",
			HasQueryCode: true, HasRecoveryEmail: true, Status: "active",
		}},
		Summary:    ListSummary{UserCount: 1},
		Page:       1,
		PageSize:   50,
		Total:      1,
		TotalPages: 1,
	}

	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, secret := range []string{"$2a$", "query_code_hash", "encrypted_email", "email_lookup_hash"} {
		if strings.Contains(body, secret) {
			t.Fatalf("用户列表响应泄漏了 %q\n%s", secret, body)
		}
	}
	// Pagination is part of the contract.
	for _, expected := range []string{`"page":1`, `"page_size":50`, `"total":1`, `"total_pages":1`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("响应缺少分页字段 %s\n%s", expected, body)
		}
	}
}
