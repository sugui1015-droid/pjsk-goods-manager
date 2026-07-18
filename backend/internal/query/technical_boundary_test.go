package query

import (
	"encoding/json"
	"strings"
	"testing"
)

// 第 5 阶段：普通用户接口的技术标识边界。
//
// 这些检查针对的是真实序列化结果，而不是结构体源码：`json:"-"` 很容易在后续
// 改动中被误删，而一旦删掉，用户接口就会开始下发内部 UUID。这里直接把 DTO
// 编码成 JSON 再检查键名，任何回退都会立刻失败。

// technicalKeys are the identifiers that must never reach a regular user.
var technicalKeys = []string{
	"id", "order_id", "order_item_id", "product_id", "user_id", "payment_id",
	"import_batch_id", "sku", "sha", "file_hash", "content_hash",
	"source_sheet", "source_row_key", "source_file", "idempotency_key",
	"order_no", "project_id", "project_name", "query_code_hash",
}

func encodeKeys(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return decoded
}

func assertNoTechnicalKeys(t *testing.T, name string, decoded map[string]any) {
	t.Helper()
	for _, key := range technicalKeys {
		if _, present := decoded[key]; present {
			t.Fatalf("%s 把技术标识 %q 下发给了普通用户", name, key)
		}
	}
}

// TestUserDTOsNeverSerializeTechnicalIdentifiers fills every user-facing DTO
// with non-zero values (so nothing is skipped by omitempty) and asserts the
// encoded payload carries business fields only.
func TestUserDTOsNeverSerializeTechnicalIdentifiers(t *testing.T) {
	displayName := "测试用户"
	hash := "should-never-appear"

	user := User{
		ID:            "11111111-1111-1111-1111-111111111111",
		CNCode:        "CN001",
		DisplayName:   &displayName,
		QueryCodeHash: &hash,
		Status:        "active",
	}
	decoded := encodeKeys(t, user)
	assertNoTechnicalKeys(t, "User", decoded)
	if _, ok := decoded["cn_code"]; !ok {
		t.Fatal("User 应保留 cn_code 业务字段")
	}

	item := OrderItem{
		GoodsName: "初音未来吧唧", Category: "吧唧", CharacterName: "初音未来",
		SeriesCode: "MK-01", DisplayName: "初音未来吧唧", Quantity: 2,
		UnitPrice: 30, Amount: 60, PaidAmount: 0, RemainingAmount: 60,
		PaymentStatus: "unpaid",
	}
	assertNoTechnicalKeys(t, "OrderItem", encodeKeys(t, item))

	paymentItem := PaymentItem{
		DisplayName: "初音未来吧唧", CharacterName: "初音未来", Category: "吧唧",
		Quantity: 2, UnitPrice: 30, Amount: 60, AppliedAmount: 25,
		PaymentStatus: "partial",
	}
	assertNoTechnicalKeys(t, "PaymentItem", encodeKeys(t, paymentItem))

	payment := PaymentRecord{
		ID: "22222222-2222-2222-2222-222222222222", PrincipalAmount: 25,
		FeeAmount: 0, TotalAmount: 25, PaymentMethod: "alipay",
		Status: "approved", PaidAt: "2026-07-17T00:00:00Z",
		Items: []PaymentItem{paymentItem},
	}
	assertNoTechnicalKeys(t, "PaymentRecord", encodeKeys(t, payment))
}

// TestUserResponseCarriesNoInternalUUIDs is a whole-payload guard: it encodes a
// complete /api/query/orders response and asserts the raw JSON text contains
// neither of the internal UUIDs nor the query-code hash, wherever they might be
// nested.
func TestUserResponseCarriesNoInternalUUIDs(t *testing.T) {
	displayName := "测试用户"
	hash := "query-code-hash-value"
	orderUUID := "33333333-3333-3333-3333-333333333333"
	paymentUUID := "44444444-4444-4444-4444-444444444444"

	response := OrdersResponse{
		User: User{ID: orderUUID, CNCode: "CN001", DisplayName: &displayName, QueryCodeHash: &hash, Status: "active"},
		Orders: []Order{{
			TotalQuantity: 2, TotalAmount: 60, PaidAmount: 0, RemainingAmount: 60,
			Items: []OrderItem{{GoodsName: "初音未来吧唧", DisplayName: "初音未来吧唧", Quantity: 2, UnitPrice: 30, Amount: 60, RemainingAmount: 60, PaymentStatus: "unpaid"}},
		}},
		Payments:      []PaymentRecord{{ID: paymentUUID, TotalAmount: 25, Status: "approved", PaidAt: "2026-07-17T00:00:00Z"}},
		TotalQuantity: 2, TotalAmount: 60, PaidAmount: 0, RemainingAmount: 60,
	}

	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, secret := range []string{orderUUID, paymentUUID, hash} {
		if strings.Contains(body, secret) {
			t.Fatalf("普通用户响应泄漏了内部标识 %q\n%s", secret, body)
		}
	}
	// 业务内容仍然完整。
	for _, expected := range []string{"CN001", "初音未来吧唧", "unpaid"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("普通用户响应缺少业务字段 %q", expected)
		}
	}
}
