package export

import (
	"strings"
	"testing"
)

// 第 5 阶段：导出字段边界。
//
// 管理员导出可以保留来源审计字段，但业务字段必须在前、技术字段统一在最后，
// 且任何导出都不得包含秘密。

// TestOrderItemExportPutsAuditColumnsLast pins the column order: an operator
// reading the file left-to-right meets the business columns first, and the
// source-tracking columns sit together at the end.
func TestOrderItemExportPutsAuditColumnsLast(t *testing.T) {
	headers := orderItemHeaders()

	auditColumns := []string{"来源文件", "来源 Sheet", "来源位置"}
	tail := headers[len(headers)-len(auditColumns):]
	for i, expected := range auditColumns {
		if tail[i] != expected {
			t.Fatalf("审计字段必须排在最后：headers=%v", headers)
		}
	}

	indexOf := func(name string) int {
		for i, header := range headers {
			if header == name {
				return i
			}
		}
		t.Fatalf("缺少表头 %q", name)
		return -1
	}
	for _, business := range []string{"CN", "谷子名称", "角色", "数量", "单价", "付款状态", "订单号"} {
		if indexOf(business) > indexOf("来源文件") {
			t.Fatalf("业务字段 %q 不应排在技术字段之后：%v", business, headers)
		}
	}

	// 表头必须是中文且含义清楚，不用模糊缩写。
	for _, header := range headers {
		if header == "" {
			t.Fatalf("导出表头不得为空：%v", headers)
		}
		for _, vague := range []string{"id", "ID", "hash", "sha", "uuid", "key"} {
			if strings.EqualFold(header, vague) {
				t.Fatalf("导出表头 %q 是模糊缩写", header)
			}
		}
	}
}

// TestExportsCarryNoSecrets guards the hard rule: no password, query code,
// verification code, recovery/session token or key ever leaves through an
// export. The user export may report whether a query code is set, but never
// the code itself.
func TestExportsCarryNoSecrets(t *testing.T) {
	headerSets := map[string][]string{
		"order-items": orderItemHeaders(),
		"users":       {"CN", "订单总金额", "有效已付总额", "剩余待付总额", "显示名称", "查询码状态", "用户状态", "订单数", "创建时间"},
	}

	for name, headers := range headerSets {
		for _, header := range headers {
			for _, secret := range []string{"密码", "验证码", "恢复令牌", "会话令牌", "加密密钥", "password", "token", "secret"} {
				if strings.Contains(strings.ToLower(header), strings.ToLower(secret)) {
					t.Fatalf("%s 导出泄漏秘密字段 %q", name, header)
				}
			}
			// 查询码本身不得导出；只允许导出它的状态。
			if strings.Contains(header, "查询码") && header != "查询码状态" {
				t.Fatalf("%s 导出不得包含查询码本身：%q", name, header)
			}
		}
	}
}
